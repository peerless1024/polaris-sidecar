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

package polaris

import (
	"errors"
	"strconv"
	"sync"

	polarisgo "github.com/polarismesh/polaris-go"
	"github.com/polarismesh/polaris-go/api"
	"github.com/polarismesh/polaris-go/pkg/config"
	"github.com/polarismesh/polaris-go/plugin/metrics/prometheus"

	"github.com/polarismesh/polaris-sidecar/pkg/log"
	"github.com/polarismesh/polaris-sidecar/pkg/utils"
)

var (
	lock       sync.Mutex
	SDKContext api.SDKContext
)

func InitPolarisContext(conf *Config) error {
	lock.Lock()
	defer lock.Unlock()
	if SDKContext != nil {
		log.Infof("polaris SDKContext already initialized")
		return nil
	}
	sdkCfg := config.NewDefaultConfiguration(conf.Addresses)
	sdkCfg.Consumer.CircuitBreaker.SetEnable(false)
	if conf.Metrics != nil {
		sdkCfg.Global.StatReporter.SetEnable(true)
		sdkCfg.Global.StatReporter.SetChain([]string{"prometheus"})
		if err := sdkCfg.Global.StatReporter.SetPluginConfig("prometheus", &prometheus.Config{
			Type:     conf.Metrics.Type,
			IP:       conf.Metrics.IP,
			PortStr:  strconv.FormatInt(int64(conf.Metrics.Port), 10),
			Interval: conf.Metrics.Interval,
			Address:  conf.Metrics.Address,
		}); err != nil {
			log.Errorf("fail to set prometheus config, err: %v", err)
		}
	}
	if conf.LocationConfigImpl != nil {
		sdkCfg.Global.Location = conf.LocationConfigImpl
	}
	if conf.NearbyMatchLevel != "" {
		sdkCfg.Consumer.ServiceRouter.GetNearbyConfig().SetMatchLevel(conf.NearbyMatchLevel)
	}
	sdkCtx, err := polarisgo.NewSDKContextByConfig(sdkCfg)
	if err != nil {
		log.Errorf("fail to create polaris SDKContext, err: %v", err)
		return err
	}
	var locationProviders []*config.LocationProviderConfigImpl
	if sdkCtx.GetConfig() != nil && sdkCtx.GetConfig().GetGlobal() != nil &&
		sdkCtx.GetConfig().GetGlobal().GetLocation() != nil {
		locationProviders = sdkCtx.GetConfig().GetGlobal().GetLocation().GetProviders()
	}
	var routerChain []string
	var matchLevel string
	if sdkCtx.GetConfig() != nil && sdkCtx.GetConfig().GetConsumer() != nil &&
		sdkCtx.GetConfig().GetConsumer().GetServiceRouter() != nil {
		routerChain = sdkCtx.GetConfig().GetConsumer().GetServiceRouter().GetChain()
		if sdkCtx.GetConfig().GetConsumer().GetServiceRouter().GetNearbyConfig() != nil {
			matchLevel = sdkCtx.GetConfig().GetConsumer().GetServiceRouter().GetNearbyConfig().GetMatchLevel()
		}
	}
	log.Infof("Using location provider: %s, chain:%s, matchLevel:%s", utils.JsonString(locationProviders),
		utils.JsonString(routerChain), matchLevel)
	SDKContext = sdkCtx
	return nil
}

func GetConsumerAPI() (polarisgo.ConsumerAPI, error) {
	if SDKContext == nil {
		log.Errorf("GetConsumerAPI failed for polaris SDKContext is nil")
		return nil, errors.New("polaris SDKContext is nil")
	}
	return polarisgo.NewConsumerAPIByContext(SDKContext), nil
}

func GetLimitAPI() (polarisgo.LimitAPI, error) {
	if SDKContext == nil {
		log.Errorf("GetLimitAPI failed for polaris SDKContext is nil")
		return nil, errors.New("polaris SDKContext is nil")
	}
	return polarisgo.NewLimitAPIByContext(SDKContext), nil
}
