package control

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/youtubediscord/RKNnoVPN/daemon/internal/ipc"
	"github.com/youtubediscord/RKNnoVPN/daemon/internal/runtimev2"
)

func TestBackendStatusCanRefreshHealthInline(t *testing.T) {
	var refreshHealthCalls int
	health := runtimev2.HealthSnapshot{}
	params := json.RawMessage(`{"includeHealthRefresh":true}`)
	handlers := RuntimeHandlers{
		DataDir: t.TempDir(),
		Status: func() runtimev2.Status {
			return runtimev2.Status{
				AppliedState: runtimev2.AppliedState{
					BackendKind: runtimev2.BackendRootTProxy,
					Phase:       runtimev2.PhaseHealthy,
					StartedAt:   time.Now().Add(-5 * time.Second),
				},
				Health: health,
			}
		},
		RefreshHealth: func() runtimev2.HealthSnapshot {
			refreshHealthCalls++
			health = runtimev2.HealthSnapshot{
				CoreReady:    true,
				RoutingReady: true,
				DNSReady:     true,
				EgressReady:  true,
				CheckedAt:    time.Now(),
			}
			return health
		},
	}

	payload, rpcErr := handlers.BackendStatus(&params)
	if rpcErr != nil {
		t.Fatalf("backend.status failed: %#v", rpcErr)
	}
	if refreshHealthCalls != 1 {
		t.Fatalf("expected inline health refresh, got %d calls", refreshHealthCalls)
	}
	status, ok := payload.(runtimev2.Status)
	if !ok {
		t.Fatalf("unexpected payload type %T", payload)
	}
	if !status.Health.OperationalHealthy() {
		t.Fatalf("expected refreshed status health in response, got %#v", status.Health)
	}
}

func TestBackendStatusRejectsUnknownParams(t *testing.T) {
	params := json.RawMessage(`{"refreshHealth":true}`)
	handlers := RuntimeHandlers{
		DataDir: t.TempDir(),
		Status:  func() runtimev2.Status { return runtimev2.Status{} },
	}

	if _, rpcErr := handlers.BackendStatus(&params); rpcErr == nil || rpcErr.Code != ipc.CodeInvalidParams {
		t.Fatalf("expected invalid params error, got %#v", rpcErr)
	}
}
