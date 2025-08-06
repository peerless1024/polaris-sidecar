package recursor

import (
	"github.com/miekg/dns"

	"github.com/polarismesh/polaris-sidecar/pkg/log"
	"github.com/polarismesh/polaris-sidecar/pkg/utils"
)

const (
	etcResolvConfPath = "/etc/resolv.conf"
	localIp           = "127.0.0.1"
)

type Config struct {
	Ndots    int      // 触发搜索域的最小点数
	Search   []string // 搜索域列表（如 ["cluster.local", "svc.cluster.local"]）
	Timeout  int      // 单次查询超时（秒）
	Attempts int      // 最大重试次数 TODO 默认为上游服务器数量
	Upstream []string // 上游DNS服务器（如 ["8.8.8.8:53", "1.1.1.1:53"]）
}

func InitRecurseProxy(bindLocalhost bool, timeout int, nameServers []string) (*Config, error) {
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
		Timeout:  timeout,
		Upstream: make([]string, 0),
	}
	nameServerMap := make(map[string]bool)
	// 优先配置项里的 dns 服务器
	config.mergeUpstream(bindLocalhost, nameServerMap, nameServers)
	// 其次本地配置的 dns 服务器
	config.mergeUpstream(bindLocalhost, nameServerMap, dnsConfig.Servers)
	log.Infof("[recursor] init recursor proxy config: %v", config.String())
	return config, nil
}

func (r *Config) String() string {
	return utils.JsonString(r)
}

func (r *Config) mergeUpstream(bindLocalhost bool, nameServerMap map[string]bool, nameServers []string) {
	for _, nameServer := range nameServers {
		if _, ok := nameServerMap[nameServer]; !ok && !needSkip(bindLocalhost, nameServer) {
			r.Upstream = append(r.Upstream, nameServer)
			nameServerMap[nameServer] = true
		}
	}
}

func needSkip(bindLocalhost bool, nameServer string) bool {
	if nameServer == localIp && bindLocalhost {
		return true
	}
	return false
}
