package main

import (
	"sync"
	"time"

	"github.com/youtubediscord/RKNnoVPN/daemon/internal/config"
	"github.com/youtubediscord/RKNnoVPN/daemon/internal/core"
	"github.com/youtubediscord/RKNnoVPN/daemon/internal/diagnostics"
	"github.com/youtubediscord/RKNnoVPN/daemon/internal/health"
	"github.com/youtubediscord/RKNnoVPN/daemon/internal/ipc"
	"github.com/youtubediscord/RKNnoVPN/daemon/internal/rescue"
	rootruntime "github.com/youtubediscord/RKNnoVPN/daemon/internal/runtime/root"
	"github.com/youtubediscord/RKNnoVPN/daemon/internal/runtimev2"
	"github.com/youtubediscord/RKNnoVPN/daemon/internal/watcher"
)

type daemon struct {
	cfg         *config.Config
	cfgPath     string
	profilePath string
	dataDir     string

	coreMgr    *core.CoreManager
	healthMon  *health.HealthMonitor
	rescueMgr  *rescue.RescueManager
	netWatcher *watcher.NetworkWatcher
	ipcServer  *ipc.Server
	runtimeV2  *runtimev2.Orchestrator

	mu                    sync.Mutex // protects cfg
	metricsMu             sync.Mutex
	compatibilityMu       sync.Mutex
	runtimeOpMu           sync.Mutex
	resetMu               sync.Mutex
	reportMu              sync.Mutex
	runtimeDesiredRunning bool
	runtimeOpEpoch        uint64
	latency               latencySnapshot
	traffic               trafficSnapshot
	trafficSamplerStop    chan struct{}
	releaseIntegrity      cachedReleaseIntegrity
	healthKick            time.Time
	lastReloadReport      core.RuntimeStageReport

	collectLeftoversOverride func(*config.Config) []string
}

type latencySnapshot = rootruntime.EgressProbeState

type trafficSnapshot struct {
	stats     runtimev2.TrafficStats
	checkedAt time.Time
}

type cachedReleaseIntegrity struct {
	value         diagnostics.ReleaseIntegrity
	releasePath   string
	manifestPath  string
	manifestMTime time.Time
	manifestSize  int64
	checkedAt     time.Time
}

func (d *daemon) currentConfig() *config.Config {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.cfg
}
