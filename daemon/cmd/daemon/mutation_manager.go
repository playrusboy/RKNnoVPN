package main

import (
	"fmt"

	applytx "github.com/youtubediscord/RKNnoVPN/daemon/internal/apply"
	"github.com/youtubediscord/RKNnoVPN/daemon/internal/config"
	"github.com/youtubediscord/RKNnoVPN/daemon/internal/core"
	profiledoc "github.com/youtubediscord/RKNnoVPN/daemon/internal/profile"
	"github.com/youtubediscord/RKNnoVPN/daemon/internal/runtimev2"
)

// persistProfileConfigMutationForAction is the daemon-owned transaction gate
// for every mutation that changes user intent and its runtime projection.
func (d *daemon) persistProfileConfigMutationForAction(nextCfg *config.Config, reload bool, action string) (applytx.ConfigTransactionResult, error) {
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
		RuntimeRunning: func() bool {
			state := d.coreMgr.GetState()
			return state == core.StateRunning || state == core.StateDegraded
		},
		ApplyConfig: func(nextCfg *config.Config, reload bool, operation runtimev2.OperationKind) error {
			return d.applyConfigWithOperation(nextCfg, reload, operation)
		},
	}.Run(nextCfg, reload)
}
