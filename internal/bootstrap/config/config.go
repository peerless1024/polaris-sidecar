/**
 * Tencent is pleased to support the open source community by making CL5 available.
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

package config

import (
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/hashicorp/go-multierror"
	sdkconf "github.com/polarismesh/polaris-go/pkg/config"
	sdkloc "github.com/polarismesh/polaris-go/plugin/location"
	"gopkg.in/yaml.v3"

	"github.com/polarismesh/polaris-sidecar/internal/debugger"
	"github.com/polarismesh/polaris-sidecar/internal/mesh/metrics"
	"github.com/polarismesh/polaris-sidecar/internal/mesh/mtls"
	"github.com/polarismesh/polaris-sidecar/internal/mesh/rls"
	"github.com/polarismesh/polaris-sidecar/internal/resolver"
	"github.com/polarismesh/polaris-sidecar/internal/resolver/common"
	"github.com/polarismesh/polaris-sidecar/internal/resolver/recursor"
	"github.com/polarismesh/polaris-sidecar/pkg/constants"
	"github.com/polarismesh/polaris-sidecar/pkg/log"
	"github.com/polarismesh/polaris-sidecar/pkg/polaris"
	"github.com/polarismesh/polaris-sidecar/pkg/utils"
)

// SidecarConfig global sidecar config struct
type SidecarConfig struct {
	Namespace     string                `yaml:"namespace"`
	PolarisConfig *PolarisConfig        `yaml:"polaris"`
	Bind          string                `yaml:"bind"`
	Port          int                   `yaml:"port"`
	Logger        *log.Options          `yaml:"logger"`
	Recurse       *RecurseConfig        `yaml:"recurse"`
	Resolvers     []*common.ConfigEntry `yaml:"resolvers"`
	MeshConfig    *MeshConfig           `yaml:"mesh"`
	Debugger      *debugger.DebugConfig `yaml:"debugger"`
	DnsEnabled    bool                  `yaml:"-"`
	MeshEnabled   bool                  `yaml:"-"`
}

type PolarisConfig struct {
	Addresses        []string                    `yaml:"addresses"`
	Location         *sdkconf.LocationConfigImpl `yaml:"location"`
	NearbyMatchLevel string                      `yaml:"nearby_match_level"`
}

func (p *PolarisConfig) setLocationConfig(providerConfig *sdkconf.LocationProviderConfigImpl) {
	if p.Location == nil {
		p.Location = &sdkconf.LocationConfigImpl{
			Providers: make([]*sdkconf.LocationProviderConfigImpl, 0),
		}
	}
	if p.Location.Providers == nil {
		p.Location.Providers = make([]*sdkconf.LocationProviderConfigImpl, 0)
	}
	for index, provider := range p.Location.Providers {
		if provider.Type == providerConfig.Type {
			p.Location.Providers[index] = providerConfig
			return
		}
	}
	p.Location.Providers = append(p.Location.Providers, providerConfig)
	return
}

type RecurseConfig struct {
	Enable      bool     `yaml:"enable"`
	TimeoutSec  int      `yaml:"timeoutSec"`
	NameServers []string `yaml:"name_servers"`
}

type MeshConfig struct {
	Metrics   *metrics.MetricConfig `yaml:"metrics"`
	RateLimit *rls.Config           `yaml:"ratelimit"`
	MTLS      *MeshMTLSConfig       `yaml:"mtls"`
}

type MeshMTLSConfig struct {
	Enable   bool   `yaml:"enable"`
	CAServer string `yaml:"ca_server"`
}

// String toString output
func (s *SidecarConfig) String() string {
	strBytes, err := yaml.Marshal(&s)
	if nil != err {
		return ""
	}
	return string(strBytes)
}

// InitPolarisApi initializes the Polaris API based on the configuration.
func (s *SidecarConfig) InitPolarisApi() error {
	polarisApiConf := &polaris.Config{
		Addresses:          s.PolarisConfig.Addresses,
		LocationConfigImpl: s.PolarisConfig.Location,
		NearbyMatchLevel:   s.PolarisConfig.NearbyMatchLevel,
	}
	if s.isMeshMetricsEnabled() {
		polarisApiConf.Metrics = &polaris.Metrics{
			Port:     s.MeshConfig.Metrics.Port,
			Type:     s.MeshConfig.Metrics.Type,
			IP:       s.Bind,
			Interval: s.MeshConfig.Metrics.Interval,
			Address:  s.MeshConfig.Metrics.Address,
		}
	}
	if err := polaris.InitPolarisContext(polarisApiConf); err != nil {
		log.Errorf("[bootstrap] fail to init polaris polaris, err: %v", err)
		return err
	}
	log.Infof("[bootstrap] init polaris api successfully")
	return nil
}

// InitDnsResolver initializes the DNS servers based on the configuration.
func (s *SidecarConfig) InitDnsResolver() (*resolver.Server, error) {
	resolveConfig := &common.ResolverConfig{
		BindIP:    s.Bind,
		BindPort:  uint32(s.Port),
		Resolvers: s.Resolvers,
	}
	var err error
	var recurseProxyConf *recursor.Config
	if s.Recurse.Enable {
		recurseProxyConf, err = recursor.InitRecurseConfig(s.bindLocalhost(), s.Recurse.TimeoutSec,
			s.Recurse.NameServers)
		if err != nil {
			log.Errorf("[bootstrap] fail to init recursor proxy config, err: %v", err)
			return nil, err
		}
	}
	svr, err := resolver.NewServer(resolveConfig, recurseProxyConf)
	if err != nil {
		log.Errorf("[bootstrap] fail to init dns server, err: %v", err)
		return nil, err
	}
	log.Infof("[bootstrap] build dns server successfully")
	return svr, nil
}

// InitDebugServer initializes the debug server based on the configuration.
func (s *SidecarConfig) InitDebugServer(dnsServer *resolver.Server) (*debugger.DebugServer, error) {
	if !s.isDebugEnabled() || dnsServer == nil {
		log.Infof("[bootstrap] debug server is not enabled, skip build")
		return nil, nil
	}
	debugSvr := debugger.NewDebugServer(s.Bind, s.Debugger.Port)
	if err := debugSvr.RegisterDebugHandler(dnsServer.Debugger()); err != nil {
		log.Errorf("[bootstrap] fail to register debug handler, err: %v", err)
		return nil, err
	}
	log.Infof("[bootstrap] build debug server successfully")
	return debugSvr, nil
}

// InitMeshMetrics initializes the mesh metrics server based on the configuration.
func (s *SidecarConfig) InitMeshMetrics() *metrics.Server {
	if !s.isMeshMetricsEnabled() {
		log.Infof("[bootstrap] mesh metrics is not enabled, skip build")
		return nil
	}
	metricServer := metrics.NewServer(s.Namespace, s.MeshConfig.Metrics.Port)
	log.Infof("[bootstrap] build metric server successfully")
	return metricServer
}

// InitMeshMtls initializes the mesh mtls agent based on the configuration.
func (s *SidecarConfig) InitMeshMtls() (*mtls.Agent, error) {
	if !s.isMeshMTLSEnabled() {
		log.Infof("[bootstrap] mesh mtls agent is not enabled, skip build")
		return nil, nil
	}
	agent, err := mtls.New(mtls.Option{
		CAServer: s.MeshConfig.MTLS.CAServer,
	})
	if err != nil {
		log.Errorf("[bootstrap] fail to init mesh mtls agent, err: %v", err)
		return nil, err
	}
	log.Info("[bootstrap] build mesh mtls agent successfully")
	return agent, nil
}

// InitMeshRatelimit initializes the mesh ratelimit server based on the configuration.
func (s *SidecarConfig) InitMeshRatelimit() *rls.RateLimitServer {
	if !s.isMeshRatelimitEnabled() {
		log.Infof("[bootstrap] mesh ratelimit is not enabled, skip build")
		return nil
	}
	conf := &rls.Config{
		Network: strings.ToLower(s.MeshConfig.RateLimit.Network),
		TLSInfo: s.MeshConfig.RateLimit.TLSInfo,
	}
	if conf.Network == constants.TcpProtocol {
		conf.Address = fmt.Sprintf("%s:%d", s.Bind, s.MeshConfig.RateLimit.BindPort)
	}
	ratelimitServer := rls.New(s.Namespace, conf)
	log.Infof("[bootstrap] build ratelimit server successfully")
	return ratelimitServer
}

func (s *SidecarConfig) mergeFileConfig(configFile string) error {
	buf, err := utils.ReadFile(configFile)
	if nil != err || buf == nil {
		return err
	}
	err = parseYamlContent(buf, s)
	if nil != err {
		return err
	}
	log.Infof("[config] config file %s parsed to sidecarConfig: \n%s", configFile, s.String())
	return nil
}

func (s *SidecarConfig) mergeEnv() {
	s.Bind = getEnvStringValue(constants.EnvSidecarBind, s.Bind)
	s.Port = getEnvIntValue(constants.EnvSidecarPort, s.Port)
	s.Namespace = getEnvStringValue(constants.EnvSidecarNamespace, s.Namespace)
	s.PolarisConfig.Addresses = getEnvStringsValue(constants.EnvPolarisAddress, s.PolarisConfig.Addresses)
	s.PolarisConfig.NearbyMatchLevel = getEnvStringValue(constants.EnvSidecarNearbyMatchLevel, s.PolarisConfig.NearbyMatchLevel)
	s.mergeLocationEnv()
	s.Recurse.Enable = getEnvBoolValue(constants.EnvSidecarRecurseEnable, s.Recurse.Enable)
	s.Recurse.TimeoutSec = getEnvIntValue(constants.EnvSidecarRecurseTimeout, s.Recurse.TimeoutSec)
	s.Logger.RotateOutputPath = getEnvStringValue(constants.EnvSidecarLogRotateOutputPath, s.Logger.RotateOutputPath)
	s.Logger.ErrorRotateOutputPath = getEnvStringValue(constants.EnvSidecarLogErrorRotateOutputPath, s.Logger.ErrorRotateOutputPath)
	s.Logger.RotationMaxSize = getEnvIntValue(constants.EnvSidecarLogRotationMaxSize, s.Logger.RotationMaxSize)
	s.Logger.RotationMaxBackups = getEnvIntValue(constants.EnvSidecarLogRotationMaxBackups, s.Logger.RotationMaxBackups)
	s.Logger.RotationMaxAge = getEnvIntValue(constants.EnvSidecarLogRotationMaxAge, s.Logger.RotationMaxAge)
	s.Logger.OutputLevel = getEnvStringValue(constants.EnvSidecarLogLevel, s.Logger.OutputLevel)
	if len(s.Resolvers) > 0 {
		for _, resolverConf := range s.Resolvers {
			resolverConf.Namespace = s.Namespace
			if resolverConf.Name == common.PluginNameDnsAgent {
				resolverConf.DnsTtl = getEnvIntValue(constants.EnvSidecarDnsTtl, resolverConf.DnsTtl)
				resolverConf.Enable = getEnvBoolValue(constants.EnvSidecarDnsEnable, resolverConf.Enable)
				resolverConf.Suffix = getEnvStringValue(constants.EnvSidecarDnsSuffix, resolverConf.Suffix)
				routeLabels := getEnvStringValue(constants.EnvSidecarDnsRouteLabels, "")
				if len(routeLabels) > 0 {
					resolverConf.Option = make(map[string]interface{})
					resolverConf.Option["route_labels"] = routeLabels
				}
			} else if resolverConf.Name == common.PluginNameMeshProxy {
				resolverConf.DnsTtl = getEnvIntValue(constants.EnvSidecarMeshTtl, resolverConf.DnsTtl)
				resolverConf.Enable = getEnvBoolValue(constants.EnvSidecarMeshEnable, resolverConf.Enable)
				reloadIntervalSec := getEnvIntValue(constants.EnvSidecarMeshReloadInterval, 0)
				if reloadIntervalSec > 0 {
					resolverConf.Option["reload_interval_sec"] = reloadIntervalSec
				}
				dnsAnswerIP := getEnvStringValue(constants.EnvSidecarMeshAnswerIp, "")
				if len(dnsAnswerIP) > 0 {
					resolverConf.Option["dns_answer_ip"] = dnsAnswerIP
				}
			}
		}
	}
	s.MeshConfig.MTLS.Enable = getEnvBoolValue(constants.EnvSidecarMtlsEnable, s.MeshConfig.MTLS.Enable)
	s.MeshConfig.MTLS.CAServer = getEnvStringValue(constants.EnvSidecarMtlsCAServer, s.MeshConfig.MTLS.CAServer)
	s.MeshConfig.RateLimit.Enable = getEnvBoolValue(constants.EnvSidecarRLSEnable, s.MeshConfig.RateLimit.Enable)
	s.MeshConfig.Metrics.Enable = getEnvBoolValue(constants.EnvSidecarMetricEnable, s.MeshConfig.Metrics.Enable)
	s.MeshConfig.Metrics.Port = getEnvIntValue(constants.EnvSidecarMetricListenPort, s.MeshConfig.Metrics.Port)
	log.Infof("[config] sidecar config merged with env: \n%s", s.String())
}

func (s *SidecarConfig) mergeLocationEnv() {
	region := getEnvStringValue(constants.EnvSidecarRegion, "")
	zone := getEnvStringValue(constants.EnvSidecarZone, "")
	campus := getEnvStringValue(constants.EnvSidecarCampus, "")
	if region == "" && zone == "" && campus == "" {
		return
	}
	s.PolarisConfig.setLocationConfig(&sdkconf.LocationProviderConfigImpl{
		Type: sdkloc.Local,
		Options: map[string]interface{}{
			areaRegion: region,
			areaZone:   zone,
			areaCampus: campus,
		},
	})
}

func (s *SidecarConfig) mergeBootConfig(config *BootConfig) error {
	log.Infof("[config] merge bootstrap config: \n%s", config.String())
	var errs multierror.Error
	var err error
	if len(config.Bind) > 0 {
		s.Bind = config.Bind
	}
	if config.Port > 0 {
		s.Port = config.Port
	}
	if len(config.LogLevel) > 0 {
		s.Logger.OutputLevel = config.LogLevel
	}
	if len(config.RecurseEnabled) > 0 {
		s.Recurse.Enable, err = strconv.ParseBool(config.RecurseEnabled)
		if nil != err {
			errs.Errors = append(errs.Errors,
				fmt.Errorf("fail to parse recurse-enabled value to boolean, err: %v", err))
		}
	}
	s.Logger.OutputLevel = config.LogLevel
	if len(config.ResolverDnsAgentEnabled) > 0 || len(config.ResolverDnsAgentRouteLabels) > 0 {
		for _, resolverConfig := range s.Resolvers {
			if resolverConfig.Name == common.PluginNameDnsAgent {
				if len(config.ResolverDnsAgentEnabled) > 0 {
					resolverConfig.Enable, err = strconv.ParseBool(config.ResolverDnsAgentEnabled)
					if nil != err {
						errs.Errors = append(errs.Errors,
							fmt.Errorf("fail to parse resolver-dnsAgent-enabled value to boolean, err: %v", err))
					}
				}
				if len(config.ResolverDnsAgentRouteLabels) > 0 {
					labels := utils.ParseLabels(config.ResolverDnsAgentRouteLabels)
					if len(labels) > 0 {
						if len(resolverConfig.Option) == 0 {
							resolverConfig.Option = make(map[string]interface{})
						}
						resolverConfig.Option["route_labels"] = labels
					}
				}
				continue
			}
			if resolverConfig.Name == common.PluginNameMeshProxy {
				if len(config.ResolverMeshProxyEnabled) > 0 {
					resolverConfig.Enable, err = strconv.ParseBool(config.ResolverMeshProxyEnabled)
					if nil != err {
						errs.Errors = append(errs.Errors,
							fmt.Errorf("fail to parse resolver-meshproxy-enabled value to boolean, err: %v", err))
					}
				}
			}
		}
	}
	log.Infof("[config] sidecar config merged with bootstrap config: \n%s", s.String())
	if err = errs.ErrorOrNil(); err != nil {
		log.Errorf("[config] fail to merge bootstrap config to sidecar config, err: %v", errs.ErrorOrNil())
		return err
	}
	return nil
}

func (s *SidecarConfig) isMeshMetricsEnabled() bool {
	return s.MeshEnabled && s.MeshConfig != nil && s.MeshConfig.Metrics != nil && s.MeshConfig.Metrics.Enable
}

func (s *SidecarConfig) isMeshMTLSEnabled() bool {
	return s.MeshEnabled && s.MeshConfig != nil && s.MeshConfig.MTLS != nil && s.MeshConfig.MTLS.Enable
}

func (s *SidecarConfig) isDebugEnabled() bool {
	return s.Debugger != nil && s.Debugger.Enable
}

func (s *SidecarConfig) isMeshRatelimitEnabled() bool {
	return s.MeshEnabled && s.MeshConfig != nil && s.MeshConfig.RateLimit != nil && s.MeshConfig.RateLimit.Enable
}

func (s *SidecarConfig) bindLocalhost() bool {
	bindIP := net.ParseIP(s.Bind)
	return bindIP.IsLoopback() || bindIP.IsUnspecified()
}
