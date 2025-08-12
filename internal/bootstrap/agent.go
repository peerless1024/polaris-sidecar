/**
 * Tencent is pleased to support the open source community by making CL5 available.
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

package bootstrap

import (
	"context"
	"sync"

	"github.com/polarismesh/polaris-sidecar/internal/bootstrap/config"
	debughttp "github.com/polarismesh/polaris-sidecar/internal/debugger"
	"github.com/polarismesh/polaris-sidecar/internal/mesh/metrics"
	mtlsAgent "github.com/polarismesh/polaris-sidecar/internal/mesh/mtls"
	"github.com/polarismesh/polaris-sidecar/internal/mesh/rls"
	"github.com/polarismesh/polaris-sidecar/internal/resolver"
	"github.com/polarismesh/polaris-sidecar/pkg/log"
)

// Agent provide the listener to dns server
type Agent struct {
	dnsResolver  *resolver.Server
	debugServer  *debughttp.DebugServer
	metricServer *metrics.Server
	mtlsAgent    *mtlsAgent.Agent
	rlsSvr       *rls.RateLimitServer
}

func initAgent(configFilePath string, bootConfig *config.BootConfig) (*Agent, error) {
	agent := &Agent{}
	sidecarConfig, err := config.InitConfig(configFilePath, bootConfig)
	if nil != err {
		log.Errorf("[bootstrap] fail to parse sidecar config, err: %v", err)
		return nil, err
	}
	if err = log.Configure(sidecarConfig.Logger); err != nil {
		log.Errorf("[bootstrap] fail to init log config, err: %v", err)
		return nil, err
	}
	if err = sidecarConfig.InitPolarisApi(); err != nil {
		return nil, err
	}
	agent.dnsResolver, err = sidecarConfig.InitDnsResolver()
	if err != nil {
		return nil, err
	}
	agent.debugServer, err = sidecarConfig.InitDebugServer(agent.dnsResolver)
	if err != nil {
		return nil, err
	}
	agent.metricServer = sidecarConfig.InitMeshMetrics()
	agent.rlsSvr = sidecarConfig.InitMeshRatelimit()
	agent.mtlsAgent, err = sidecarConfig.InitMeshMtls()
	if err != nil {
		return nil, err
	}
	return agent, nil
}

func (p *Agent) runServices(ctx context.Context, wg *sync.WaitGroup, errChan chan error) {
	// 启动所有组件
	if p.debugServer != nil {
		go p.debugServer.Run(ctx, wg, errChan)
	}
	if p.dnsResolver != nil {
		go p.dnsResolver.Run(ctx, wg, errChan)
	}
	if p.mtlsAgent != nil {
		go p.mtlsAgent.Run(ctx, wg, errChan)
	}
	if p.metricServer != nil {
		go p.metricServer.Run(ctx, wg, errChan)
	}
	if p.rlsSvr != nil {
		go p.rlsSvr.Run(ctx, wg, errChan)
	}
}

func (p *Agent) getErrorChannel() chan error {
	// 使用带缓冲的错误通道（容量等于service数量）
	componentCount := 0
	if p.debugServer != nil {
		componentCount++
	}
	if p.dnsResolver != nil {
		componentCount = componentCount + 2
	}
	if p.mtlsAgent != nil {
		componentCount++
	}
	if p.metricServer != nil {
		componentCount++
	}
	if p.rlsSvr != nil {
		componentCount++
	}

	return make(chan error, componentCount)
}
