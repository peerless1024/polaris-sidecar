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

package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/polarismesh/polaris-sidecar/internal/debugger"
	"github.com/polarismesh/polaris-sidecar/internal/mesh/metrics"
	"github.com/polarismesh/polaris-sidecar/internal/mesh/mtls"
	"github.com/polarismesh/polaris-sidecar/internal/mesh/rls"
	"github.com/polarismesh/polaris-sidecar/internal/resolver/common"
	"github.com/polarismesh/polaris-sidecar/pkg/constants"
	"github.com/polarismesh/polaris-sidecar/pkg/log"
)

// BootConfig simple config for bootstrap
type BootConfig struct {
	Bind                        string
	Port                        int
	LogLevel                    string
	RecurseEnabled              string
	ResolverDnsAgentEnabled     string
	ResolverDnsAgentRouteLabels string
	ResolverMeshProxyEnabled    string
}

func (b *BootConfig) String() string {
	strBytes, err := json.Marshal(&b)
	if nil != err {
		return ""
	}
	return string(strBytes)
}

// InitConfig parse config file, environments and bootstrap configurations to object
// order of priority is: file < environment < bootstrap
func InitConfig(configFile string, bootConfig *BootConfig) (*SidecarConfig, error) {
	sidecarConfig := defaultSidecarConfig()
	if err := sidecarConfig.mergeFileConfig(configFile); nil != err {
		return nil, err
	}
	sidecarConfig.mergeEnv()
	if err := sidecarConfig.mergeBootConfig(bootConfig); nil != err {
		return nil, err
	}
	if err := sidecarConfig.verify(); err != nil {
		return nil, err
	}
	return sidecarConfig, nil
}

// 设置关键默认值
func defaultSidecarConfig() *SidecarConfig {
	s := &SidecarConfig{
		PolarisConfig: &PolarisConfig{
			Addresses: []string{
				"127.0.0.1:8091",
			},
		},
		Namespace: "default",
		Bind:      "0.0.0.0",
		Port:      53,
		Recurse: &RecurseConfig{
			Enable:     false,
			TimeoutSec: 1,
		},
		Logger: &log.Options{
			OutputPaths: []string{
				"stdout",
			},
			ErrorOutputPaths: []string{
				"stderr",
			},
			RotateOutputPath:      "logs/polaris-sidecar.log",
			ErrorRotateOutputPath: "logs/polaris-sidecar-error.log",
			RotationMaxAge:        7,
			RotationMaxBackups:    100,
			RotationMaxSize:       100,
			OutputLevel:           "info",
		},
		Resolvers: []*common.ConfigEntry{
			{
				Name:   common.PluginNameDnsAgent,
				DnsTtl: 10,
				Enable: true,
				Suffix: constants.DotSymbol,
			},
			{
				Name:   common.PluginNameMeshProxy,
				DnsTtl: 120,
				Enable: false,
				Suffix: constants.DotSymbol,
				Option: map[string]interface{}{
					"reload_interval_sec": constants.MeshDefaultReloadIntervalSec,
					"dns_answer_ip":       constants.MeshDefaultDnsAnswerIp,
				},
			},
		},
		MeshConfig: &MeshConfig{
			MTLS: &MeshMTLSConfig{
				Enable:   false,
				CAServer: mtls.DefaultCaServer,
			},
			RateLimit: &rls.Config{
				Enable:  false,
				Network: "unix",
				Address: rls.DefaultRLSAddress,
			},
			Metrics: &metrics.MetricConfig{
				Enable: false,
				Port:   metrics.DefaultListenPort,
			},
		},
		Debugger: &debugger.DebugConfig{
			Enable: true,
			Port:   debugger.DefaultListenPort,
		},
	}
	log.Infof("[config] default sidecar config:%s", s.String())
	return s
}

func parseYamlContent(content []byte, sidecarConfig *SidecarConfig) error {
	data := []byte(os.ExpandEnv(string(content)))
	decoder := yaml.NewDecoder(bytes.NewBuffer(data))
	if err := decoder.Decode(sidecarConfig); nil != err {
		log.Errorf("[config] parse yaml error: %v", err)
		return fmt.Errorf("parse yaml %s error:%s", content, err.Error())
	}
	return nil
}

func getEnvStringValue(envName string, defaultValue string) string {
	envValue := os.Getenv(envName)
	if len(envValue) > 0 {
		return envValue
	}
	return defaultValue
}

func getEnvStringsValue(envName string, defaultValues []string) []string {
	envValue := os.Getenv(envName)
	if len(envValue) > 0 {
		return strings.Split(envValue, constants.CommaSymbol)
	}
	return defaultValues
}

func getEnvIntValue(envName string, defaultValue int) int {
	envValue := os.Getenv(envName)
	if len(envValue) > 0 {
		intValue, err := strconv.Atoi(envValue)
		if nil != err {
			log.Errorf("[agent] fail to parse env %s, value %s to int, err: %v", envName, envValue, err)
			return defaultValue
		}
		return intValue
	}
	return defaultValue
}

func getEnvBoolValue(envName string, defaultValue bool) bool {
	envValue := os.Getenv(envName)
	if len(envValue) > 0 {
		boolValue, err := strconv.ParseBool(envValue)
		if nil != err {
			log.Errorf("[agent] fail to parse env %s, value %s to bool, err: %v", envName, envValue, err)
			return defaultValue
		}
		return boolValue
	}
	return defaultValue
}
