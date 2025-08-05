package utils

import (
	"testing"

	"github.com/polarismesh/polaris-go/pkg/config"
	"github.com/polarismesh/polaris-go/pkg/model"
	"github.com/stretchr/testify/assert"
)

func TestParseQname(t *testing.T) {
	tests := []struct {
		name      string
		qname     string
		suffix    string
		currentNs string
		expected  *model.ServiceKey
	}{
		{
			name:      "后缀不匹配",
			qname:     "service.ns.svc.cluster.local",
			suffix:    "cluster.local",
			currentNs: "default",
			expected: &model.ServiceKey{
				Namespace: "svc",
				Service:   "service.ns",
			},
		},
		{
			name:      "后缀匹配但qname以点结尾",
			qname:     "service.ns.svc.cluster.local.",
			suffix:    "svc.cluster.local",
			currentNs: "default",
			expected: &model.ServiceKey{
				Namespace: "ns",
				Service:   "service",
			},
		},
		{
			name:      "没有命名空间分隔符",
			qname:     "service.svc.cluster.local",
			suffix:    "svc.cluster.local",
			currentNs: "default",
			expected: &model.ServiceKey{
				Namespace: "default",
				Service:   "service",
			},
		},
		{
			name:      "命名空间为polaris",
			qname:     "service.polaris.svc.cluster.local",
			suffix:    "svc.cluster.local",
			currentNs: "default",
			expected: &model.ServiceKey{
				Namespace: config.ServerNamespace,
				Service:   "service",
			},
		},
		{
			name:      "正常情况",
			qname:     "service.production.svc.cluster.local",
			suffix:    "svc.cluster.local",
			currentNs: "default",
			expected: &model.ServiceKey{
				Namespace: "production",
				Service:   "service",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ParseQname(tt.qname, tt.suffix, tt.currentNs)
			assert.Equal(t, tt.expected, result)
		})
	}
}
