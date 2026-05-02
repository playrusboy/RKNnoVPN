package control

import (
	"encoding/json"
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
	}

	if _, rpcErr := handlers.UpdateDownload(nil); rpcErr == nil || rpcErr.Code != ipc.CodeRuntimeBusy {
		t.Fatalf("expected update-download to reject active runtime operation, got %#v", rpcErr)
	}
}

func TestCurrentUpdateVersionIgnoresAPKProvidedVersion(t *testing.T) {
	raw := json.RawMessage(`{"current_version":"2.2.0"}`)
	handlers := UpdateHandlers{Version: "v2.2.1"}

	if got := handlers.currentUpdateVersion(&raw); got != "v2.2.1" {
		t.Fatalf("currentUpdateVersion = %q, want v2.2.1", got)
	}
}

func TestCurrentUpdateVersionFallsBackToDaemonVersion(t *testing.T) {
	raw := json.RawMessage(`{"current_version":""}`)
	handlers := UpdateHandlers{Version: "2.2.1"}

	if got := handlers.currentUpdateVersion(&raw); got != "v2.2.1" {
		t.Fatalf("currentUpdateVersion = %q, want v2.2.1", got)
	}
}
