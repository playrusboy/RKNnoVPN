package control

import (
	"encoding/json"
	"testing"
	"time"

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

func TestProfilePatchClearActiveNodeNoopSkipsPersistMutation(t *testing.T) {
	cfg := profileHandlerTestConfig("", false)
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
	raw := json.RawMessage(`{"activeNodeId":null}`)

	result, rpcErr := handler.ProfilePatch(&raw)
	if rpcErr != nil {
		t.Fatalf("unexpected rpc error: %#v", rpcErr)
	}
	if persistCalled {
		t.Fatal("expected active-node patch noop to skip profile persistence/runtime apply")
	}
	obj, ok := result.(map[string]interface{})
	if !ok || obj["status"] != "noop" {
		t.Fatalf("unexpected noop result: %#v", result)
	}
}

func TestSubscriptionCommitPreviewUsesPreviewCache(t *testing.T) {
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

	previewResult, rpcErr := handler.SubscriptionPreview(&raw)
	if rpcErr != nil {
		t.Fatalf("preview failed: %#v", rpcErr)
	}
	preview, ok := previewResult.(subscription.PreviewResult)
	if !ok {
		t.Fatalf("unexpected preview result type: %T", previewResult)
	}
	if preview.PreviewID == "" {
		t.Fatal("preview did not return previewId")
	}
	commitRaw := json.RawMessage(`{"previewId":"` + preview.PreviewID + `"}`)
	if _, rpcErr := handler.SubscriptionCommitPreview(&commitRaw); rpcErr != nil {
		t.Fatalf("commitPreview failed: %#v", rpcErr)
	}
	if fetchCalls != 1 {
		t.Fatalf("preview + commitPreview should fetch once, got %d calls", fetchCalls)
	}
	if len(profiledoc.FromConfig(cfg).Nodes) != 1 {
		t.Fatalf("refresh did not persist preview nodes: %#v", profiledoc.FromConfig(cfg).Nodes)
	}
}

func TestSubscriptionCommitPreviewRejectsUnknownPreviewID(t *testing.T) {
	cfg := config.DefaultConfig()
	handler := ProfileHandlers{
		CurrentConfig: func() *config.Config {
			return cfg
		},
		PersistConfigMutation: func(next *config.Config, reload bool, action string) (applytx.ConfigTransactionResult, error) {
			t.Fatal("persist should not run for unknown previewId")
			return applytx.ConfigTransactionResult{}, nil
		},
		RuntimeStatus:        profileHandlerTestRuntimeStatus,
		SubscriptionPreviews: NewSubscriptionPreviewCache(),
		SubscriptionClient:   subscription.NewClient(subscription.FetcherFunc(nil)),
	}
	raw := json.RawMessage(`{"previewId":"missing"}`)

	if _, rpcErr := handler.SubscriptionCommitPreview(&raw); rpcErr == nil {
		t.Fatal("expected missing previewId to be rejected")
	}
}

func TestSubscriptionCommitPreviewRejectsExpiredPreviewID(t *testing.T) {
	cfg := config.DefaultConfig()
	now := time.Unix(100, 0)
	handler := ProfileHandlers{
		CurrentConfig: func() *config.Config {
			return cfg
		},
		PersistConfigMutation: func(next *config.Config, reload bool, action string) (applytx.ConfigTransactionResult, error) {
			t.Fatal("persist should not run for expired previewId")
			return applytx.ConfigTransactionResult{}, nil
		},
		RuntimeStatus: profileHandlerTestRuntimeStatus,
		SubscriptionClient: subscription.NewClient(subscription.FetcherFunc(func(rawURL string) (subscription.FetchResult, error) {
			return subscription.FetchResult{
				Status: 200,
				Body:   "vless://00000000-0000-0000-0000-000000000000@example.com:443#node-a",
			}, nil
		})),
		SubscriptionPreviews: NewSubscriptionPreviewCache(),
		Now: func() time.Time {
			return now
		},
	}
	raw := json.RawMessage(`{"url":"https://example.com/sub"}`)
	previewResult, rpcErr := handler.SubscriptionPreview(&raw)
	if rpcErr != nil {
		t.Fatalf("preview failed: %#v", rpcErr)
	}
	preview := previewResult.(subscription.PreviewResult)
	now = now.Add(subscriptionPreviewTTL + time.Second)
	commitRaw := json.RawMessage(`{"previewId":"` + preview.PreviewID + `"}`)

	if _, rpcErr := handler.SubscriptionCommitPreview(&commitRaw); rpcErr == nil {
		t.Fatal("expected expired previewId to be rejected")
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
