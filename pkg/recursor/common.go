package recursor

import (
	"strings"

	"github.com/miekg/dns"

	"github.com/polarismesh/polaris-sidecar/pkg/log"
	"github.com/polarismesh/polaris-sidecar/pkg/utils"
)

const (
	TcpProtocol = "tcp"
	UdpProtocol = "udp"

	etcResolvConfPath = "/etc/resolv.conf"
	localIp           = "127.0.0.1"
)

// TODO searchNames和nameservers处理, 优先使用配置文件中的 nameservers
func ParseResolvConf(bindLocalhost bool, nameServerList []string) (*dns.ClientConfig, error) {
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
	var searchNames []string
	var nameservers []string
	if dnsConfig != nil {
		for _, search := range dnsConfig.Search {
			searchNames = append(searchNames, search+".")
		}

		for _, server := range dnsConfig.Servers {
			if server == localIp && bindLocalhost {
				continue
			}
			nameservers = append(nameservers, server)
		}
	}
	log.Infof("[recursor] etcResolvConf updated:%s", utils.JsonString(dnsConfig))
	return dnsConfig, nil
}

func shouldUseSearch(domain string, ndots int) bool {
	dotCount := strings.Count(domain, utils.Quota)
	return dotCount < ndots // 域名中 "." 数量 < ndots 时启用 search
}
