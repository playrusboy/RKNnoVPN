package control

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/youtubediscord/RKNnoVPN/daemon/internal/runtimev2"
)

func TestDiagnosticsTestNodesContextHonorsCancelledContext(t *testing.T) {
	called := false
	handler := DiagnosticsHandlers{
		TestNodes: func(url string, timeoutMS int, nodeIDs []string, mode string) []runtimev2.NodeProbeResult {
			called = true
			return nil
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, rpcErr := handler.DiagnosticsTestNodesContext(ctx, nil)
	if rpcErr == nil || !strings.Contains(rpcErr.Message, context.Canceled.Error()) {
		t.Fatalf("expected context cancellation error, got %#v", rpcErr)
	}
	if called {
		t.Fatal("cancelled context should not start node probes")
	}
}

func TestDiagnosticsTestNodesPassesNormalizedMode(t *testing.T) {
	gotMode := ""
	handler := DiagnosticsHandlers{
		TestNodes: func(url string, timeoutMS int, nodeIDs []string, mode string) []runtimev2.NodeProbeResult {
			gotMode = mode
			return []runtimev2.NodeProbeResult{{ID: "node-a"}}
		},
	}
	raw := json.RawMessage(`{"mode":"throughput"}`)

	result, rpcErr := handler.DiagnosticsTestNodes(&raw)
	if rpcErr != nil {
		t.Fatalf("unexpected rpc error: %#v", rpcErr)
	}
	if gotMode != "full" {
		t.Fatalf("mode = %q, want full", gotMode)
	}
	obj, ok := result.(map[string]interface{})
	if !ok || obj["mode"] != "full" {
		t.Fatalf("unexpected result: %#v", result)
	}
}
