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

package utils

import (
	"strings"

	"github.com/polarismesh/polaris-go/pkg/config"
	"github.com/polarismesh/polaris-go/pkg/model"

	"github.com/polarismesh/polaris-sidecar/pkg/constants"
	"github.com/polarismesh/polaris-sidecar/pkg/log"
)

// ParseQname parse the qname into service and suffix
// qname format: <service>.<namespace>.<suffix>
func ParseQname(qname string, suffix string, currentNs string) *model.ServiceKey {
	var matched bool
	qname, matched = MatchSuffix(qname, suffix)
	if !matched {
		log.Infof("[utils] parse qname %s failed, suffix %s not match", qname, suffix)
		return nil
	}
	qname = RemoveQuota(qname)
	var namespace string
	var serviceName string
	// quota not found, use current namespace
	sepIndex := strings.LastIndex(qname, constants.DotSymbol)
	if sepIndex < 0 {
		namespace = currentNs
		serviceName = qname
	} else {
		namespace = qname[sepIndex+1:]
		if strings.ToLower(namespace) == constants.SysNamespace {
			namespace = config.ServerNamespace
		}
		serviceName = qname[:sepIndex]
	}
	return &model.ServiceKey{Namespace: namespace, Service: serviceName}
}

// MatchSuffix match the suffix and return the split qname
func MatchSuffix(qname string, suffix string) (string, bool) {
	if len(suffix) == 0 {
		return qname, true
	}
	qname = AddQuota(qname)
	suffix = AddQuota(suffix)
	if !strings.HasSuffix(qname, suffix) {
		return qname, false
	}
	qname = qname[:len(qname)-len(suffix)]
	return qname, true
}

// AddQuota add quota to the qname if not exist
func AddQuota(qname string) string {
	if !strings.HasSuffix(qname, constants.DotSymbol) {
		qname += constants.DotSymbol
	}
	return qname
}

// RemoveQuota remove quota from the qname if exist
func RemoveQuota(qname string) string {
	if strings.HasSuffix(qname, constants.DotSymbol) {
		qname = qname[:len(qname)-1]
	}
	return qname
}
