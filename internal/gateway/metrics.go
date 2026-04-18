package gateway

import (
	"strings"
	"sync"

	"github.com/prometheus/client_golang/prometheus"

	"neo-code/internal/gateway/protocol"
)

const (
	// unknownMethodMetricLabel 统一收敛未知 method 的指标标签值，防止高基数放大。
	unknownMethodMetricLabel = "unknown_method"
)

var allowedRPCMethodMetricLabels = map[string]struct{}{
	strings.ToLower(protocol.MethodGatewayAuthenticate):      {},
	strings.ToLower(protocol.MethodGatewayPing):              {},
	strings.ToLower(protocol.MethodGatewayBindStream):        {},
	strings.ToLower(protocol.MethodGatewayRun):               {},
	strings.ToLower(protocol.MethodGatewayCompact):           {},
	strings.ToLower(protocol.MethodGatewayCancel):            {},
	strings.ToLower(protocol.MethodGatewayListSessions):      {},
	strings.ToLower(protocol.MethodGatewayLoadSession):       {},
	strings.ToLower(protocol.MethodGatewayResolvePermission): {},
	strings.ToLower(protocol.MethodGatewayEvent):             {},
	strings.ToLower(protocol.MethodWakeOpenURL):              {},
}

// GatewayMetrics 维护网关关键指标，并同时提供 Prometheus 与 JSON 视图。
type GatewayMetrics struct {
	registry *prometheus.Registry

	requestsTotal     *prometheus.CounterVec
	authFailuresTotal *prometheus.CounterVec
	aclDeniedTotal    *prometheus.CounterVec
	connectionsActive *prometheus.GaugeVec
	streamDropped     *prometheus.CounterVec

	mu       sync.RWMutex
	snapshot map[string]map[string]float64
}

// NewGatewayMetrics 创建网关指标收集器。
func NewGatewayMetrics() *GatewayMetrics {
	requestsTotal := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "gateway_requests_total",
			Help: "Total gateway rpc requests grouped by source, method and status.",
		},
		[]string{"source", "method", "status"},
	)
	authFailuresTotal := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "gateway_auth_failures_total",
			Help: "Total gateway auth failures grouped by source and reason.",
		},
		[]string{"source", "reason"},
	)
	aclDeniedTotal := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "gateway_acl_denied_total",
			Help: "Total gateway ACL denials grouped by source and method.",
		},
		[]string{"source", "method"},
	)
	connectionsActive := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "gateway_connections_active",
			Help: "Current active stream connections grouped by channel.",
		},
		[]string{"channel"},
	)
	streamDropped := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "gateway_stream_dropped_total",
			Help: "Total dropped stream connections grouped by reason.",
		},
		[]string{"reason"},
	)

	registry := prometheus.NewRegistry()
	registry.MustRegister(
		requestsTotal,
		authFailuresTotal,
		aclDeniedTotal,
		connectionsActive,
		streamDropped,
	)

	return &GatewayMetrics{
		registry:          registry,
		requestsTotal:     requestsTotal,
		authFailuresTotal: authFailuresTotal,
		aclDeniedTotal:    aclDeniedTotal,
		connectionsActive: connectionsActive,
		streamDropped:     streamDropped,
		snapshot: map[string]map[string]float64{
			"gateway_requests_total":       {},
			"gateway_auth_failures_total":  {},
			"gateway_acl_denied_total":     {},
			"gateway_connections_active":   {},
			"gateway_stream_dropped_total": {},
		},
	}
}

// Registry 返回 Prometheus 注册表。
func (m *GatewayMetrics) Registry() *prometheus.Registry {
	if m == nil {
		return nil
	}
	return m.registry
}

// Snapshot 返回用于 /metrics.json 的指标快照。
func (m *GatewayMetrics) Snapshot() map[string]map[string]float64 {
	if m == nil {
		return map[string]map[string]float64{}
	}
	m.mu.RLock()
	defer m.mu.RUnlock()

	cloned := make(map[string]map[string]float64, len(m.snapshot))
	for name, values := range m.snapshot {
		clonedValues := make(map[string]float64, len(values))
		for key, value := range values {
			clonedValues[key] = value
		}
		cloned[name] = clonedValues
	}
	return cloned
}

// IncRequests 增加请求总量计数。
func (m *GatewayMetrics) IncRequests(source, method, status string) {
	if m == nil {
		return
	}
	source = normalizeMetricLabel(source)
	method = normalizeMethodMetricLabel(method)
	status = normalizeMetricLabel(status)
	m.requestsTotal.WithLabelValues(source, method, status).Inc()
	m.addSnapshotCounter("gateway_requests_total", source+"|"+method+"|"+status, 1)
}

// IncAuthFailures 增加认证失败计数。
func (m *GatewayMetrics) IncAuthFailures(source, reason string) {
	if m == nil {
		return
	}
	source = normalizeMetricLabel(source)
	reason = normalizeMetricLabel(reason)
	m.authFailuresTotal.WithLabelValues(source, reason).Inc()
	m.addSnapshotCounter("gateway_auth_failures_total", source+"|"+reason, 1)
}

// IncACLDenied 增加 ACL 拒绝计数。
func (m *GatewayMetrics) IncACLDenied(source, method string) {
	if m == nil {
		return
	}
	source = normalizeMetricLabel(source)
	method = normalizeMethodMetricLabel(method)
	m.aclDeniedTotal.WithLabelValues(source, method).Inc()
	m.addSnapshotCounter("gateway_acl_denied_total", source+"|"+method, 1)
}

// SetConnectionsActive 更新当前连接数指标。
func (m *GatewayMetrics) SetConnectionsActive(channel string, value int) {
	if m == nil {
		return
	}
	channel = normalizeMetricLabel(channel)
	m.connectionsActive.WithLabelValues(channel).Set(float64(value))
	m.setSnapshotGauge("gateway_connections_active", channel, float64(value))
}

// IncStreamDropped 增加流连接剔除计数。
func (m *GatewayMetrics) IncStreamDropped(reason string) {
	if m == nil {
		return
	}
	reason = normalizeMetricLabel(reason)
	m.streamDropped.WithLabelValues(reason).Inc()
	m.addSnapshotCounter("gateway_stream_dropped_total", reason, 1)
}

func (m *GatewayMetrics) addSnapshotCounter(metricName, key string, delta float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	metric := m.snapshot[metricName]
	if metric == nil {
		metric = map[string]float64{}
		m.snapshot[metricName] = metric
	}
	metric[key] += delta
}

func (m *GatewayMetrics) setSnapshotGauge(metricName, key string, value float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	metric := m.snapshot[metricName]
	if metric == nil {
		metric = map[string]float64{}
		m.snapshot[metricName] = metric
	}
	metric[key] = value
}

func normalizeMetricLabel(value string) string {
	normalized := strings.TrimSpace(strings.ToLower(value))
	if normalized == "" {
		return "unknown"
	}
	return normalized
}

// normalizeMethodMetricLabel 将 method 标签收敛到有限集合，未知值统一折叠为 unknown_method。
func normalizeMethodMetricLabel(method string) string {
	normalized := normalizeMetricLabel(method)
	if normalized == "unknown" {
		return unknownMethodMetricLabel
	}
	if _, exists := allowedRPCMethodMetricLabels[normalized]; !exists {
		return unknownMethodMetricLabel
	}
	return normalized
}
