/**
 * Tencent is pleased to support the open source community by making Polaris available.
 *
 * Copyright (C) 2019 THL A29 Limited, a Tencent company. All rights reserved.
 *
 * Licensed under the BSD 3-Clause License (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 * https://opensource.org/licenses/BSD-3-Clause
 *
 * Unless required by applicable law or agreed to in writing, software distributed
 * under the License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR
 * CONDITIONS OF ANY KIND, either express or implied. See the License for the
 * specific language governing permissions and limitations under the License.
 */

package metrics

import (
	"context"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	adminv3 "github.com/envoyproxy/go-control-plane/envoy/admin/v3"
	polarisgo "github.com/polarismesh/polaris-go"
	"github.com/polarismesh/polaris-go/pkg/model"
	"github.com/polarismesh/polaris-go/pkg/model/pb"
	"github.com/polarismesh/specification/source/go/api/v1/service_manage"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/wrapperspb"

	"github.com/polarismesh/polaris-sidecar/pkg/log"
	"github.com/polarismesh/polaris-sidecar/pkg/polaris"
)

type Server struct {
	port              int
	consumer          polarisgo.ConsumerAPI
	namespace         string
	ClusterMetricsURL string
	once              sync.Once
}

func NewServer(namespace string, port int) *Server {
	srv := &Server{
		namespace: namespace,
		port:      port,
	}
	return srv
}

const (
	DefaultListenPort = 15985
	ticketDuration    = 30 * time.Second
)

func (s *Server) Run(ctx context.Context, wg *sync.WaitGroup, errChan chan error) {
	if s == nil {
		log.Infof("[envoy-metrics] metric server is nil, skip running")
		return
	}
	log.Info("[envoy-metrics] start metric server")
	wg.Add(1)
	defer func() {
		s.Destroy()
		wg.Done()
	}()
	var err error
	s.consumer, err = polaris.GetConsumerAPI()
	if nil != err {
		log.Errorf("[envoy-metrics] fail to get consumer api, error: %v", err)
		errChan <- err
		return
	}
	ticker := time.NewTicker(ticketDuration)
	defer func() {
		ticker.Stop()
	}()
	values := make(map[InstanceMetricKey]*InstanceMetricValue)
	cleanCounter := 0
	const cleanThreshold = 10                // 每10次ticker触发清理一次
	const inactiveDuration = 5 * time.Minute // 5分钟未更新视为不活跃
	for {
		select {
		case <-ticker.C:
			cleanCounter++
			if cleanCounter >= cleanThreshold {
				cleanCounter = 0
				now := time.Now()
				for k, v := range values {
					if now.Sub(v.LastActiveTime) > inactiveDuration {
						// values map会持续增长且未清理旧数据，可能会导致内存泄漏，因此每隔一段时间清理一次
						delete(values, k)
					}
				}
			}
			s.reportMetricByCluster(values)
		case <-ctx.Done():
			log.Infof("[envoy-metrics] metric server get stop signal")
			return
		}
	}
}

func (s *Server) Destroy() {
	s.once.Do(func() {
		if s.consumer != nil {
			s.consumer.Destroy()
		}
		log.Infof("[envoy-metrics] metric server stopped")
	})
}

func (s *Server) getClusterStats() *StatsObject {
	resp, err := http.Get("http://127.0.0.1:15000/stats?format=json")
	if nil != err {
		if err == io.EOF {
			return nil
		}
		log.Warnf("[envoy-metrics] metric server received stat debugger error %s", err)
		return nil
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			log.Errorf("fail to close body stream, error: %v", err)
		}
	}(resp.Body)
	body, err := io.ReadAll(resp.Body)
	if nil != err {
		log.Warnf("[envoy-metrics] fail to read all text from stat body stream, error: %s", err)
		return nil
	}
	statsObject := &StatsObject{}
	err = json.Unmarshal(body, statsObject)
	if nil != err {
		log.Warnf("[envoy-metrics] fail to unmarshal stat response text %s to cluster, error %s", string(body), err)
		return nil
	}
	return statsObject
}

var clusterRegex = regexp.MustCompile(`^cluster\..+\.upstream_rq_time$`)

func (s *Server) parseUpstreamDelay() map[string]float64 {
	var retValues = make(map[string]float64)
	statsObject := s.getClusterStats()
	if nil == statsObject || len(statsObject.Stats) == 0 {
		return retValues
	}
	for _, stat := range statsObject.Stats {
		if nil == stat.Histograms {
			continue
		}
		computedQuantiles := stat.Histograms.ComputedQuantiles
		if len(computedQuantiles) == 0 {
			continue
		}
		for _, computedQuantile := range computedQuantiles {
			matches := clusterRegex.Match([]byte(computedQuantile.Name))
			if !matches {
				log.Debugf("skip non cluster stats %s", computedQuantile.Name)
				continue
			}
			dotIndex := strings.Index(computedQuantile.Name, ".")
			if dotIndex == -1 {
				log.Debugf("skip invalid stats name %s without dot", computedQuantile.Name)
				continue
			}
			svcName := computedQuantile.Name[dotIndex+1:]
			lastDotIndex := strings.LastIndex(svcName, ".")
			if lastDotIndex == -1 {
				log.Debugf("skip invalid stats name %s without last dot", svcName)
				continue
			}
			svcName = svcName[0:lastDotIndex]
			values := computedQuantile.Values
			if len(values) < 3 {
				continue
			}
			// get the P50 value to perform as average
			value := values[2].Cumulative
			if nil == value {
				continue
			}
			switch v := value.(type) {
			case float64:
				retValues[svcName] = v
			case int:
				retValues[svcName] = float64(v)
			}

		}
	}
	return retValues
}

func (s *Server) reportMetricByCluster(values map[InstanceMetricKey]*InstanceMetricValue) {
	// 从配置或环境变量中获取URL
	clusterURL := s.ClusterMetricsURL // 假设配置结构体中有一个ClusterMetricsURL字段
	if clusterURL == "" {
		// 如果配置中没有设置，可以回退到环境变量
		clusterURL = os.Getenv("CLUSTER_METRICS_URL")
		if clusterURL == "" {
			// 如果环境变量也没有设置，使用默认值（可选）
			clusterURL = "http://127.0.0.1:15000"
		}
	}

	resp, err := http.Get(clusterURL + "/clusters?format=json")
	if nil != err {
		if err == io.EOF {
			return
		}
		log.Warnf("[envoy-metrics] metric server received clusters debugger error %s", err)
		return
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			log.Errorf("[envoy-metrics] fail to close body stream, error: %v", err)
		}
	}(resp.Body)
	body, err := io.ReadAll(resp.Body)
	if nil != err {
		log.Warnf("[envoy-metrics] fail to read all text from body stream, error: %s", err)
		return
	}
	clusters := &adminv3.Clusters{}
	err = protojson.Unmarshal(body, clusters)
	if nil != err {
		log.Warnf("[envoy-metrics] fail to unmarshal response text %s to cluster, error %s", string(body), err)
		return
	}
	delayValues := s.parseUpstreamDelay()
	log.Debugf("[envoy-metrics] parsed upstream delay is %v", delayValues)
	clusterStatuses := clusters.GetClusterStatuses()
	if len(clusterStatuses) > 0 {
		for _, clusterStatus := range clusterStatuses {
			clusterName := clusterStatus.GetName()
			hostStatuses := clusterStatus.GetHostStatuses()
			if len(hostStatuses) == 0 {
				continue
			}
			for _, hostStatus := range hostStatuses {
				address := hostStatus.GetAddress()
				if nil == address {
					continue
				}
				socketAddress := address.GetSocketAddress()
				if nil == socketAddress {
					continue
				}
				metricKey := InstanceMetricKey{
					ClusterName: clusterName, Host: socketAddress.GetAddress(), Port: socketAddress.GetPortValue()}
				metricValue := &InstanceMetricValue{
					LastActiveTime: time.Now(),
				}
				stats := hostStatus.GetStats()
				if len(stats) > 0 {
					for _, stat := range stats {
						if stat.GetName() == "rq_total" {
							metricValue.RqTotal = stat.GetValue()
						} else if stat.GetName() == "rq_success" {
							metricValue.RqSuccess = stat.GetValue()
						} else if stat.GetName() == "rq_error" {
							metricValue.RqError = stat.GetValue()
						}
					}
				}
				subMetricValue := &InstanceMetricValue{}
				latestValue, ok := values[metricKey]
				if !ok {
					subMetricValue = metricValue
				} else {
					subMetricValue.LastActiveTime = time.Now()
					if metricValue.RqTotal > latestValue.RqTotal {
						subMetricValue.RqTotal = metricValue.RqTotal - latestValue.RqTotal
					}
					if metricValue.RqSuccess > latestValue.RqSuccess {
						subMetricValue.RqSuccess = metricValue.RqSuccess - latestValue.RqSuccess
					}
					if metricValue.RqError > latestValue.RqError {
						subMetricValue.RqError = metricValue.RqError - latestValue.RqError
					}
				}
				values[metricKey] = metricValue
				s.reportMetrics(metricKey, subMetricValue, delayValues[metricKey.ClusterName])
			}
		}
	}
}

func (s *Server) reportMetrics(metricKey InstanceMetricKey, subMetricValue *InstanceMetricValue, delay float64) {
	log.Debugf("[envoy-metrics] start to report metric data %s, metric key %s, delay %v", *subMetricValue, metricKey, delay)
	for i := 0; i < int(subMetricValue.RqSuccess); i++ {
		s.reportStatus(metricKey, model.RetSuccess, 200, delay)
	}
	for i := 0; i < int(subMetricValue.RqError); i++ {
		s.reportStatus(metricKey, model.RetFail, 500, delay)
	}
}

func (s *Server) reportStatus(metricKey InstanceMetricKey, retStatus model.RetStatus, code int32, delay float64) {
	callResult := &polarisgo.ServiceCallResult{}
	callResult.SetRetStatus(retStatus)
	namingInstance := &service_manage.Instance{}
	namingInstance.Service = &wrapperspb.StringValue{Value: metricKey.ClusterName}
	namingInstance.Namespace = &wrapperspb.StringValue{Value: s.namespace}
	namingInstance.Host = &wrapperspb.StringValue{Value: metricKey.Host}
	namingInstance.Port = &wrapperspb.UInt32Value{Value: metricKey.Port}
	instance := pb.NewInstanceInProto(namingInstance,
		&model.ServiceKey{Namespace: s.namespace, Service: metricKey.ClusterName}, nil)
	callResult.SetCalledInstance(instance)
	callResult.SetRetCode(code)
	callResult.SetDelay(time.Duration(delay) * time.Millisecond)

	// 添加指数退避重试机制
	const maxRetries = 3
	const initialBackoff = 100 * time.Millisecond
	const maxBackoff = 5 * time.Second

	backoff := initialBackoff
	for attempt := 0; attempt < maxRetries; attempt++ {
		err := s.consumer.UpdateServiceCallResult(callResult)
		if err == nil {
			return // 成功则返回
		}
		// 计算下一次退避时间
		sleepTime := time.Duration(math.Pow(2, float64(attempt))) * backoff
		if sleepTime > maxBackoff {
			sleepTime = maxBackoff
		}
		log.Warnf("[envoy-metrics] update service call result for %s failed (attempt %d/%d), retrying in %v: %v",
			metricKey, attempt+1, maxRetries, sleepTime, err)
		time.Sleep(sleepTime)
	}
	// 所有重试失败后记录错误
	log.Errorf("[envoy-metrics] fail to update service call result for %s after %d attempts", metricKey, maxRetries)
}
