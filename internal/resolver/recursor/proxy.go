package recursor

import (
	"net"
	"strings"
	"sync"
	"time"

	"github.com/miekg/dns"

	"github.com/polarismesh/polaris-sidecar/internal/resolver/common"
	"github.com/polarismesh/polaris-sidecar/pkg/constants"
	"github.com/polarismesh/polaris-sidecar/pkg/log"
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

func (p *Proxy) HandleDNS(protocol string, w dns.ResponseWriter, r *dns.Msg) {
	startTime := time.Now()
	clientAddr := w.RemoteAddr()
	q := r.Question[0]
	// 确定协议类型
	network := constants.UdpProtocol
	if _, isTCP := clientAddr.(*net.TCPAddr); isTCP {
		network = constants.TcpProtocol
	}
	// 延迟记录日志
	defer func() {
		log.Infof("[resolver] recurse question: %s, network: %s, latency: %s, client_addr: %s, client_network: %s",
			q.String(), network, time.Since(startTime).String(), clientAddr.String(), clientAddr.Network())
	}()
	// 根据 ndots 和 search 配置生成带解析域名列表
	domains := p.expandQuery(q.Name)
	// 创建DNS客户端
	client := &dns.Client{Timeout: time.Duration(p.config.Timeout) * time.Second}
	// 开始解析
	for _, domain := range domains {
		req := r.Copy()
		req.Question[0].Name = domain
		// 尝试请求配置的DNS服务器
		for i := 0; i < p.config.Attempts; i++ {
			upstream := p.rotate.Next()
			r, rtt, err := client.Exchange(req, upstream)
			// 处理服务器返回的响应
			switch { //TODO 重新梳理
			case r != nil && !isAcceptableRcode(r.Rcode):
				log.Warnf("[resolver] recurse failed for question, question: %s, rtt: %s, recursor: %s, rcode: %s",
					req.String(), rtt, upstream, dns.RcodeToString[r.Rcode])

			case shouldAcceptResponse(err, r):
				log.Infof("[resolver] recurse succeeded for question, question: %s, rtt: %s, recursor: %s",
					req.String(), rtt, upstream)
				if err := w.WriteMsg(r); err != nil {
					log.Warnf("failed to respond to client: %v", err)
				}
				return

			default:
				log.Errorf("[resolver] recurse failed, nameserver:%s, error: %v", upstream, err)
			}
			log.Errorf("查询 %s 失败 (尝试 %d): %v", domain, i+1, err)
		}
	}
	common.WriteDnsCode(protocol, w, r, dns.RcodeServerFailure)
}

func isAcceptableRcode(rcode int) bool {
	return rcode == dns.RcodeSuccess || rcode == dns.RcodeNameError
}

func shouldAcceptResponse(err error, r *dns.Msg) bool {
	return err == nil || (r != nil && r.Truncated)
}

func (p *Proxy) expandQuery(name string) []string {
	ndots := p.config.Ndots
	search := p.config.Search
	if strings.Count(name, constants.DotSymbol) < ndots {
		expanded := make([]string, 0, len(search))
		for _, suffix := range search {
			expanded = append(expanded, name+constants.DotSymbol+suffix)
		}
		return expanded
	}
	return []string{name}
}
