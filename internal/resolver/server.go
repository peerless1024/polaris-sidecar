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

package resolver

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/miekg/dns"

	debughttp "github.com/polarismesh/polaris-sidecar/internal/debugger"
	"github.com/polarismesh/polaris-sidecar/internal/resolver/common"
	_ "github.com/polarismesh/polaris-sidecar/internal/resolver/dnsagent"
	_ "github.com/polarismesh/polaris-sidecar/internal/resolver/meshproxy"
	"github.com/polarismesh/polaris-sidecar/internal/resolver/recursor"
	"github.com/polarismesh/polaris-sidecar/pkg/constants"
	"github.com/polarismesh/polaris-sidecar/pkg/log"
)

func NewServer(conf *common.ResolverConfig, recurseProxyConf *recursor.Config) (*Server, error) {
	namingResolvers := make([]common.NamingResolver, 0, len(conf.Resolvers))
	for _, resolverCfg := range conf.Resolvers {
		if !resolverCfg.Enable {
			log.Infof("[resolver] resolver %s is not enabled", resolverCfg.Name)
			continue
		}
		handler := common.NameResolver(resolverCfg.Name)
		if nil == handler {
			log.Errorf("[resolver] resolver %s is not found", resolverCfg.Name)
			return nil, fmt.Errorf("fail to lookup resolver %s, consider it's not registered", resolverCfg.Name)
		}
		if err := handler.Initialize(resolverCfg); nil != err {
			for _, initHandler := range namingResolvers {
				initHandler.Destroy()
			}
			log.Errorf("[resolver] fail to init resolver %s, err: %v", resolverCfg.Name, err)
			return nil, err
		}
		log.Infof("[resolver] finished to init resolver %s", resolverCfg.Name)
		namingResolvers = append(namingResolvers, handler)
	}
	recurseProxy := recursor.BuildProxy(recurseProxyConf)
	udpServer := &dns.Server{
		Addr: conf.BindIP + constants.ColonSymbol + strconv.FormatUint(uint64(conf.BindPort), 10),
		Net:  constants.UdpProtocol,
		Handler: buildDnsHandler(
			constants.UdpProtocol,
			namingResolvers,
			recurseProxy,
		),
	}
	tcpServer := &dns.Server{
		Addr: conf.BindIP + constants.ColonSymbol + strconv.FormatUint(uint64(conf.BindPort), 10),
		Net:  constants.TcpProtocol,
		Handler: buildDnsHandler(
			constants.TcpProtocol,
			namingResolvers,
			recurseProxy,
		),
	}
	return &Server{
		dnsSeverList: []*dns.Server{udpServer, tcpServer},
		resolvers:    namingResolvers,
	}, nil
}

type Server struct {
	dnsSeverList []*dns.Server
	resolvers    []common.NamingResolver
	once         sync.Once
}

func (svr *Server) Run(ctx context.Context, errChan chan error) {
	log.Infof("[resolver] start to run resolver")
	defer func() {
		svr.Destroy()
	}()

	// 启动解析器
	for _, handler := range svr.resolvers {
		handler.Start(ctx)
		log.Infof("[resolver] success to start resolver %s", handler.Name())
	}

	// 使用 WaitGroup 管理 DNS 服务器 goroutine
	var wg sync.WaitGroup
	serveErr := make(chan error, len(svr.dnsSeverList))

	// 创建子上下文用于控制 DNS 服务器
	dnsCtx, cancelDNS := context.WithCancel(ctx)
	defer cancelDNS()

	for i := range svr.dnsSeverList {
		wg.Add(1)
		go func(dnsSvr *dns.Server) {
			defer wg.Done()

			// 使用通道监听服务器启动结果
			serveResult := make(chan error, 1)
			go func() {
				serveResult <- dnsSvr.ListenAndServe()
			}()

			// 等待服务器结果或上下文取消
			select {
			case err := <-serveResult:
				if err != nil {
					log.Errorf("[resolver] fail to start dns server %s %s, err: %v", dnsSvr.Addr, dnsSvr.Net, err)
					serveErr <- err
				} else {
					log.Infof("[resolver] success to start dns server %s %s", dnsSvr.Addr, dnsSvr.Net)
				}
			case <-dnsCtx.Done():
				log.Infof("[resolver] context canceled, shutting down dns server %s %s", dnsSvr.Addr, dnsSvr.Net)
				if err := dnsSvr.Shutdown(); err != nil {
					log.Warnf("[resolver] force shutdown dns server %s %s: %v", dnsSvr.Addr, dnsSvr.Net, err)
				}
				// 确保从 serveResult 通道读取结果
				var err error
				err = <-serveResult
				log.Infof("[resolver] dns server %s %s shutdown: %v", dnsSvr.Addr, dnsSvr.Net, err)
			}
		}(svr.dnsSeverList[i])
	}

	// 等待所有 goroutine 完成或收到关闭信号
	select {
	case sErr := <-serveErr:
		select {
		case errChan <- sErr: // 尝试发送
		default: // 通道满时记录日志
			log.Errorf("[resolver] error channel full, drop error: %v", sErr)
		}
		cancelDNS() // 取消所有 DNS 服务器
	case <-ctx.Done():
		log.Infof("[resolver] get context cancel signal")
		cancelDNS() // 取消所有 DNS 服务器
	}

	// 设置等待超时（防止永久阻塞）
	waitDone := make(chan struct{})
	go func() {
		defer close(waitDone)
		wg.Wait()
	}()

	select {
	case <-waitDone:
		log.Infof("[resolver] all dns servers exited")
	case <-time.After(30 * time.Second):
		log.Warnf("[resolver] timeout waiting for dns servers to exit, forcing shutdown")
	}
}

// Destroy 销毁
func (svr *Server) Destroy() {
	svr.once.Do(func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		var wg sync.WaitGroup
		// 关闭 DNS 服务器
		for _, dnsSvr := range svr.dnsSeverList {
			wg.Add(1)
			go func(s *dns.Server) {
				defer wg.Done()
				if err := s.ShutdownContext(shutdownCtx); err != nil {
					log.Errorf("[resolver] fail to stop dns server %s %s, err: %v", s.Addr, s.Net, err)
				}
			}(dnsSvr)
		}
		wg.Wait()
		// 销毁解析器
		for _, handler := range svr.resolvers {
			handler.Destroy()
		}
		log.Infof("[resolver] success to stop all services")
	})
}

func (svr *Server) Debugger() []debughttp.DebugHandler {
	ret := make([]debughttp.DebugHandler, 0, 8)
	for i := range svr.resolvers {
		ret = append(ret, svr.resolvers[i].Debugger()...)
	}
	return ret
}
