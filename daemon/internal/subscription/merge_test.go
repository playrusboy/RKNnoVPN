package subscription

import (
	"encoding/json"
	"testing"

	profiledoc "github.com/youtubediscord/RKNnoVPN/daemon/internal/profile"
)

func TestMergeSubscriptionNodesCanMarkProviderEmptyRefreshStale(t *testing.T) {
	current := profiledoc.Document{
		ID:           "main",
		Name:         "Primary",
		ActiveNodeID: "sub-live",
		Nodes: []profiledoc.Node{
			{ID: "manual", Name: "Manual", Protocol: "vless", Server: "manual.example", Port: 443, Outbound: json.RawMessage(`{}`), Source: profiledoc.NodeSource{Type: "MANUAL"}},
			{ID: "sub-live", Name: "Subscription", Protocol: "vless", Server: "sub.example", Port: 443, Outbound: json.RawMessage(`{}`), Source: profiledoc.NodeSource{Type: "SUBSCRIPTION", ProviderKey: "https://sub.example/list"}},
			{ID: "other-provider", Name: "Other", Protocol: "vless", Server: "other.example", Port: 443, Outbound: json.RawMessage(`{}`), Source: profiledoc.NodeSource{Type: "SUBSCRIPTION", ProviderKey: "https://other.example/list"}},
		},
	}

	next, stats := MergeSubscriptionNodes(current, profiledoc.Subscription{
		ProviderKey: "https://sub.example/list",
		URL:         "https://sub.example/list",
	}, nil)

	if stats["stale"] != 1 {
		t.Fatalf("expected one stale node, got stats %#v", stats)
	}
	if !next.Nodes[1].Stale {
		t.Fatalf("provider node was not marked stale: %#v", next.Nodes[1])
	}
	if next.Nodes[0].Stale || next.Nodes[2].Stale {
		t.Fatalf("empty refresh touched nodes outside provider scope: %#v", next.Nodes)
	}
	if next.ActiveNodeID != "manual" {
		t.Fatalf("active stale node was not repaired to live manual node: %q", next.ActiveNodeID)
	}
}
