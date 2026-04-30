package main

import (
	"fmt"

	"github.com/youtubediscord/RKNnoVPN/daemon/internal/control"
	rootruntime "github.com/youtubediscord/RKNnoVPN/daemon/internal/runtime/root"
	"github.com/youtubediscord/RKNnoVPN/daemon/internal/runtimev2"
)

func (d *daemon) desiredStateV2() runtimev2.DesiredState {
	return rootruntime.DesiredStateFromConfig(d.currentConfig())
}

func (d *daemon) syncRuntimeV2DesiredState() error {
	if d.runtimeV2 == nil {
		return nil
	}
	return d.runtimeV2.ApplyDesiredState(d.desiredStateV2())
}

func (d *daemon) applyDesiredStateV2(desired runtimev2.DesiredState) (runtimev2.Status, error) {
	desired = rootruntime.CompleteDesiredState(desired, d.desiredStateV2())
	if err := d.runtimeV2.ValidateDesiredState(desired); err != nil {
		return runtimev2.Status{}, control.DesiredStateApplyError{Stage: control.DesiredStateApplyStageValidate, Err: err}
	}
	if err := d.persistDesiredStateV2(desired); err != nil {
		return runtimev2.Status{}, control.DesiredStateApplyError{Stage: control.DesiredStateApplyStagePersist, Err: fmt.Errorf("persist desired state: %w", err)}
	}
	return d.runtimeV2.Status(), nil
}

func (d *daemon) persistDesiredStateV2(desired runtimev2.DesiredState) error {
	nextCfg, err := rootruntime.ApplyDesiredStateToConfig(d.currentConfig(), desired)
	if err != nil {
		return err
	}
	if _, err := d.persistConfigMutationForAction(nextCfg, false, "backend.applyDesiredState"); err != nil {
		return err
	}
	return nil
}
