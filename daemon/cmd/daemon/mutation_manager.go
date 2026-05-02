package main

import (
	"fmt"

	applytx "github.com/youtubediscord/RKNnoVPN/daemon/internal/apply"
	"github.com/youtubediscord/RKNnoVPN/daemon/internal/config"
	profiledoc "github.com/youtubediscord/RKNnoVPN/daemon/internal/profile"
	rootruntime "github.com/youtubediscord/RKNnoVPN/daemon/internal/runtime/root"
	"github.com/youtubediscord/RKNnoVPN/daemon/internal/runtimev2"
)

// persistConfigMutationForAction is the daemon-owned transaction gate
// for every mutation that changes user intent and its runtime projection.
func (d *daemon) persistConfigMutationForAction(nextCfg *config.Config, reload bool, action string) (applytx.ConfigTransactionResult, error) {
	currentCfg := d.currentConfig()
	reloadPlan := rootruntime.PlanReload(
		rootruntime.BuildScriptEnv(currentCfg, d.dataDir),
		rootruntime.BuildScriptEnv(nextCfg, d.dataDir),
	)
	selectorSwitchTag := activeNodeSelectorSwitchTarget(currentCfg, nextCfg, reloadPlan)
	return applytx.ConfigTransaction{
		Action:     action,
		EnsureIdle: d.failIfRuntimeOperationActive,
		ValidateConfig: func(nextCfg *config.Config) error {
			if err := nextCfg.Validate(); err != nil {
				return fmt.Errorf("validation failed: %w", err)
			}
			return nil
		},
		CheckRuntimeProjection: func(nextCfg *config.Config) error {
			if selectorSwitchTag != "" {
				return nil
			}
			profile := nextCfg.ResolveProfile()
			if profile == nil || profile.Address == "" {
				return nil
			}
			if _, err := config.RenderSingboxConfig(nextCfg, profile); err != nil {
				return fmt.Errorf("render validation failed: %w", err)
			}
			return nil
		},
		SaveProfile: func(nextCfg *config.Config) error {
			return profiledoc.Save(d.profilePath, profiledoc.FromConfig(nextCfg))
		},
		RuntimeRunning: d.isRuntimeRunningOrDegraded,
		ApplyConfig: func(nextCfg *config.Config, reload bool, operation runtimev2.OperationKind) error {
			return d.applyConfigWithOperation(nextCfg, reload, operation)
		},
	}.Run(nextCfg, reload)
}
