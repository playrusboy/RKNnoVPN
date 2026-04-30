package control

import (
	"errors"
	"strings"

	"github.com/youtubediscord/RKNnoVPN/daemon/internal/ipc"
	"github.com/youtubediscord/RKNnoVPN/daemon/internal/runtimev2"
)

type DesiredStateApplyStage string

const (
	DesiredStateApplyStageValidate DesiredStateApplyStage = "validate"
	DesiredStateApplyStagePersist  DesiredStateApplyStage = "persist"
)

type DesiredStateApplyError struct {
	Stage DesiredStateApplyStage
	Err   error
}

func (e DesiredStateApplyError) Error() string {
	if e.Err == nil {
		return ""
	}
	return e.Err.Error()
}

func (e DesiredStateApplyError) Unwrap() error {
	return e.Err
}

func RuntimeRPCError(err error) *ipc.RPCError {
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

func DesiredStateApplyRPCError(err error) *ipc.RPCError {
	var applyErr DesiredStateApplyError
	if !errors.As(err, &applyErr) {
		return RuntimeRPCError(err)
	}
	if rpcErr := RuntimeRPCError(applyErr.Err); rpcErr.Code == ipc.CodeRuntimeBusy {
		return rpcErr
	}
	if applyErr.Stage == DesiredStateApplyStageValidate {
		return &ipc.RPCError{Code: ipc.CodeInvalidParams, Message: applyErr.Err.Error()}
	}
	return &ipc.RPCError{Code: ipc.CodeInternalError, Message: applyErr.Err.Error()}
}
