package main

import (
	"time"

	"github.com/youtubediscord/RKNnoVPN/daemon/internal/control"
	"github.com/youtubediscord/RKNnoVPN/daemon/internal/core"
)

func (d *daemon) diagnosticsControlHandlers() control.DiagnosticsHandlers {
	return control.DiagnosticsHandlers{
		Version:               Version,
		ConfigPath:            d.cfgPath,
		ProfilePath:           d.profilePath,
		DataDir:               d.dataDir,
		CurrentConfig:         d.currentConfig,
		RunHealth:             d.healthMon.RunOnce,
		HealthSnapshot:        d.buildRuntimeV2HealthSnapshot,
		RuntimeStatus:         d.controlRuntimeStatus,
		NetstackReport:        d.diagnosticNetstackReport,
		NetstackRuntimeReport: d.diagnosticNetstackRuntimeReport,
		TestNodes:             d.testNodeProbesV2,
		CoreStartReport:       d.coreMgr.LastStartReport,
		CoreRuntimeReport:     d.coreMgr.LastRuntimeReport,
		ReloadReport:          d.LastReloadReport,
		Exec:                  core.ExecCommand,
		Now:                   time.Now,
	}
}
