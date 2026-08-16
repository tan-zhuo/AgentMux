package app

import (
	"time"

	"agentmux/internal/metrics"
)

// MetricsService reads host vitals on demand.
//
// There is no background ticker: the panel polls while it is open and stops
// when it is closed. A monitoring feature that keeps hammering fifty servers
// after you navigate away is a cost with no reader.
type MetricsService struct{ core *Core }

// NewMetricsService binds a metrics service to the core.
func NewMetricsService(c *Core) *MetricsService { return &MetricsService{core: c} }

// ServiceName identifies the service in Wails logs.
func (m *MetricsService) ServiceName() string { return "MetricsService" }

// Sample reads one set of vitals from a server.
func (m *MetricsService) Sample(serverID string) metrics.Sample {
	return metrics.Collect(m.core.Pool, serverID, time.Now().Unix())
}

// SampleMany reads several servers concurrently, for an overview across a fleet.
func (m *MetricsService) SampleMany(serverIDs []string) []metrics.Sample {
	out := make([]metrics.Sample, len(serverIDs))
	done := make(chan int, len(serverIDs))
	sem := make(chan struct{}, 6)

	now := time.Now().Unix()
	for i, id := range serverIDs {
		go func(i int, id string) {
			sem <- struct{}{}
			defer func() { <-sem }()
			out[i] = metrics.Collect(m.core.Pool, id, now)
			done <- i
		}(i, id)
	}
	for range serverIDs {
		<-done
	}
	return out
}
