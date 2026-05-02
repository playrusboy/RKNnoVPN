package main

import (
	"log"
	"strings"

	applytx "github.com/youtubediscord/RKNnoVPN/daemon/internal/apply"
	"github.com/youtubediscord/RKNnoVPN/daemon/internal/config"
	"github.com/youtubediscord/RKNnoVPN/daemon/internal/core"
	"github.com/youtubediscord/RKNnoVPN/daemon/internal/netstack"
	rootruntime "github.com/youtubediscord/RKNnoVPN/daemon/internal/runtime/root"
	"github.com/youtubediscord/RKNnoVPN/daemon/internal/runtimev2"
)

func (d *daemon) applyConfigWithOperation(newCfg *config.Config, reload bool, operation runtimev2.OperationKind) error {
	currentCfg := d.currentConfig()
	reloadPlan := rootruntime.PlanReload(
		rootruntime.BuildScriptEnv(currentCfg, d.dataDir),
		rootruntime.BuildScriptEnv(newCfg, d.dataDir),
	)
	selectorSwitchTag := ""
	if operation == runtimev2.OperationProfileApply {
		selectorSwitchTag = activeNodeSelectorSwitchTarget(currentCfg, newCfg, reloadPlan)
	}
	if reload && d.isRuntimeRunningOrDegraded() && len(reloadPlan.ChangedKeys) > 0 {
		mode := "hot-swap"
		switch {
		case reloadPlan.FullRestart:
			mode = "full-restart"
		case reloadPlan.NetstackReapplyAfter:
			mode = "hot-swap+netstack"
		}
		log.Printf("runtime reload plan: mode=%s changed_env=%s", mode, strings.Join(reloadPlan.ChangedKeys, ","))
	}

	return applytx.ApplyRuntimeConfig(
		applytx.RuntimeConfigApplyInput{
			NewConfig:            newCfg,
			ConfigPath:           d.cfgPath,
			Reload:               reload,
			Operation:            operation,
			WasRunning:           d.isRuntimeRunningOrDegraded(),
			NeedsFullRestart:     reloadPlan.FullRestart,
			NeedsNetstackReapply: reloadPlan.NetstackReapplyAfter,
		},
		applytx.RuntimeConfigApplyDeps{
			EnsureIdle:       d.failIfRuntimeOperationActive,
			CommitConfig:     d.commitAppliedRuntimeConfig,
			SyncDesiredState: d.syncRuntimeV2DesiredState,
			RunOperation: func(kind runtimev2.OperationKind, phase runtimev2.Phase, fn func(generation int64) error) error {
				_, err := d.runtimeV2.RunOperation(kind, phase, fn)
				return err
			},
			ReloadRuntime: func(cfg *config.Config, generation int64, fullRestart bool, netstackReapplyAfter bool) error {
				if selectorSwitchTag != "" && !fullRestart && !netstackReapplyAfter {
					if err := switchSingboxSelector(cfg, singboxProxySelectorTag, selectorSwitchTag); err == nil {
						log.Printf("runtime reload plan: mode=selector-switch selector=%s outbound=%s", singboxProxySelectorTag, selectorSwitchTag)
						return nil
					} else {
						log.Printf("runtime selector switch failed; falling back to hot-swap: %v", err)
					}
				}
				return d.reloadRuntimeAfterConfigChange(
					cfg,
					"apply config",
					"config saved",
					generation,
					fullRestart,
					netstackReapplyAfter,
				)
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

func (d *daemon) reloadRuntimeAfterConfigChange(cfg *config.Config, context string, savedLabel string, generation int64, fullRestart bool, netstackReapplyAfter bool) error {
	if err := d.failIfResetInProgress(); err != nil {
		return err
	}
	return rootruntime.ReloadAfterConfigChange(
		rootruntime.ConfigReloadInput{
			Config:               cfg,
			Context:              context,
			SavedLabel:           savedLabel,
			Generation:           generation,
			FullRestart:          fullRestart,
			NetstackReapplyAfter: netstackReapplyAfter,
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
			ResetRescueState: d.rescueMgr.Reset,
			StartSubsystems:  d.startSubsystems,
			RefreshHealth:    d.runtimeV2.RefreshHealth,
			ObserveReloadReport: func(report core.RuntimeStageReport) {
				d.setLastReloadReport(report)
				d.publishReloadOperationStep(generation, report)
			},
		},
	)
}

func (d *daemon) publishReloadOperationStep(generation int64, report core.RuntimeStageReport) {
	if d.runtimeV2 == nil || len(report.Stages) == 0 {
		return
	}
	stage := report.Stages[len(report.Stages)-1]
	d.runtimeV2.SetActiveOperationStep(
		generation,
		stage.Name,
		stage.Status,
		stage.Code,
		stage.Detail,
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
