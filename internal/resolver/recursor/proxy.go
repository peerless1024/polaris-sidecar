package recursor

import (
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/miekg/dns"

	"github.com/polarismesh/polaris-sidecar/pkg/constants"
	"github.com/polarismesh/polaris-sidecar/pkg/log"
	"github.com/polarismesh/polaris-sidecar/pkg/utils"
)

type Proxy struct {
	config Config
	rotate *RotatingUpstream
}

type RotatingUpstream struct {
	servers []string
	index   int
	mu      sync.Mutex
}

func BuildProxy(r *Config) *Proxy {
	if r == nil {
		return nil
	}
	return &Proxy{
		config: *r,
		rotate: &RotatingUpstream{
			servers: r.Upstream,
		},
	}
}

func (p *Proxy) GetSearch() []string {
	return p.config.Search
}

func (r *RotatingUpstream) Next() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	server := r.servers[r.index]
	r.index = (r.index + 1) % len(r.servers)
	return server
}

// HandleDNS 降级到本地代理处理DNS请求
func (p *Proxy) HandleDNS(protocol string, w dns.ResponseWriter, r *dns.Msg) *dns.Msg {
	if p == nil {
		log.Infof("[recursor] recursor is not configured, return nil")
		return nil
	}
	startTime := time.Now()
	clientAddr := w.RemoteAddr()
	q := r.Question[0]
	// 确定协议类型
	network := constants.UdpProtocol
	if _, isTCP := clientAddr.(*net.TCPAddr); isTCP {
		network = constants.TcpProtocol
	}
	// 根据 ndots 和 search 配置生成带解析域名列表
	domains := p.expandQuery(q.Name)
	log.Infof("[recursor] expand query for %s, get domains: %v", q.Name, domains)
	// 创建DNS客户端
	client := &dns.Client{Net: network, Timeout: time.Duration(p.config.Timeout) * time.Second}
	// 开始解析
	var lastNXDomainResp *dns.Msg
	for _, domain := range domains {
		req := r.Copy()
		req.Question[0].Name = domain
		// 尝试请求配置的DNS服务器
		for i := 0; i < p.config.Attempts; i++ {
			upstream := p.rotate.Next()
			resp, rtt, err := client.Exchange(req, upstream)
			resInfo := fmt.Sprintf("upstream: %s, rtt: %s, err:%v, question: %s, code:%s，protocol: %s,"+
				"client_addr: %s, network:%s, latency: %s", upstream, rtt, err, req.Question[0].String(),
				getDnsMsgCode(resp), protocol, clientAddr.String(), network, time.Since(startTime).String())
			switch {
			case resp != nil && resp.Rcode == dns.RcodeNameError:
				// NXDOMAIN：域名不存在，记录日志并继续尝试下一个 search domain
				log.Infof("[recursor] got NXDOMAIN for %s, try next search domain, info:%s", domain, resInfo)
				lastNXDomainResp = resp
			case resp != nil && resp.Rcode != dns.RcodeSuccess:
				// 如果返回的响应码不是NOERROR（0查询成功），则仅记录日志，尝试下一个DNS服务器
				log.Warnf("[recursor] need retry for dns rcode not pass, info:%s", resInfo)
			case err == nil || (resp != nil && resp.Truncated):
				// 如果没有错误，或者有错误但是响应被截断，都返回响应，并退出循环
				// 客户端如果感知到响应被截断，会自动切换成 TCP 协议重试
				log.Infof("[recursor] return for query succeeded, info:%s, ", resInfo)
				return resp
			default:
				log.Warnf("[recursor] need retry for query failed, info:%s", resInfo)
			}
			if resp == nil || resp.Rcode == dns.RcodeNameError {
				// NXDOMAIN 跳出 attempts 循环，直接尝试下一个 search domain
				break
			}
			log.Errorf("nameserver %s query %s failed (try %d times), err:%v, config:%s", upstream, domain, i+1, err,
				p.config.String())
		}
	}
	// 所有 search domain 都返回 NXDOMAIN，返回最后一个 NXDOMAIN 响应
	if lastNXDomainResp != nil {
		log.Infof("[recursor] all search domains returned NXDOMAIN for %s", q.Name)
		return lastNXDomainResp
	}
	return nil
}

func (p *Proxy) expandQuery(name string) []string {
	ndots := p.config.Ndots
	search := p.config.Search
	if strings.Count(name, constants.DotSymbol) < ndots {
		expanded := make([]string, 0, len(search))
		for _, suffix := range search {
			expanded = append(expanded, utils.AddQuota(name+suffix))
		}
		return expanded
	}
	return []string{name}
}

func getDnsMsgCode(r *dns.Msg) string {
	if r == nil {
		return "<nil>"
	}
	return fmt.Sprintf("code:%s", dns.RcodeToString[r.Rcode])
}
