package resolver

import (
	"context"
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
	log.Debugf("[resolver] input question name %s", qname)
	if d.recurseProxy == nil && len(d.recurseProxy.GetSearch()) == 0 {
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
	// questions length is 0, send refused
	if len(req.Question) == 0 {
		common.WriteDnsCode(d.protocol, w, req, dns.RcodeRefused)
		return
	}
	// questions type we only accept
	question := req.Question[0]
	qname := d.Preprocess(question.Name)
	log.Debugf("[resolver] input question name %s, after Preprocess name %s", question.Name, qname)
	ctx := context.WithValue(context.Background(), constants.ContextProtocol, d.protocol)
	var resp *dns.Msg
	for _, handler := range d.resolvers {
		resp = handler.ServeDNS(ctx, question, qname)
		if nil != resp {
			log.Infof("[resolver] request %v, response for %s is %v", req, question.Name, resp)
			common.WriteDnsResponse(d.protocol, w, req, resp)
			return
		}
	}
	if d.recurseProxy == nil {
		log.Errorf("[resolver] empty result from polaris, recurse is not enabled, request %v, response for %s is nil",
			req, question.Name)
		common.WriteDnsCode(d.protocol, w, req, dns.RcodeServerFailure)
	}
	// 降级到本地 nameserver
	d.recurseProxy.HandleDNS(d.protocol, w, req)
}
