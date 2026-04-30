package main

import (
	"errors"
	"strings"

	"github.com/youtubediscord/RKNnoVPN/daemon/internal/control"
	"github.com/youtubediscord/RKNnoVPN/daemon/internal/core"
	"github.com/youtubediscord/RKNnoVPN/daemon/internal/ipc"
	"github.com/youtubediscord/RKNnoVPN/daemon/internal/runtimev2"
)

func (d *daemon) runtimeControlHandlers() control.RuntimeHandlers {
	return control.RuntimeHandlers{
		DataDir: d.dataDir,
		Initialized: func() bool {
			return d.runtimeV2 != nil
		},
		RefreshCompatibility: d.refreshRuntimeV2Compatibility,
		RefreshActiveProgress: func() runtimev2.Status {
			return d.runtimeV2.RefreshActiveProgress()
		},
		Status: func() runtimev2.Status {
			return d.runtimeV2.Status()
		},
		IsRunningOrDegraded: func() bool {
			state := d.coreMgr.GetState()
			return state == core.StateRunning || state == core.StateDegraded
		},
		CurrentHealth: func() runtimev2.HealthSnapshot {
			return d.runtimeV2.CurrentHealth()
		},
		RefreshHealth: func() runtimev2.HealthSnapshot {
			return d.runtimeV2.RefreshHealth()
		},
		ApplyDesiredState:      d.applyDesiredStateV2,
		DesiredStateApplyError: d.rpcErrorFromDesiredStateApplyError,
		SyncDesiredState:       d.syncRuntimeV2DesiredState,
		Start: func() (runtimev2.Status, error) {
			return d.runtimeV2.Start()
		},
		Stop: func() (runtimev2.Status, error) {
			return d.runtimeV2.Stop()
		},
		Restart: func() (runtimev2.Status, error) {
			return d.runtimeV2.Restart()
		},
		Reset: func() (runtimev2.Status, error) {
			return d.runtimeV2.Reset()
		},
		TestNodes: func(url string, timeoutMS int, nodeIDs []string) ([]runtimev2.NodeProbeResult, error) {
			return d.runtimeV2.TestNodes(url, timeoutMS, nodeIDs)
		},
		RuntimeError: d.rpcErrorFromRuntimeError,
	}
}

func (d *daemon) controlRuntimeStatus() (runtimev2.Status, bool) {
	if d.runtimeV2 == nil {
		return runtimev2.Status{}, false
	}
	return d.runtimeV2.Status(), true
}

func (d *daemon) rpcErrorFromDesiredStateApplyError(err error) *ipc.RPCError {
	var applyErr desiredStateApplyError
	if !errors.As(err, &applyErr) {
		return d.rpcErrorFromRuntimeError(err)
	}
	if rpcErr := d.rpcErrorFromRuntimeError(applyErr.err); rpcErr.Code == ipc.CodeRuntimeBusy {
		return rpcErr
	}
	if applyErr.stage == desiredStateApplyStageValidate {
		return &ipc.RPCError{Code: ipc.CodeInvalidParams, Message: applyErr.err.Error()}
	}
	return &ipc.RPCError{Code: ipc.CodeInternalError, Message: applyErr.err.Error()}
}

func (d *daemon) rpcErrorFromRuntimeError(err error) *ipc.RPCError {
	var busy *runtimev2.OperationBusyError
	if errors.As(err, &busy) {
		return &ipc.RPCError{
			Code:    ipc.CodeRuntimeBusy,
			Message: busy.Error(),
			Data:    busy.Data(),
		}
	}
	if strings.Contains(err.Error(), "no node configured") {
		return &ipc.RPCError{Code: ipc.CodeConfigError, Message: err.Error()}
	}
	return &ipc.RPCError{Code: ipc.CodeInternalError, Message: err.Error()}
}
