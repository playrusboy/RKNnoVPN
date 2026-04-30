package main

import (
	"time"

	"github.com/youtubediscord/RKNnoVPN/daemon/internal/control"
	"github.com/youtubediscord/RKNnoVPN/daemon/internal/core"
	"github.com/youtubediscord/RKNnoVPN/daemon/internal/health"
	"github.com/youtubediscord/RKNnoVPN/daemon/internal/runtimev2"
)

func (d *daemon) diagnosticsControlHandlers() control.DiagnosticsHandlers {
	return control.DiagnosticsHandlers{
		Version: Version,
		CurrentState: func() control.DiagnosticsState {
			d.mu.Lock()
			defer d.mu.Unlock()
			return control.DiagnosticsState{
				Config:      d.cfg,
				ConfigPath:  d.cfgPath,
				ProfilePath: d.profilePath,
				DataDir:     d.dataDir,
			}
		},
		RunHealth: func() *health.HealthResult {
			return d.healthMon.RunOnce()
		},
		HealthSnapshot: func(result *health.HealthResult, allowEgressProbe bool) runtimev2.HealthSnapshot {
			return d.buildRuntimeV2HealthSnapshot(result, allowEgressProbe)
		},
		RuntimeStatus:         d.controlRuntimeStatus,
		NetstackReport:        d.diagnosticNetstackReport,
		NetstackRuntimeReport: d.diagnosticNetstackRuntimeReport,
		TestNodes: func(url string, timeoutMS int, nodeIDs []string) []runtimev2.NodeProbeResult {
			return d.testNodeProbesV2(url, timeoutMS, nodeIDs)
		},
		CoreStartReport: func() core.RuntimeStageReport {
			return d.coreMgr.LastStartReport()
		},
		CoreRuntimeReport: func() core.RuntimeStageReport {
			return d.coreMgr.LastRuntimeReport()
		},
		ReloadReport: d.LastReloadReport,
		Exec:         core.ExecCommand,
		Now:          time.Now,
	}
}
