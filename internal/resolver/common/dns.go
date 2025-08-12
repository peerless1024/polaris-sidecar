package common

import (
	"github.com/miekg/dns"

	"github.com/polarismesh/polaris-sidecar/pkg/constants"
	"github.com/polarismesh/polaris-sidecar/pkg/log"
)

// WriteDnsCode 失败时返回响应码
func WriteDnsCode(protocol string, w dns.ResponseWriter, r *dns.Msg, code int) {
	msg := &dns.Msg{}
	msg.SetReply(r)
	msg.RecursionDesired = true
	msg.RecursionAvailable = true
	msg.Rcode = code
	msg.Truncate(size(protocol, r))
	if edns := r.IsEdns0(); edns != nil {
		setEDNS(r, msg, true)
		log.Infof("[resolver] write dns response message with edns0")
	}
	log.Errorf("[resolver] dns resolve failed, code: %s, req:%s, resp:%s",
		dns.RcodeToString[code], r.String(), msg.String())
	err := w.WriteMsg(msg)
	if nil != err {
		log.Errorf("[resolver] fail to write dns response message, err: %v", err)
	}
}

// WriteDnsResponse 成功时返回响应
func WriteDnsResponse(protocol string, w dns.ResponseWriter, r *dns.Msg, msg *dns.Msg) {
	msg.SetReply(r)
	msg.Authoritative = true
	// nslookup 默认会发送递归请求，这里需要设置为可递归, 否则会导致nslookup请求失败
	msg.RecursionAvailable = true
	msg.Truncate(size(protocol, r))
	if edns := r.IsEdns0(); edns != nil {
		setEDNS(r, msg, true)
	}
	log.Infof("[resolver] dns resolve succeed, code: %s, req:%s, resp:%s",
		dns.RcodeToString[msg.Rcode], r.String(), msg.String())
	err := w.WriteMsg(msg)
	if nil != err {
		log.Errorf("[resolver] fail to write dns response message, err: %v", err)
	}
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
	if proto == constants.TcpProtocol {
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
