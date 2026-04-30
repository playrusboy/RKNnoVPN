package control

import (
	"testing"
	"time"

	"github.com/youtubediscord/RKNnoVPN/daemon/internal/ipc"
	"github.com/youtubediscord/RKNnoVPN/daemon/internal/runtimev2"
)

func TestUpdateDownloadRejectsActiveRuntimeOperation(t *testing.T) {
	active := runtimev2.OperationStatus{
		Kind:       runtimev2.OperationUpdateInstall,
		Generation: 7,
		StartedAt:  time.Now(),
	}
	handlers := UpdateHandlers{
		RuntimeStatus: func() (runtimev2.Status, bool) {
			return runtimev2.Status{ActiveOperation: &active}, true
		},
		RuntimeError: func(err error) *ipc.RPCError {
			return &ipc.RPCError{Code: ipc.CodeRuntimeBusy, Message: err.Error()}
		},
	}

	if _, rpcErr := handlers.UpdateDownload(nil); rpcErr == nil || rpcErr.Code != ipc.CodeRuntimeBusy {
		t.Fatalf("expected update-download to reject active runtime operation, got %#v", rpcErr)
	}
}
