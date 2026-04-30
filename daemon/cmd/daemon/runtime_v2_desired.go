package main

import (
	"fmt"

	rootruntime "github.com/youtubediscord/RKNnoVPN/daemon/internal/runtime/root"
	"github.com/youtubediscord/RKNnoVPN/daemon/internal/runtimev2"
)

type desiredStateApplyStage string

const (
	desiredStateApplyStageValidate desiredStateApplyStage = "validate"
	desiredStateApplyStagePersist  desiredStateApplyStage = "persist"
)

type desiredStateApplyError struct {
	stage desiredStateApplyStage
	err   error
}

func (e desiredStateApplyError) Error() string {
	if e.err == nil {
		return ""
	}
	return e.err.Error()
}

func (e desiredStateApplyError) Unwrap() error {
	return e.err
}

func (d *daemon) desiredStateV2() runtimev2.DesiredState {
	d.mu.Lock()
	cfg := d.cfg
	d.mu.Unlock()

	return rootruntime.DesiredStateFromConfig(cfg)
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
		return runtimev2.Status{}, desiredStateApplyError{stage: desiredStateApplyStageValidate, err: err}
	}
	if err := d.persistDesiredStateV2(desired); err != nil {
		return runtimev2.Status{}, desiredStateApplyError{stage: desiredStateApplyStagePersist, err: fmt.Errorf("persist desired state: %w", err)}
	}
	return d.runtimeV2.Status(), nil
}

func (d *daemon) persistDesiredStateV2(desired runtimev2.DesiredState) error {
	d.mu.Lock()
	currentCfg := d.cfg
	d.mu.Unlock()

	nextCfg, err := rootruntime.ApplyDesiredStateToConfig(currentCfg, desired)
	if err != nil {
		return err
	}
	if _, err := d.persistProfileConfigMutationForAction(nextCfg, false, "backend.applyDesiredState"); err != nil {
		return err
	}
	return nil
}
