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

func (svr *Server) Run(ctx context.Context, wg *sync.WaitGroup, errChan chan error) {
	if svr == nil {
		log.Infof("[resolver] resolver is nil, return")
		return
	}
	log.Infof("[resolver] start to run resolver")
	wg.Add(1)
	defer func() {
		svr.Destroy()
		wg.Done()
	}()
	for _, handler := range svr.resolvers {
		handler.Start(ctx)
	}
	for i := range svr.dnsSeverList {
		go func(dnsSvr *dns.Server) {
			log.Infof("[resolver] dns server listening %s %s", dnsSvr.Addr, dnsSvr.Net)
			errChan <- dnsSvr.ListenAndServe()
		}(svr.dnsSeverList[i])
	}
	<-ctx.Done()
	log.Infof("[resolver] get context cancel signal, return")
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
