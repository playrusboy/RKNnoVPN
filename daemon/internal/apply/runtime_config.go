package applytx

import (
	"fmt"

	"github.com/youtubediscord/RKNnoVPN/daemon/internal/config"
	"github.com/youtubediscord/RKNnoVPN/daemon/internal/runtimev2"
)

type RuntimeConfigApplyInput struct {
	NewConfig            *config.Config
	ConfigPath           string
	Reload               bool
	Operation            runtimev2.OperationKind
	WasRunning           bool
	NeedsFullRestart     bool
	NeedsNetstackReapply bool
}

type RuntimeConfigApplyDeps struct {
	EnsureIdle       func() error
	CommitConfig     func(*config.Config)
	SyncDesiredState func() error
	RunOperation     func(runtimev2.OperationKind, runtimev2.Phase, func(generation int64) error) error
	ReloadRuntime    func(cfg *config.Config, generation int64, fullRestart bool, netstackReapplyAfter bool) error
}

func ApplyRuntimeConfig(input RuntimeConfigApplyInput, deps RuntimeConfigApplyDeps) error {
	if input.NewConfig == nil {
		return fmt.Errorf("config is nil")
	}
	operation := input.Operation
	if operation == "" {
		operation = runtimev2.OperationReload
	}
	if deps.EnsureIdle != nil {
		if err := deps.EnsureIdle(); err != nil {
			return err
		}
	}
	if err := input.NewConfig.Save(input.ConfigPath); err != nil {
		return fmt.Errorf("persist config: %w", err)
	}
	if deps.CommitConfig != nil {
		deps.CommitConfig(input.NewConfig)
	}
	if deps.SyncDesiredState != nil {
		if err := deps.SyncDesiredState(); err != nil {
			return fmt.Errorf("config saved: sync runtime desired state: %w", err)
		}
	}
	if !input.Reload || !input.WasRunning {
		return nil
	}
	if deps.RunOperation == nil {
		return fmt.Errorf("config saved: runtime operation runner is not configured")
	}
	if deps.ReloadRuntime == nil {
		return fmt.Errorf("config saved: runtime reload hook is not configured")
	}
	if err := deps.RunOperation(operation, runtimev2.PhaseStarting, func(generation int64) error {
		return deps.ReloadRuntime(input.NewConfig, generation, input.NeedsFullRestart, input.NeedsNetstackReapply)
	}); err != nil {
		return fmt.Errorf("config saved: %w", err)
	}
	return nil
}
