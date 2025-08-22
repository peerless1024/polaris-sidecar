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
	"fmt"
	"os"
	"testing"
)

const testCfg = `logger:
  output_paths:
    - stdout
  error_output_paths:
    - stderr
  rotate_output_path: logs/polaris-sidecar.log
  error_rotate_output_path: logs/polaris-sidecar-error.log
  rotation_max_size: 100
  rotation_max_backups: 10
  rotation_max_age: 7
  output_level: info
debugger:
  enable: false
  port: 30000
polaris:
  addresses: 
    - 10.0.1.35:8091
  # 地址提供插件，用于获取当前SDK所在的地域信息
  location:
    providers:
      - type: local
        options:
          region: ${REGION}
          zone: ${ZONE}
          campus: ${CAMPUS}
bind: 0.0.0.0
port: 53
namespace: default
recurse:
  enable: false
  timeoutSec: 1
mtls:
  enable: false
metrics:
  enable: true
  type: pull
  metricPort: 0
ratelimit:
  enable: true
  network: unix
resolvers:
  - name: dnsagent
    dns_ttl: 10
    enable: true
    suffix: "."
    # option:
    #   route_labels: "key:value,key:value"
  - name: meshproxy
    dns_ttl: 120
    enable: false
    option:
      reload_interval_sec: 30
      dns_answer_ip: ${aswip}
      recursion_available: true
`

const testAnswerIP = "127.0.0.8"

func TestParseYamlConfig(t *testing.T) {
	err := os.Setenv("aswip", testAnswerIP)
	if nil != err {
		t.Fatal(err)
	}
	cfg := &SidecarConfig{}
	err = parseYamlContent([]byte(testCfg), cfg)
	if nil != err {
		t.Fatal(err)
	}
	result := cfg.Resolvers[1].Option["dns_answer_ip"]
	if result != testAnswerIP {
		t.Fatal("answer ip should be " + testAnswerIP)
	}

}

const testRegion = "ap-guangzhou"

func TestParseYamlConfigRegion(t *testing.T) {
	err := os.Setenv("REGION", testRegion)
	if nil != err {
		t.Fatal(err)
	}
	cfg := &SidecarConfig{}
	err = parseYamlContent([]byte(testCfg), cfg)
	if nil != err {
		t.Fatal(err)
	}
	result := cfg.PolarisConfig.Location.GetProviders()[0].GetOptions()["region"]
	if result != testRegion {
		t.Fatal("region should be " + testRegion)
	}
}

const value = "this is a ${animal}, today is ${today}"

func TestReplaceEnv(t *testing.T) {
	err := os.Setenv("animal", "cat")
	if nil != err {
		t.Fatal(err)
	}
	nextValue := os.ExpandEnv(value)
	fmt.Println("nextValue is " + nextValue)

}
