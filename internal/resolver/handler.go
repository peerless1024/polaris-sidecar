package resolver

import (
	"context"
	"runtime/debug"
	"strings"

	"github.com/miekg/dns"

	"github.com/polarismesh/polaris-sidecar/internal/resolver/common"
	"github.com/polarismesh/polaris-sidecar/internal/resolver/recursor"
	"github.com/polarismesh/polaris-sidecar/pkg/constants"
	"github.com/polarismesh/polaris-sidecar/pkg/log"
)

func buildDnsHandler(protocol string, resolvers []common.NamingResolver, recurseProxy *recursor.Proxy) *dnsHandler {
	return &dnsHandler{
		protocol:     protocol,
		resolvers:    resolvers,
		recurseProxy: recurseProxy,
	}
}

type dnsHandler struct {
	protocol     string
	resolvers    []common.NamingResolver
	recurseProxy *recursor.Proxy
}

// Preprocess removes the search suffix from the query name if it is present.
func (d *dnsHandler) Preprocess(qname string) string {
	if d.recurseProxy == nil || len(d.recurseProxy.GetSearch()) == 0 {
		return qname
	}
	for _, searchName := range d.recurseProxy.GetSearch() {
		if !strings.HasSuffix(searchName, constants.DotSymbol) {
			searchName += constants.DotSymbol
		}
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

// ServeDNS handler callback
func (d *dnsHandler) ServeDNS(w dns.ResponseWriter, req *dns.Msg) {
	defer func() {
		if r := recover(); r != nil {
			stack := debug.Stack()
			log.Errorf("[resolver] agent panic recovered: %v\nStack trace:\n%s", r, string(stack))
		}
	}()
	// questions length is 0, send refused
	if len(req.Question) == 0 {
		common.WriteDnsCode(d.protocol, w, req, dns.RcodeRefused)
		return
	}
	// questions type we only accept
	question := req.Question[0]
	if canDoResolve(question.Qtype) {
		qname := d.Preprocess(question.Name)
		log.Infof("[resolver] qname %s, raw question name：%s", qname, question.Name)
		ctx := context.WithValue(context.Background(), constants.ContextProtocol, d.protocol)
		for _, handler := range d.resolvers {
			resp := handler.ServeDNS(ctx, question, qname)
			if nil != resp {
				common.WriteDnsResponse(d.protocol, w, req, resp)
				return
			}
		}
	}
	// 降级到本地 nameserver
	resp := d.recurseProxy.HandleDNS(d.protocol, w, req)
	if nil != resp {
		common.WriteDnsResponse(d.protocol, w, req, resp)
		return
	}
	common.WriteDnsCode(d.protocol, w, req, dns.RcodeServerFailure)
}

func canDoResolve(qType uint16) bool {
	if qType == dns.TypeA {
		return true
	}
	if qType == dns.TypeAAAA {
		return true
	}
	if qType == dns.TypeSRV {
		return true
	}

	return false
}
