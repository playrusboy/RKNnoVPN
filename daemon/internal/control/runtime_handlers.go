package control

import (
	"encoding/json"
	"os"
	"time"

	"github.com/youtubediscord/RKNnoVPN/daemon/internal/ipc"
	"github.com/youtubediscord/RKNnoVPN/daemon/internal/runtimev2"
	"github.com/youtubediscord/RKNnoVPN/daemon/internal/updater"
)

type RuntimeHandlers struct {
	DataDir               string
	Initialized           func() bool
	RefreshCompatibility  func()
	RefreshActiveProgress func() runtimev2.Status
	Status                func() runtimev2.Status
	IsRunningOrDegraded   func() bool
	CurrentHealth         func() runtimev2.HealthSnapshot
	RefreshHealth         func() runtimev2.HealthSnapshot
	ApplyDesiredState     func(runtimev2.DesiredState) (runtimev2.Status, error)
	SyncDesiredState      func() error
	Start                 func() (runtimev2.Status, error)
	Stop                  func() (runtimev2.Status, error)
	Restart               func() (runtimev2.Status, error)
	Reset                 func() (runtimev2.Status, error)
}

func (h RuntimeHandlers) BackendStatus(params *json.RawMessage) (interface{}, *ipc.RPCError) {
	if !h.initialized() {
		return nil, &ipc.RPCError{Code: ipc.CodeInternalError, Message: "v2 runtime is not initialized"}
	}
	if h.RefreshCompatibility != nil {
		h.RefreshCompatibility()
	}
	status := h.refreshActiveProgress()
	if status.ActiveOperation != nil {
		return h.statusWithUpdateInstallState(status), nil
	}
	if h.IsRunningOrDegraded != nil && h.IsRunningOrDegraded() {
		healthSnapshot := h.currentHealth()
		if healthSnapshot.CheckedAt.IsZero() || time.Since(healthSnapshot.CheckedAt) > 10*time.Second {
			go h.refreshHealth()
		}
	}
	return h.statusWithUpdateInstallState(h.status()), nil
}

func (h RuntimeHandlers) BackendApplyDesiredState(params *json.RawMessage) (interface{}, *ipc.RPCError) {
	if params == nil {
		return nil, &ipc.RPCError{
			Code:    ipc.CodeInvalidParams,
			Message: "params required: desired state object",
		}
	}
	var desired runtimev2.DesiredState
	if err := json.Unmarshal(*params, &desired); err != nil {
		return nil, &ipc.RPCError{Code: ipc.CodeInvalidParams, Message: "invalid params: " + err.Error()}
	}
	if h.ApplyDesiredState == nil {
		return nil, &ipc.RPCError{Code: ipc.CodeInternalError, Message: "desired state apply callback is not configured"}
	}
	status, err := h.ApplyDesiredState(desired)
	if err != nil {
		return nil, DesiredStateApplyRPCError(err)
	}
	return status, nil
}

func (h RuntimeHandlers) BackendStart(params *json.RawMessage) (interface{}, *ipc.RPCError) {
	if h.SyncDesiredState != nil {
		if err := h.SyncDesiredState(); err != nil {
			return nil, RuntimeRPCError(err)
		}
	}
	if h.Start == nil {
		return nil, &ipc.RPCError{Code: ipc.CodeInternalError, Message: "runtime start callback is not configured"}
	}
	status, err := h.Start()
	if err != nil {
		return nil, RuntimeRPCError(err)
	}
	return status, nil
}

func (h RuntimeHandlers) BackendStop(params *json.RawMessage) (interface{}, *ipc.RPCError) {
	if h.Stop == nil {
		return nil, &ipc.RPCError{Code: ipc.CodeInternalError, Message: "runtime stop callback is not configured"}
	}
	status, err := h.Stop()
	if err != nil {
		return nil, RuntimeRPCError(err)
	}
	return status, nil
}

func (h RuntimeHandlers) BackendRestart(params *json.RawMessage) (interface{}, *ipc.RPCError) {
	if h.SyncDesiredState != nil {
		if err := h.SyncDesiredState(); err != nil {
			return nil, RuntimeRPCError(err)
		}
	}
	if h.Restart == nil {
		return nil, &ipc.RPCError{Code: ipc.CodeInternalError, Message: "runtime restart callback is not configured"}
	}
	status, err := h.Restart()
	if err != nil {
		return nil, RuntimeRPCError(err)
	}
	return status, nil
}

func (h RuntimeHandlers) BackendReset(params *json.RawMessage) (interface{}, *ipc.RPCError) {
	if h.Reset == nil {
		return nil, &ipc.RPCError{Code: ipc.CodeInternalError, Message: "runtime reset callback is not configured"}
	}
	status, err := h.Reset()
	if err != nil {
		return nil, RuntimeRPCError(err)
	}
	return status, nil
}

func (h RuntimeHandlers) statusWithUpdateInstallState(status runtimev2.Status) runtimev2.Status {
	state, err := updater.ReadInstallState(h.DataDir)
	if err == nil {
		status.UpdateInstall = &runtimev2.UpdateInstallState{
			Status:          state.Status,
			Generation:      state.Generation,
			Step:            state.Step,
			StepStatus:      state.StepStatus,
			Code:            state.Code,
			Detail:          state.Detail,
			ModulePath:      state.ModulePath,
			ApkPath:         state.ApkPath,
			ApkInstalled:    state.ApkInstalled,
			ModuleInstalled: state.ModuleInstalled,
			StartedAt:       state.StartedAt,
			UpdatedAt:       state.UpdatedAt,
		}
		return status
	}
	if os.IsNotExist(err) {
		return status
	}
	status.UpdateInstall = &runtimev2.UpdateInstallState{
		Status: "unknown",
		Code:   "UPDATE_INSTALL_STATE_INVALID",
		Detail: err.Error(),
	}
	return status
}

func (h RuntimeHandlers) initialized() bool {
	if h.Initialized != nil {
		return h.Initialized()
	}
	return h.Status != nil
}

func (h RuntimeHandlers) refreshActiveProgress() runtimev2.Status {
	if h.RefreshActiveProgress != nil {
		return h.RefreshActiveProgress()
	}
	return h.status()
}

func (h RuntimeHandlers) status() runtimev2.Status {
	if h.Status != nil {
		return h.Status()
	}
	return runtimev2.Status{}
}

func (h RuntimeHandlers) currentHealth() runtimev2.HealthSnapshot {
	if h.CurrentHealth != nil {
		return h.CurrentHealth()
	}
	return runtimev2.HealthSnapshot{}
}

func (h RuntimeHandlers) refreshHealth() runtimev2.HealthSnapshot {
	if h.RefreshHealth != nil {
		return h.RefreshHealth()
	}
	return runtimev2.HealthSnapshot{}
}
