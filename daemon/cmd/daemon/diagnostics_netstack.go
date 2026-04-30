package main

import (
	"github.com/youtubediscord/RKNnoVPN/daemon/internal/config"
	"github.com/youtubediscord/RKNnoVPN/daemon/internal/core"
	"github.com/youtubediscord/RKNnoVPN/daemon/internal/diagnostics"
	"github.com/youtubediscord/RKNnoVPN/daemon/internal/netstack"
	rootruntime "github.com/youtubediscord/RKNnoVPN/daemon/internal/runtime/root"
)

func (d *daemon) diagnosticNetstackReport(cfg *config.Config) netstack.Report {
	if cfg == nil {
		return diagnostics.VerifyCleanup(d.dataDir, nil, false)
	}
	return diagnostics.VerifyCleanup(d.dataDir, rootruntime.BuildScriptEnv(cfg, d.dataDir), true)
}

func (d *daemon) diagnosticNetstackRuntimeReport(cfg *config.Config) netstack.Report {
	if cfg == nil {
		return diagnostics.VerifyRuntime(d.dataDir, nil, false, false)
	}
	if d.coreMgr == nil {
		return diagnostics.VerifyRuntime(d.dataDir, rootruntime.BuildScriptEnv(cfg, d.dataDir), true, false)
	}
	status := d.coreMgr.Status()
	runtimeActive := status.State == core.StateRunning.String() || status.State == core.StateDegraded.String()
	return diagnostics.VerifyRuntime(d.dataDir, rootruntime.BuildScriptEnv(cfg, d.dataDir), true, runtimeActive)
}
