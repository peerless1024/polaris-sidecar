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

package dnsagent

import (
	"context"
	"encoding/hex"
	"fmt"
	"net"

	"github.com/miekg/dns"
	"github.com/polarismesh/polaris-go"
	"github.com/polarismesh/polaris-go/pkg/model"
	"go.uber.org/zap"

	debughttp "github.com/polarismesh/polaris-sidecar/internal/debugger"
	"github.com/polarismesh/polaris-sidecar/internal/resolver/common"
	"github.com/polarismesh/polaris-sidecar/pkg/constants"
	"github.com/polarismesh/polaris-sidecar/pkg/log"
	polarisApi "github.com/polarismesh/polaris-sidecar/pkg/polaris"
	"github.com/polarismesh/polaris-sidecar/pkg/utils"
)

func init() {
	common.Register(&resolverDiscovery{})
}

const name = common.PluginNameDnsAgent

type resolverDiscovery struct {
	consumer  polaris.ConsumerAPI
	suffix    string
	dnsTtl    int
	config    *resolverConfig
	namespace string
}

// Name will return the name to resolver
func (r *resolverDiscovery) Name() string {
	return name
}

func (r *resolverDiscovery) String() string {
	return utils.JsonString(r)
}

// Initialize will init the resolver on startup
func (r *resolverDiscovery) Initialize(c *common.ConfigEntry) error {
	var err error
	defer func() {
		if nil != err {
			log.Errorf("[dnsagent] fail to init resolver %s, err: %v", name, err)
		}
	}()
	r.config, err = parseOptions(c.Option)
	if nil != err {
		return err
	}
	r.consumer, err = polarisApi.GetConsumerAPI()
	if nil != err {
		return err
	}
	r.suffix = utils.AddQuota(c.Suffix)
	r.dnsTtl = c.DnsTtl
	r.namespace = c.Namespace
	return nil
}

// Start the plugin runnable
func (r *resolverDiscovery) Start(context.Context) {
	log.Infof("[dnsagent] %s resolver started", name)
}

func (r *resolverDiscovery) Debugger() []debughttp.DebugHandler {
	return []debughttp.DebugHandler{}
}

// Destroy will destroy the resolver on shutdown
func (r *resolverDiscovery) Destroy() {
	if nil != r.consumer {
		r.consumer.Destroy()
		log.Infof("[dnsagent] %s resolver polaris consumerAPI destroyed", name)
	}
}

// ServeDNS is like dns.Handler except ServeDNS may return an rcode
// and/or error.
// If ServeDNS writes to the response body, it should return a status
// code. Resolvers assumes *no* reply has yet been written if the status
// code is one of the following:
//
// * SERVFAIL (dns.RcodeServerFailure)
//
// * REFUSED (dns.RecodeRefused)
//
// * NOTIMP (dns.RcodeNotImplemented)
func (r *resolverDiscovery) ServeDNS(ctx context.Context, question dns.Question, qname string) *dns.Msg {
	protocol := ctx.Value(constants.ContextProtocol)

	msg := &dns.Msg{}
	labels := dns.SplitDomainName(qname)
	for i := range labels {
		if labels[i] == "_addr" {
			ret, err := hex.DecodeString(labels[i-1])
			if err != nil {
				log.Error("[dnsagent] decode ip str fail", zap.String("domain", qname), zap.Error(err))
				return nil
			}
			rr := r.markRecord(question, net.IP(ret), nil)
			msg.Answer = append(msg.Answer, rr)
			log.Infof("[dnsagent] serve dns for %s, protocol: %s, ip: %s", qname, protocol, net.IP(ret).String())
			return msg
		}
	}

	instances, err := r.lookupFromPolaris(qname, r.namespace)
	if err != nil || len(instances) == 0 {
		return nil
	}

	//do reorder and unique
	for i := range instances {
		ins := instances[i]
		rr := r.markRecord(question, net.ParseIP(ins.GetHost()), ins)
		msg.Answer = append(msg.Answer, rr)
	}

	msg.Rcode = dns.RcodeSuccess

	return msg
}

func (r *resolverDiscovery) lookupFromPolaris(qname string, currentNs string) ([]model.Instance, error) {
	svcKey := utils.ParseQname(qname, r.suffix, currentNs)
	if nil == svcKey {
		log.Errorf("[dnsagent] fail to parse qname %s, namespace: %s, suffix:%s", qname, currentNs, r.suffix)
		return nil, nil
	}
	request := &polaris.GetOneInstanceRequest{}
	request.Namespace = svcKey.Namespace
	request.Service = svcKey.Service
	if len(r.config.RouteLabelsMap) > 0 {
		request.SourceService = &model.ServiceInfo{Metadata: r.config.RouteLabelsMap}
	}
	resp, err := r.consumer.GetOneInstance(request)
	if nil != err {
		log.Errorf("[dnsagent] fail to lookup service %s, err: %v, req:%s", *svcKey, err, utils.JsonString(request))
		return nil, err
	}
	log.Infof("[dnsagent] lookup service %s success, resp: %v, req:%s", *svcKey, utils.JsonString(resp.GetInstances()),
		utils.JsonString(request))
	return resp.GetInstances(), nil
}

func encodeIPAsFqdn(ip net.IP, svcKey model.ServiceKey) string {
	respDomain := fmt.Sprintf("%s._addr.%s.%s", hex.EncodeToString(ip), svcKey.Service, svcKey.Namespace)
	return dns.Fqdn(respDomain)
}

func (r *resolverDiscovery) markRecord(question dns.Question, address net.IP, ins model.Instance) dns.RR {

	var rr dns.RR

	qname := question.Name

	switch question.Qtype {
	case dns.TypeA:
		rr = &dns.A{
			Hdr: dns.RR_Header{Name: qname, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: uint32(r.dnsTtl)},
			A:   address,
		}
	case dns.TypeSRV:
		if ins == nil {
			return rr
		}

		rr = &dns.SRV{
			Hdr:      dns.RR_Header{Name: qname, Rrtype: dns.TypeSRV, Class: dns.ClassINET, Ttl: uint32(r.dnsTtl)},
			Priority: uint16(ins.GetPriority()),
			Weight:   uint16(ins.GetWeight()),
			Port:     uint16(ins.GetPort()),
			Target:   encodeIPAsFqdn(address, ins.GetInstanceKey().ServiceKey),
		}
	case dns.TypeAAAA:
		rr = &dns.AAAA{
			Hdr:  dns.RR_Header{Name: qname, Rrtype: dns.TypeAAAA, Class: dns.ClassINET, Ttl: uint32(r.dnsTtl)},
			AAAA: address,
		}
	}
	return rr
}
