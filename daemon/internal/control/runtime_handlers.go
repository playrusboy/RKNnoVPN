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
	RuntimeStats          func(runtimev2.Status) runtimev2.TrafficStats
	IsRunningOrDegraded   func() bool
	CurrentHealth         func() runtimev2.HealthSnapshot
	RefreshHealth         func() runtimev2.HealthSnapshot
	ApplyDesiredState     func(runtimev2.DesiredState) (runtimev2.Status, error)
	SyncDesiredState      func() error
	Start                 func() (runtimev2.Status, error)
	Stop                  func() (runtimev2.Status, error)
	Restart               func() (runtimev2.Status, error)
	Reset                 func() (runtimev2.Status, error)
	UpdateCheckState      func() (*updater.UpdateCheckState, error)
}

type BackendStatusRequest struct {
	IncludeHealthRefresh bool `json:"includeHealthRefresh"`
	IncludeCompatibility bool `json:"includeCompatibility"`
}

func (h RuntimeHandlers) BackendStatus(params *json.RawMessage) (interface{}, *ipc.RPCError) {
	if !h.initialized() {
		return nil, &ipc.RPCError{Code: ipc.CodeInternalError, Message: "v2 runtime is not initialized"}
	}
	request, err := DecodeBackendStatusParams(params)
	if err != nil {
		return nil, &ipc.RPCError{
			Code:    ipc.CodeInvalidParams,
			Message: err.Error(),
		}
	}
	if request.IncludeCompatibility && h.RefreshCompatibility != nil {
		h.RefreshCompatibility()
	}
	if request.IncludeHealthRefresh {
		h.refreshHealth()
		return h.statusPayload(h.status(), request.IncludeCompatibility), nil
	}
	return h.statusPayload(h.status(), request.IncludeCompatibility), nil
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
	return h.finalizeStatus(status), nil
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
	return h.finalizeStatus(status), nil
}

func (h RuntimeHandlers) BackendStop(params *json.RawMessage) (interface{}, *ipc.RPCError) {
	if h.Stop == nil {
		return nil, &ipc.RPCError{Code: ipc.CodeInternalError, Message: "runtime stop callback is not configured"}
	}
	status, err := h.Stop()
	if err != nil {
		return nil, RuntimeRPCError(err)
	}
	return h.finalizeStatus(status), nil
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
	return h.finalizeStatus(status), nil
}

func (h RuntimeHandlers) BackendReset(params *json.RawMessage) (interface{}, *ipc.RPCError) {
	if h.Reset == nil {
		return nil, &ipc.RPCError{Code: ipc.CodeInternalError, Message: "runtime reset callback is not configured"}
	}
	status, err := h.Reset()
	if err != nil {
		return nil, RuntimeRPCError(err)
	}
	return h.finalizeStatus(status), nil
}

func (h RuntimeHandlers) finalizeStatus(status runtimev2.Status) runtimev2.Status {
	if status.AppliedState.Phase != runtimev2.PhaseStopped && !status.AppliedState.StartedAt.IsZero() {
		status.UptimeSeconds = int64(time.Since(status.AppliedState.StartedAt).Seconds())
		if status.UptimeSeconds < 0 {
			status.UptimeSeconds = 0
		}
	}
	if h.RuntimeStats != nil {
		status.Traffic = h.RuntimeStats(status)
	}
	return h.statusWithUpdateInstallState(status)
}

func (h RuntimeHandlers) statusPayload(status runtimev2.Status, includeCompatibility bool) interface{} {
	status = h.finalizeStatus(status)
	if includeCompatibility {
		return status
	}
	return backendStatusResponse{
		DesiredState:    status.DesiredState,
		AppliedState:    status.AppliedState,
		Canonical:       status.Canonical,
		Health:          status.Health,
		UptimeSeconds:   status.UptimeSeconds,
		Traffic:         status.Traffic,
		Capabilities:    status.Capabilities,
		ActiveOperation: status.ActiveOperation,
		LastOperation:   status.LastOperation,
		UpdateInstall:   status.UpdateInstall,
		UpdateCheck:     h.updateCheckState(),
	}
}

type backendStatusResponse struct {
	DesiredState    runtimev2.DesiredState        `json:"desiredState"`
	AppliedState    runtimev2.AppliedState        `json:"appliedState"`
	Canonical       runtimev2.CanonicalStatus     `json:"canonical"`
	Health          runtimev2.HealthSnapshot      `json:"health"`
	UptimeSeconds   int64                         `json:"uptimeSeconds,omitempty"`
	Traffic         runtimev2.TrafficStats        `json:"traffic,omitempty"`
	Capabilities    []runtimev2.BackendCapability `json:"capabilities"`
	ActiveOperation *runtimev2.OperationStatus    `json:"activeOperation,omitempty"`
	LastOperation   *runtimev2.OperationResult    `json:"lastOperation,omitempty"`
	UpdateInstall   *runtimev2.UpdateInstallState `json:"updateInstall,omitempty"`
	UpdateCheck     *updater.UpdateCheckState     `json:"updateCheck,omitempty"`
}

func (h RuntimeHandlers) updateCheckState() *updater.UpdateCheckState {
	if h.UpdateCheckState == nil {
		return nil
	}
	state, err := h.UpdateCheckState()
	if err != nil {
		return &updater.UpdateCheckState{Error: err.Error()}
	}
	return state
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
