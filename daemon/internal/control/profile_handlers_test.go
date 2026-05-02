package control

import (
	"encoding/json"
	"testing"

	applytx "github.com/youtubediscord/RKNnoVPN/daemon/internal/apply"
	"github.com/youtubediscord/RKNnoVPN/daemon/internal/config"
	profiledoc "github.com/youtubediscord/RKNnoVPN/daemon/internal/profile"
	"github.com/youtubediscord/RKNnoVPN/daemon/internal/runtimev2"
	"github.com/youtubediscord/RKNnoVPN/daemon/internal/subscription"
)

func TestProfileSetActiveNodeNoopSkipsPersistMutation(t *testing.T) {
	cfg := profileHandlerTestConfig("node-a", false)
	persistCalled := false
	handler := ProfileHandlers{
		CurrentConfig: func() *config.Config {
			return cfg
		},
		PersistConfigMutation: func(*config.Config, bool, string) (applytx.ConfigTransactionResult, error) {
			persistCalled = true
			return applytx.ConfigTransactionResult{}, nil
		},
		RuntimeStatus: profileHandlerTestRuntimeStatus,
	}
	raw := json.RawMessage(`{"nodeId":"node-a"}`)

	result, rpcErr := handler.ProfileSetActiveNode(&raw)
	if rpcErr != nil {
		t.Fatalf("unexpected rpc error: %#v", rpcErr)
	}
	if persistCalled {
		t.Fatal("expected active-node noop to skip profile persistence/runtime apply")
	}
	obj, ok := result.(map[string]interface{})
	if !ok {
		t.Fatalf("unexpected result type: %T", result)
	}
	if obj["status"] != "noop" || obj["config_saved"] != false || obj["runtime_applied"] != false {
		t.Fatalf("unexpected noop result: %#v", obj)
	}
	profile, ok := obj["profile"].(profiledoc.Document)
	if !ok || profile.ActiveNodeID != "node-a" {
		t.Fatalf("unexpected profile in result: %#v", obj["profile"])
	}
}

func TestProfileSetActiveNodeNoopStillRejectsStaleActiveNode(t *testing.T) {
	cfg := profileHandlerTestConfig("node-a", true)
	handler := ProfileHandlers{
		CurrentConfig: func() *config.Config {
			return cfg
		},
		PersistConfigMutation: func(*config.Config, bool, string) (applytx.ConfigTransactionResult, error) {
			t.Fatal("persist should not run for invalid active-node request")
			return applytx.ConfigTransactionResult{}, nil
		},
		RuntimeStatus: profileHandlerTestRuntimeStatus,
	}
	raw := json.RawMessage(`{"nodeId":"node-a"}`)

	if _, rpcErr := handler.ProfileSetActiveNode(&raw); rpcErr == nil {
		t.Fatal("expected stale active node to be rejected")
	}
}

func TestSubscriptionRefreshUsesPreviewCache(t *testing.T) {
	cfg := config.DefaultConfig()
	fetchCalls := 0
	handler := ProfileHandlers{
		CurrentConfig: func() *config.Config {
			return cfg
		},
		PersistConfigMutation: func(next *config.Config, reload bool, action string) (applytx.ConfigTransactionResult, error) {
			cfg = next
			return applytx.ConfigTransactionResult{
				Action:            action,
				ConfigSaved:       true,
				RuntimeWasRunning: false,
				RuntimeOperation:  runtimev2.OperationProfileApply,
			}, nil
		},
		RuntimeStatus: profileHandlerTestRuntimeStatus,
		SubscriptionClient: subscription.NewClient(subscription.FetcherFunc(func(rawURL string) (subscription.FetchResult, error) {
			fetchCalls++
			return subscription.FetchResult{
				Status: 200,
				Body:   "vless://00000000-0000-0000-0000-000000000000@example.com:443#node-a",
			}, nil
		})),
		SubscriptionPreviews: NewSubscriptionPreviewCache(),
	}
	raw := json.RawMessage(`{"url":"https://example.com/sub"}`)

	if _, rpcErr := handler.SubscriptionPreview(&raw); rpcErr != nil {
		t.Fatalf("preview failed: %#v", rpcErr)
	}
	if _, rpcErr := handler.SubscriptionRefresh(&raw); rpcErr != nil {
		t.Fatalf("refresh failed: %#v", rpcErr)
	}
	if fetchCalls != 1 {
		t.Fatalf("preview + refresh should fetch once, got %d calls", fetchCalls)
	}
	if len(profiledoc.FromConfig(cfg).Nodes) != 1 {
		t.Fatalf("refresh did not persist preview nodes: %#v", profiledoc.FromConfig(cfg).Nodes)
	}
}

func profileHandlerTestConfig(activeNodeID string, stale bool) *config.Config {
	cfg := config.DefaultConfig()
	cfg.Profile.ID = "default"
	cfg.Profile.Name = "Default"
	cfg.Profile.ActiveNodeID = activeNodeID
	staleJSON := "false"
	if stale {
		staleJSON = "true"
	}
	cfg.Profile.Nodes = []json.RawMessage{json.RawMessage(`{
		"id":"node-a",
		"name":"Node A",
		"protocol":"socks",
		"server":"127.0.0.1",
		"port":1080,
		"stale":` + staleJSON + `,
		"source":{"type":"MANUAL"}
	}`)}
	return cfg
}

func profileHandlerTestRuntimeStatus() (runtimev2.Status, bool) {
	return runtimev2.Status{
		AppliedState: runtimev2.AppliedState{
			BackendKind:     runtimev2.BackendRootTProxy,
			Phase:           runtimev2.PhaseHealthy,
			ActiveProfileID: "node-a",
			Generation:      42,
		},
	}, true
}
