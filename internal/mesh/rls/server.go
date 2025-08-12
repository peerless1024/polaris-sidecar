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

package rls

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"

	v3 "github.com/envoyproxy/go-control-plane/envoy/extensions/common/ratelimit/v3"
	pb "github.com/envoyproxy/go-control-plane/envoy/service/ratelimit/v3"
	polarisgo "github.com/polarismesh/polaris-go"
	"github.com/polarismesh/polaris-go/pkg/model"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	"github.com/polarismesh/polaris-sidecar/pkg/constants"
	"github.com/polarismesh/polaris-sidecar/pkg/log"
	polarisApi "github.com/polarismesh/polaris-sidecar/pkg/polaris"
)

func New(namespace string, conf *Config) *RateLimitServer {
	if conf == nil {
		log.Errorf("[envoy-rls] config is nil")
		return nil
	}
	conf.init()
	return &RateLimitServer{
		namespace: namespace,
		conf:      conf,
	}
}

type RateLimitServer struct {
	namespace string
	conf      *Config
	limiter   polarisgo.LimitAPI
	ln        net.Listener
	grpcSvr   *grpc.Server
	once      sync.Once
}

func (svr *RateLimitServer) Run(ctx context.Context, wg *sync.WaitGroup, errChan chan error) {
	if svr == nil {
		log.Infof("[envoy-rls] ratelimit server is nil, skip run")
		return
	}
	log.Info("[envoy-rls] start ratelimit server")
	wg.Add(1)
	defer func() {
		svr.Destroy()
		wg.Done()
	}()
	if svr.conf.Network == "unix" {
		if err := os.MkdirAll(filepath.Dir(svr.conf.Address), os.ModePerm); err != nil {
			log.Errorf("[envoy-rls] create unix socket dir error: %v", err)
			errChan <- err
			return
		}
	}
	ln, err := net.Listen(svr.conf.Network, svr.conf.Address)
	if err != nil {
		log.Errorf("[envoy-rls] create listener error: %v", err)
		errChan <- err
		return
	}
	svr.ln = ln
	svr.limiter, err = polarisApi.GetLimitAPI()
	if err != nil {
		log.Errorf("[envoy-rls] get limit api error: %v", err)
		errChan <- err
		return
	}
	// 指定使用服务端证书创建一个 TLS credentials
	var creds credentials.TransportCredentials
	if !svr.conf.TLSInfo.IsEmpty() {
		creds, err = credentials.NewServerTLSFromFile(svr.conf.TLSInfo.CertFile, svr.conf.TLSInfo.KeyFile)
		if err != nil {
			log.Errorf("[envoy-rls] create tls credentials error: %v", err)
			errChan <- err
			return
		}
	}
	// 设置 grpc server options
	opts := []grpc.ServerOption{}
	if creds != nil {
		// 指定使用 TLS credentials
		opts = append(opts, grpc.Creds(creds))
	}
	svr.grpcSvr = grpc.NewServer(opts...)
	pb.RegisterRateLimitServiceServer(svr.grpcSvr, svr)
	go func() {
		errChan <- svr.grpcSvr.Serve(ln)
	}()
	<-ctx.Done()
	log.Infof("[envoy-rls] get context cancel signal")
}

func (svr *RateLimitServer) Destroy() {
	svr.once.Do(func() {
		if svr.limiter != nil {
			svr.limiter.Destroy()
		}
		if svr.grpcSvr != nil {
			svr.grpcSvr.GracefulStop()
		}
		if svr.ln != nil {
			if err := svr.ln.Close(); err != nil {
				log.Errorf("[envoy-rls] close listener error: %v", err)
			}
		}
		if svr.conf != nil && svr.conf.Network == "unix" {
			if err := os.RemoveAll(filepath.Dir(svr.conf.Address)); err != nil {
				log.Errorf("[envoy-rls] remove unix socket dir error: %v", err)
			}
		}
		log.Infof("[envoy-rls] ratelimit server stopped")
	})
}

func (svr *RateLimitServer) ShouldRateLimit(ctx context.Context, req *pb.RateLimitRequest) (*pb.RateLimitResponse, error) {
	protocol := ctx.Value(constants.ContextProtocol)
	log.Info("[envoy-rls] receive ratelimit request", zap.Any("req", req), zap.Any("protocol", protocol))
	acquireQuota := req.GetHitsAddend()
	if acquireQuota == 0 {
		acquireQuota = 1
	}

	quotaReq, err := svr.buildQuotaRequest(req.GetDomain(), acquireQuota, req.GetDescriptors())
	if err != nil {
		log.Error("[envoy-rls] build ratelimit quota request", zap.Error(err))
		return nil, err
	}
	future, err := svr.limiter.GetQuota(quotaReq)
	if err != nil {
		log.Error("[envoy-rls] get quota", zap.Error(err))
		return nil, err
	}
	if future == nil {
		log.Error("[envoy-rls] get quota future is nil")
		return nil, err
	}
	resp := future.Get()

	overallCode := pb.RateLimitResponse_OK
	if resp.Code == model.QuotaResultLimited {
		overallCode = pb.RateLimitResponse_OVER_LIMIT
	}

	descriptorStatus := make([]*pb.RateLimitResponse_DescriptorStatus, 0, len(req.GetDescriptors()))
	for range req.GetDescriptors() {
		descriptorStatus = append(descriptorStatus, &pb.RateLimitResponse_DescriptorStatus{
			Code: overallCode,
		})
	}

	rlsRsp := &pb.RateLimitResponse{
		OverallCode: overallCode,
		Statuses:    descriptorStatus,
		RawBody:     []byte(resp.Info),
	}
	log.Info("[envoy-rls] send envoy rls response", zap.Any("rsp", rlsRsp))
	return rlsRsp, nil
}

func (svr *RateLimitServer) buildQuotaRequest(domain string, acquireQuota uint32,
	descriptors []*v3.RateLimitDescriptor) (polarisgo.QuotaRequest, error) {
	req := polarisgo.NewQuotaRequest()
	if domain == "" || svr.namespace == "" {
		log.Errorf("[envoy-rls] domain or namespace is empty")
		return req, nil
	}
	for i := range descriptors {
		descriptor := descriptors[i]
		if descriptor == nil {
			log.Errorf("[envoy-rls] descriptor is nil")
			continue
		}
		for _, entry := range descriptor.GetEntries() {
			if entry.GetKey() == ":path" {
				req.SetMethod(entry.GetValue())
				continue
			}
			req.AddArgument(model.BuildArgumentFromLabel(entry.GetKey(), entry.GetValue()))
		}
	}

	if strings.HasSuffix(domain, "."+svr.namespace) {
		domain = strings.TrimSuffix(domain, "."+svr.namespace)
	}
	req.SetNamespace(svr.namespace)
	req.SetService(domain)
	req.SetToken(acquireQuota)
	log.Info("[envoy-rls] build polaris quota request", zap.Any("param", req))
	return req, nil
}
