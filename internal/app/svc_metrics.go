package app

import (
	"sync"
	"time"

	"agentmux/internal/metrics"
)

// MetricsService reads host vitals on demand.
//
// There is no background ticker: the panel polls while it is open and stops
// when it is closed. A monitoring feature that keeps hammering fifty servers
// after you navigate away is a cost with no reader.
type MetricsService struct {
	core *Core

	hwMu sync.Mutex
	hw   map[string]metrics.Hardware
}

// NewMetricsService binds a metrics service to the core.
func NewMetricsService(c *Core) *MetricsService {
	return &MetricsService{core: c, hw: map[string]metrics.Hardware{}}
}

// ServiceName identifies the service in Wails logs.
func (m *MetricsService) ServiceName() string { return "MetricsService" }

// Sample reads one set of vitals from a server.
func (m *MetricsService) Sample(serverID string) metrics.Sample {
	return m.collect(serverID, time.Now().Unix())
}

// collect asks in whichever shell dialect the host speaks.
func (m *MetricsService) collect(serverID string, at int64) metrics.Sample {
	if m.core.IsWinHost(serverID) {
		return metrics.CollectWindows(m.core.Run, serverID, at)
	}
	if m.core.IsDarwinHost(serverID) {
		return metrics.CollectDarwin(m.core.Run, serverID, at)
	}
	return metrics.Collect(m.core.Run, serverID, at)
}

// Hardware reads what a server is made of: CPU model, memory modules, physical
// drives and graphics adapters. The answer cannot change while a box stays up,
// so a good reading is kept for the rest of the session; a failed one is not,
// so a host that was offline is asked again next time the panel opens.
func (m *MetricsService) Hardware(serverID string) metrics.Hardware {
	m.hwMu.Lock()
	if h, ok := m.hw[serverID]; ok {
		m.hwMu.Unlock()
		return h
	}
	m.hwMu.Unlock()

	var h metrics.Hardware
	switch {
	case m.core.IsWinHost(serverID):
		h = metrics.CollectWindowsHardware(m.core.Run, serverID)
	case m.core.IsDarwinHost(serverID):
		h = metrics.CollectDarwinHardware(m.core.Run, serverID)
	default:
		h = metrics.CollectHardware(m.core.Run, serverID)
	}
	if h.OK {
		m.hwMu.Lock()
		m.hw[serverID] = h
		m.hwMu.Unlock()
	}
	return h
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
			out[i] = m.collect(id, now)
			done <- i
		}(i, id)
	}
	for range serverIDs {
		<-done
	}
	return out
}
