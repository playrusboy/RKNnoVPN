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

func TestBackendStatusCanOmitCompatibility(t *testing.T) {
	params := json.RawMessage(`{"includeCompatibility":false}`)
	var refreshCompatibilityCalls int
	handlers := RuntimeHandlers{
		DataDir: t.TempDir(),
		Status: func() runtimev2.Status {
			return runtimev2.Status{
				AppliedState: runtimev2.AppliedState{Phase: runtimev2.PhaseStopped},
				Compatibility: runtimev2.CompatibilityStatus{
					ControlProtocolVersion: 5,
					SchemaVersion:          5,
				},
			}
		},
		RefreshCompatibility: func() {
			refreshCompatibilityCalls++
		},
	}

	payload, rpcErr := handlers.BackendStatus(&params)
	if rpcErr != nil {
		t.Fatalf("backend.status failed: %#v", rpcErr)
	}
	if refreshCompatibilityCalls != 0 {
		t.Fatalf("expected no compatibility refresh, got %d calls", refreshCompatibilityCalls)
	}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(data, &obj); err != nil {
		t.Fatal(err)
	}
	if _, ok := obj["compatibility"]; ok {
		t.Fatalf("expected compatibility to be omitted, got %s", data)
	}
	if _, ok := obj["desiredState"]; !ok {
		t.Fatalf("expected status payload fields, got %s", data)
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
