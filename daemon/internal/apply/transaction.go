package applytx

import (
	"fmt"

	"github.com/youtubediscord/RKNnoVPN/daemon/internal/config"
	"github.com/youtubediscord/RKNnoVPN/daemon/internal/ipc"
	"github.com/youtubediscord/RKNnoVPN/daemon/internal/runtimev2"
)

type ConfigTransactionResult struct {
	Action             string
	ConfigSaved        bool
	RuntimeWasRunning  bool
	RuntimeOperation   runtimev2.OperationKind
	RuntimeApply       string
	RuntimeApplied     bool
	RuntimeApplyMode   string
	RuntimeApplyReason string
	RequiresHotSwap    bool
}

type ConfigApplyResult struct {
	RuntimeApply       string
	RuntimeApplied     bool
	RuntimeApplyMode   string
	RuntimeApplyReason string
	RequiresHotSwap    bool
}

type ConfigTransaction struct {
	Action                 string
	ValidateConfig         func(*config.Config) error
	CheckRuntimeProjection func(*config.Config) error
	EnsureIdle             func() error
	SaveProfile            func(*config.Config) error
	ApplyConfig            func(*config.Config, bool, runtimev2.OperationKind) error
	ApplyConfigWithResult  func(*config.Config, bool, runtimev2.OperationKind) (ConfigApplyResult, error)
	RuntimeRunning         func() bool
}

func (tx ConfigTransaction) Run(nextCfg *config.Config, reload bool) (ConfigTransactionResult, error) {
	var result ConfigTransactionResult
	if nextCfg == nil {
		return result, fmt.Errorf("config is nil")
	}
	if tx.Action == "" {
		return result, fmt.Errorf("mutation action is not configured")
	}
	result.Action = tx.Action
	operation, ok := RuntimeOperationForAction(tx.Action)
	if !ok {
		return result, fmt.Errorf("unknown config mutation action %q", tx.Action)
	}
	result.RuntimeOperation = operation
	if tx.ValidateConfig != nil {
		if err := tx.ValidateConfig(nextCfg); err != nil {
			return result, ConfigValidationError{Stage: "validate", Err: err}
		}
	}
	if tx.CheckRuntimeProjection != nil {
		if err := tx.CheckRuntimeProjection(nextCfg); err != nil {
			return result, ConfigValidationError{Stage: "render", Err: err}
		}
	}
	if tx.EnsureIdle != nil {
		if err := tx.EnsureIdle(); err != nil {
			return result, err
		}
	}
	if tx.SaveProfile == nil {
		return result, fmt.Errorf("profile persistence is not configured")
	}
	if err := tx.SaveProfile(nextCfg); err != nil {
		return result, fmt.Errorf("persist profile: %w", err)
	}
	result.ConfigSaved = true
	if tx.RuntimeRunning != nil {
		result.RuntimeWasRunning = tx.RuntimeRunning()
	}
	if tx.ApplyConfigWithResult != nil {
		applyResult, err := tx.ApplyConfigWithResult(nextCfg, reload, result.RuntimeOperation)
		if err != nil {
			return result, err
		}
		result.RuntimeApply = applyResult.RuntimeApply
		result.RuntimeApplied = applyResult.RuntimeApplied
		result.RuntimeApplyMode = applyResult.RuntimeApplyMode
		result.RuntimeApplyReason = applyResult.RuntimeApplyReason
		result.RequiresHotSwap = applyResult.RequiresHotSwap
		return result, nil
	}
	if tx.ApplyConfig == nil {
		return result, fmt.Errorf("config apply is not configured")
	}
	if err := tx.ApplyConfig(nextCfg, reload, result.RuntimeOperation); err != nil {
		return result, err
	}
	return result, nil
}

type ConfigValidationError struct {
	Stage string
	Err   error
}

func (e ConfigValidationError) Error() string {
	if e.Err == nil {
		return e.Stage
	}
	if e.Stage == "" {
		return e.Err.Error()
	}
	return e.Stage + ": " + e.Err.Error()
}

func (e ConfigValidationError) Unwrap() error {
	return e.Err
}

func (e ConfigValidationError) RuntimeCode() string {
	switch e.Stage {
	case "validate":
		return "CONFIG_VALIDATION_FAILED"
	case "render":
		return "CONFIG_RENDER_FAILED"
	default:
		return "CONFIG_APPLY_FAILED"
	}
}

func RuntimeOperationForAction(action string) (runtimev2.OperationKind, bool) {
	operationType, ok := ipc.MethodOperationType(action)
	if !ok {
		return "", false
	}
	return RuntimeOperationForType(operationType)
}

func RuntimeOperationForType(operationType string) (runtimev2.OperationKind, bool) {
	switch operationType {
	case "config-mutation":
		return runtimev2.OperationConfigMutation, true
	case "profile-apply":
		return runtimev2.OperationProfileApply, true
	case "applyDesiredState":
		return runtimev2.OperationApplyDesiredState, true
	default:
		return "", false
	}
}
