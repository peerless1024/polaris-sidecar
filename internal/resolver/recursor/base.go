package recursor

import (
	"fmt"

	"github.com/miekg/dns"

	"github.com/polarismesh/polaris-sidecar/pkg/log"
	"github.com/polarismesh/polaris-sidecar/pkg/utils"
)

const (
	etcResolvConfPath = "/etc/resolv.conf"
	localIp           = "127.0.0.1"
)

// Config 递归代理配置
type Config struct {
	Ndots    int      // 触发搜索域的最小点数
	Search   []string // 搜索域列表（如 ["cluster.local", "svc.cluster.local"]）
	Timeout  int      // 单次查询超时（秒）
	Attempts int      // 最大重试次数
	Upstream []string // 上游DNS服务器（如 ["8.8.8.8:53", "1.1.1.1:53"]）
}

func InitRecurseConfig(bindLocalhost bool, timeout int, nameServers []string) (*Config, error) {
	if !utils.IsFile(etcResolvConfPath) {
		log.Infof("[recursor] /etc/resolv.conf is not exist, skip to parse it")
		return nil, nil
	}
	dnsConfig, err := dns.ClientConfigFromFile(etcResolvConfPath)
	if err != nil {
		log.Errorf("[recursor] failed to load /etc/resolv.conf: %v", err)
		return nil, err
	}
	log.Infof("[recursor] successfully loaded etcResolvConf:%s", utils.JsonString(dnsConfig))
	config := &Config{
		Timeout:  getBigger(timeout, dnsConfig.Timeout),
		Upstream: make([]string, 0),
	}
	nameServerMap := make(map[string]bool)
	// 优先将配置项里的 dns 服务器加入 upstream
	config.mergeUpstream(bindLocalhost, nameServerMap, nameServers)
	// 其次将本地配置的 dns 服务器加入 upstream
	config.mergeUpstream(bindLocalhost, nameServerMap, dnsConfig.Servers)
	// 补充其他配置
	config.fillByResolvConfig(dnsConfig)
	log.Infof("[recursor] init recursor proxy config: %v", config.String())
	return config, nil
}

func (r *Config) String() string {
	return utils.JsonString(r)
}

func (r *Config) fillByResolvConfig(dnsConfig *dns.ClientConfig) {
	r.Ndots = 1
	if dnsConfig.Ndots > 0 {
		r.Ndots = dnsConfig.Ndots
	}
	r.Search = dnsConfig.Search
	r.Attempts = getBigger(dnsConfig.Attempts, len(r.Upstream))
}

func (r *Config) mergeUpstream(bindLocalhost bool, nameServerMap map[string]bool, nameServers []string) {
	for _, nameServer := range nameServers {
		if _, ok := nameServerMap[nameServer]; !ok && !needSkip(bindLocalhost, nameServer) {
			r.Upstream = append(r.Upstream, fmt.Sprintf("%s:53", nameServer))
			nameServerMap[nameServer] = true
		}
	}
}

func getBigger(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func needSkip(bindLocalhost bool, nameServer string) bool {
	if nameServer == localIp && bindLocalhost {
		return true
	}
	return false
}
