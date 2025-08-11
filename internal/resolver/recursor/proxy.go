package recursor

import (
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/miekg/dns"

	"github.com/polarismesh/polaris-sidecar/internal/resolver/common"
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
		log.Infof("[recursor] question: (%s), protocol: %s, latency: %s, client_addr: %s, client_network: %s, "+
			"config:%s", q.String(), protocol, time.Since(startTime).String(), clientAddr.String(),
			clientAddr.Network(), p.config.String())
	}()
	// 根据 ndots 和 search 配置生成带解析域名列表
	domains := p.expandQuery(q.Name)
	// 创建DNS客户端
	client := &dns.Client{Net: network, Timeout: time.Duration(p.config.Timeout) * time.Second}
	// 开始解析
	for _, domain := range domains {
		req := r.Copy()
		req.Question[0].Name = domain
		// 尝试请求配置的DNS服务器
		for i := 0; i < p.config.Attempts; i++ {
			upstream := p.rotate.Next()
			r, rtt, err := client.Exchange(req, upstream)
			resInfo := fmt.Sprintf("upstream: %s, req: %s, rtt: %s, err:%v, resp:%s", upstream, req.String(), rtt,
				err, getDnsMsg(r))

			switch {
			case r != nil && !(r.Rcode == dns.RcodeSuccess || r.Rcode == dns.RcodeNameError):
				// 如果返回的响应码不是NOERROR（0查询成功）或者NXDOMAIN（3域名不存在），则仅记录日志，尝试下一个DNS服务器
				log.Warnf("[recursor] need retry for dns rcode not pass, info:%s", resInfo)
			case err == nil || (r != nil && r.Truncated):
				// 如果没有错误，或者有错误但是响应被截断，都返回响应，并退出循环
				// 客户端如果感知到响应被截断，会自动切换成 TCP 协议重试
				if err = w.WriteMsg(r); err != nil {
					log.Warnf("[recursor] failed to respond to client: %v", err)
				}
				log.Infof("[recursor] return for query succeeded, info:%s, ", resInfo)
				return
			default:
				log.Warnf("[recursor] need retry for query failed, info:%s", resInfo)
			}
			log.Errorf("nameserver %s query %s failed (try %d times): %v", upstream, domain, i+1, err)
		}
	}
	// If all resolvers fail, return a SERVFAIL message
	log.Errorf("[recursor] question: %s, protocol: %s, latency: %s, client_addr: %s, client_network: %s, domains:%s",
		q.String(), protocol, time.Since(startTime).String(), clientAddr.String(), clientAddr.Network(),
		utils.JsonString(domains))
	common.WriteDnsCode(protocol, w, r, dns.RcodeServerFailure)
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

func getDnsMsg(r *dns.Msg) string {
	if r == nil {
		return "<nil>"
	}
	return fmt.Sprintf("code:%s, msg:%s", dns.RcodeToString[r.Rcode], r.String())
}
