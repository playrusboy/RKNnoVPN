package control

import (
	"context"
	"strings"
	"testing"

	"github.com/youtubediscord/RKNnoVPN/daemon/internal/runtimev2"
)

func TestDiagnosticsTestNodesContextHonorsCancelledContext(t *testing.T) {
	called := false
	handler := DiagnosticsHandlers{
		TestNodes: func(url string, timeoutMS int, nodeIDs []string) []runtimev2.NodeProbeResult {
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
