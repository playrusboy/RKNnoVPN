package control

import (
	"context"
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

func TestUpdateInstallContextCancelledSkipsOperationRunner(t *testing.T) {
	runnerCalled := false
	handlers := UpdateHandlers{
		RunUpdateInstallOperation: func(func(generation int64) error) (runtimev2.Status, error) {
			runnerCalled = true
			return runtimev2.Status{}, nil
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, rpcErr := handlers.UpdateInstallContext(ctx, nil); rpcErr == nil {
		t.Fatal("expected cancelled context error")
	}
	if runnerCalled {
		t.Fatal("cancelled update-install should not start the operation runner")
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
