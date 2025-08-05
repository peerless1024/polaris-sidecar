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
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/miekg/dns"

	debughttp "github.com/polarismesh/polaris-sidecar/internal/debugger"
	"github.com/polarismesh/polaris-sidecar/pkg/log"
	"github.com/polarismesh/polaris-sidecar/pkg/recursor"
	"github.com/polarismesh/polaris-sidecar/pkg/utils"
)

// TODO 解析配置文件增加其他参数
func NewServers(conf *ResolverConfig) (*Server, error) {
	namingResolvers := make([]NamingResolver, 0, len(conf.Resolvers))
	for _, resolverCfg := range conf.Resolvers {
		if !resolverCfg.Enable {
			log.Infof("[resolver] resolver %s is not enabled", resolverCfg.Name)
			continue
		}
		name := resolverCfg.Name
		handler := NameResolver(name)
		if nil == handler {
			log.Errorf("[resolver] resolver %s is not found", resolverCfg.Name)
			return nil, fmt.Errorf("fail to lookup resolver %s, consider it's not registered", name)
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

	resolvConfig, err := recursor.ParseResolvConf(conf.BindLocalhost, conf.Recurse.NameServers)
	if nil != err {
		log.Errorf("[resolver] ParseResolvConf err: %v", err)
		return nil, err
	}
	udpServer := &dns.Server{
		Addr: conf.BindIP + utils.ColonSep + strconv.FormatUint(uint64(conf.BindPort), 10), Net: "udp",
		Handler: buildDNSServer(
			recursor.UdpProtocol,
			namingResolvers,
			resolvConfig,
			conf.Recurse.Enable,
		),
	}
	tcpServer := &dns.Server{
		Addr: conf.BindIP + utils.ColonSep + strconv.FormatUint(uint64(conf.BindPort), 10), Net: "tcp",
		Handler: buildDNSServer(
			recursor.TcpProtocol,
			namingResolvers,
			resolvConfig,
			conf.Recurse.Enable,
		),
	}

	return &Server{
		dnsSeverList: []*dns.Server{udpServer, tcpServer},
		resolvers:    namingResolvers,
	}, nil
}

type Server struct {
	dnsSeverList []*dns.Server
	resolvers    []NamingResolver
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

func buildDNSServer(protocol string, resolvers []NamingResolver, resolveConfig *dns.ClientConfig,
	recurseEnable bool) *dnsServer {
	return &dnsServer{
		protocol:      protocol,
		resolvers:     resolvers,
		recurseEnable: recurseEnable,
		resolveConfig: resolveConfig,
	}
}

type dnsServer struct {
	protocol      string
	resolvers     []NamingResolver
	resolveConfig *dns.ClientConfig
	recurseEnable bool
}

// Preprocess 在容器环境中会用到
func (d *dnsServer) Preprocess(qname string) string {
	log.Debugf("[resolver] input question name %s", qname)
	if len(d.resolveConfig.Search) == 0 {
		return qname
	}

	for _, searchName := range d.resolveConfig.Search {
		if strings.HasSuffix(qname, searchName) {
			processed := qname[:len(qname)-len(searchName)]
			if processed == "" {
				return qname // 避免返回空字符串
			}
			return processed
		}
	}

	return qname
}

func (d *dnsServer) sendDnsCode(w dns.ResponseWriter, r *dns.Msg, code int) {
	msg := &dns.Msg{}
	msg.SetReply(r)
	msg.RecursionDesired = true
	msg.RecursionAvailable = true
	msg.Rcode = code
	msg.Truncate(size(d.protocol, r))
	if edns := r.IsEdns0(); edns != nil {
		setEDNS(r, msg, true)
	}
	err := w.WriteMsg(msg)
	if nil != err {
		log.Errorf("[resolver] fail to write dns response message, err: %v", err)
	}
}

func (d *dnsServer) sendDnsResponse(w dns.ResponseWriter, r *dns.Msg, msg *dns.Msg) {
	msg.SetReply(r)
	msg.Truncate(size(d.protocol, r))
	if edns := r.IsEdns0(); edns != nil {
		setEDNS(r, msg, true)
	}
	err := w.WriteMsg(msg)
	if nil != err {
		log.Errorf("[resolver] fail to write dns response message, err: %v", err)
	}
}

// ServeDNS handler callback
func (d *dnsServer) ServeDNS(w dns.ResponseWriter, req *dns.Msg) {
	// questions length is 0, send refused
	if len(req.Question) == 0 {
		d.sendDnsCode(w, req, dns.RcodeRefused)
	}
	// questions type we only accept
	question := req.Question[0]
	qname := d.Preprocess(question.Name)
	log.Debugf("[resolver] input question name %s, after Preprocess name %s", question.Name, qname)
	ctx := context.WithValue(context.Background(), utils.ContextProtocol, d.protocol)
	var resp *dns.Msg
	for _, handler := range d.resolvers {
		resp = handler.ServeDNS(ctx, question, qname)
		if nil != resp {
			log.Infof("[resolver] request %v, response for %s is %v", req, question.Name, resp)
			d.sendDnsResponse(w, req, resp)
			return
		}
	}
	if !d.recurseEnable {
		log.Errorf("[resolver] empty result from polaris, recurse is not enabled, request %v, response for %s is nil",
			req, question.Name)
		d.sendDnsCode(w, req, dns.RcodeServerFailure)
	}
	// 降级到本地 nameserver
	d.handleRecurse(w, req)
}

// handleRecurse is used to handle recursive DNS queries
func (d *dnsServer) handleRecurse(resp dns.ResponseWriter, req *dns.Msg) {
	q := req.Question[0]
	network := "udp"
	defer func(s time.Time) {
		log.Infof("[resolver] request served from polaris, "+
			"question: %s, network: %s, latency: %s, polaris: %s, client_network: %s",
			q.String(), network, time.Since(s).String(), resp.RemoteAddr().String(), resp.RemoteAddr().Network())
	}(time.Now())

	// Switch to TCP if the polaris is
	if _, ok := resp.RemoteAddr().(*net.TCPAddr); ok {
		network = "tcp"
	}

	// Recursively resolve
	c := &dns.Client{Net: network, Timeout: time.Duration(d.resolveConfig.Timeout) * time.Second}
	var r *dns.Msg
	var rtt time.Duration
	var err error
	// TODO: 增加对 ndots 和 options 配置的处理
	for _, recursor := range d.resolveConfig.Servers {
		r, rtt, err = c.Exchange(req, recursor)
		// 只要是 0（NOERROR） 或 3（NXDOMAIN），resolver 不会轮询
		if r != nil && (r.Rcode != dns.RcodeSuccess && r.Rcode != dns.RcodeNameError) {
			log.Warnf("[resolver] recurse failed for question, question: %s, rtt: %s, recursor: %s, rcode: %s",
				q.String(), rtt, recursor, dns.RcodeToString[r.Rcode])
			// If we still have recursors to forward the query to,
			// we move forward onto the next one else the loop ends
			continue
		} else if err == nil || (r != nil && r.Truncated) {
			// 当r.Truncated为true时，即使响应被截断，也视为成功响应。服务器会转发这个被截断的响应给客户端
			// 客户端负责使用TCP重新查询以获取完整响应
			// Forward the response
			log.Infof("[resolver] recurse succeeded for question, question: %s, rtt: %s, recursor: %s",
				q.String(), rtt, recursor)
			if err := resp.WriteMsg(r); err != nil {
				log.Warnf("failed to respond, error: %v", err)
			}
			return
		}
		log.Errorf("[resolver] recurse failed, error: %v", err)
	}

	// If all resolvers fail, return a SERVFAIL message
	log.Errorf(
		"[resolver] all resolvers failed for question from polaris, question: %s, polaris: %s, client_network: %s",
		q.String(), resp.RemoteAddr().String(), resp.RemoteAddr().Network())
	d.sendDnsCode(resp, req, dns.RcodeServerFailure)
}

// Size returns if buffer size *advertised* in the requests OPT record.
// Or when the request was over TCP, we return the maximum allowed size of 64K.
func size(proto string, r *dns.Msg) int {
	size := uint16(0)
	if o := r.IsEdns0(); o != nil {
		size = o.UDPSize()
	}

	// normalize size
	size = ednsSize(proto, size)
	return int(size)
}

// ednsSize returns a normalized size based on proto.
func ednsSize(proto string, size uint16) uint16 {
	if proto == "tcp" {
		return dns.MaxMsgSize
	}
	if size < dns.MinMsgSize {
		return dns.MinMsgSize
	}
	return size
}

func ednsSubnetForRequest(req *dns.Msg) *dns.EDNS0_SUBNET {
	// IsEdns0 returns the EDNS RR if present or nil otherwise
	edns := req.IsEdns0()

	if edns == nil {
		return nil
	}

	for _, o := range edns.Option {
		if subnet, ok := o.(*dns.EDNS0_SUBNET); ok {
			return subnet
		}
	}

	return nil
}

// setEDNS is used to set the responses EDNS size headers and
// possibly the ECS headers as well if they were present in the
// original request
func setEDNS(request *dns.Msg, response *dns.Msg, ecsGlobal bool) {
	edns := request.IsEdns0()
	if edns == nil {
		return
	}

	// cannot just use the SetEdns0 function as we need to embed
	// the ECS option as well
	ednsResp := new(dns.OPT)
	ednsResp.Hdr.Name = "."
	ednsResp.Hdr.Rrtype = dns.TypeOPT
	ednsResp.SetUDPSize(edns.UDPSize())

	// Setup the ECS option if present
	if subnet := ednsSubnetForRequest(request); subnet != nil {
		subOp := new(dns.EDNS0_SUBNET)
		subOp.Code = dns.EDNS0SUBNET
		subOp.Family = subnet.Family
		subOp.Address = subnet.Address
		subOp.SourceNetmask = subnet.SourceNetmask
		if c := response.Rcode; ecsGlobal || c == dns.RcodeNameError || c == dns.RcodeServerFailure ||
			c == dns.RcodeRefused || c == dns.RcodeNotImplemented {
			// reply is globally valid and should be cached accordingly
			subOp.SourceScope = 0
		} else {
			// reply is only valid for the subnet it was queried with
			subOp.SourceScope = subnet.SourceNetmask
		}
		ednsResp.Option = append(ednsResp.Option, subOp)
	}

	response.Extra = append(response.Extra, ednsResp)
}
