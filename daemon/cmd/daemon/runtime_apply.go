package main

import (
	applytx "github.com/youtubediscord/RKNnoVPN/daemon/internal/apply"
	"github.com/youtubediscord/RKNnoVPN/daemon/internal/config"
	"github.com/youtubediscord/RKNnoVPN/daemon/internal/core"
	"github.com/youtubediscord/RKNnoVPN/daemon/internal/netstack"
	rootruntime "github.com/youtubediscord/RKNnoVPN/daemon/internal/runtime/root"
	"github.com/youtubediscord/RKNnoVPN/daemon/internal/runtimev2"
)

func (d *daemon) applyConfigWithOperation(newCfg *config.Config, reload bool, operation runtimev2.OperationKind) error {
	needsFullRestart := rootruntime.ReloadNeedsFullRestart(
		rootruntime.BuildScriptEnv(d.currentConfig(), d.dataDir),
		rootruntime.BuildScriptEnv(newCfg, d.dataDir),
	)

	return applytx.ApplyRuntimeConfig(
		applytx.RuntimeConfigApplyInput{
			NewConfig:        newCfg,
			ConfigPath:       d.cfgPath,
			Reload:           reload,
			Operation:        operation,
			WasRunning:       d.isRuntimeRunningOrDegraded(),
			NeedsFullRestart: needsFullRestart,
		},
		applytx.RuntimeConfigApplyDeps{
			EnsureIdle:       d.failIfRuntimeOperationActive,
			CommitConfig:     d.commitAppliedRuntimeConfig,
			SyncDesiredState: d.syncRuntimeV2DesiredState,
			RunOperation: func(kind runtimev2.OperationKind, phase runtimev2.Phase, fn func(generation int64) error) error {
				_, err := d.runtimeV2.RunOperation(kind, phase, fn)
				return err
			},
			ReloadRuntime: func(cfg *config.Config, generation int64, fullRestart bool) error {
				return d.reloadRuntimeAfterConfigChange(cfg, "apply config", "config saved", generation, fullRestart)
			},
		},
	)
}

func (d *daemon) commitAppliedRuntimeConfig(newCfg *config.Config) {
	d.mu.Lock()
	d.cfg = newCfg
	d.mu.Unlock()

	d.coreMgr.SetConfig(newCfg)
	d.rescueMgr.SetConfig(newCfg)
	applyHealthRuntimeConfig(d.healthMon, newHealthRuntimeConfig(newCfg))
	if d.netWatcher != nil {
		d.netWatcher.SetEnv(rootruntime.BuildScriptEnv(newCfg, d.dataDir))
	}
}

func (d *daemon) reloadRuntimeAfterConfigChange(cfg *config.Config, context string, savedLabel string, generation int64, fullRestart bool) error {
	if err := d.failIfResetInProgress(); err != nil {
		return err
	}
	return rootruntime.ReloadAfterConfigChange(
		rootruntime.ConfigReloadInput{
			Config:      cfg,
			Context:     context,
			SavedLabel:  savedLabel,
			Generation:  generation,
			FullRestart: fullRestart,
		},
		rootruntime.ConfigReloadDeps{
			StopSubsystems: d.stopSubsystems,
			FullRestart: func(generation int64) error {
				return newRootRuntimeBackend(d).RestartAfterConfigChange(generation)
			},
			LastRuntimeReport: d.coreMgr.LastRuntimeReport,
			HotSwap:           d.coreMgr.HotSwap,
			ReapplyRuntimeRules: func(cfg *config.Config) (netstack.Report, error) {
				return rootruntime.ReapplyRuntimeRules(cfg, d.dataDir, rootruntime.BuildScriptEnv(cfg, d.dataDir), core.ExecScript)
			},
			ResetNetworkState: func(generation int64) runtimev2.ResetReport {
				return d.resetNetworkStateReport(generation, runtimev2.BackendRootTProxy)
			},
			ResetRescueState:    d.rescueMgr.Reset,
			StartSubsystems:     d.startSubsystems,
			RefreshHealth:       d.runtimeV2.RefreshHealth,
			RuntimeErrorCode:    rootruntime.RuntimeErrorCode,
			ObserveReloadReport: d.setLastReloadReport,
		},
	)
}

func (d *daemon) setLastReloadReport(report core.RuntimeStageReport) {
	d.reportMu.Lock()
	defer d.reportMu.Unlock()
	d.lastReloadReport = report
}

func (d *daemon) LastReloadReport() core.RuntimeStageReport {
	d.reportMu.Lock()
	defer d.reportMu.Unlock()
	return d.lastReloadReport
}
