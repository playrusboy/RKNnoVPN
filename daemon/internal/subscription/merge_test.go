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

func TestMergeSubscriptionNodesPreservesManualAndMarksRemovedStale(t *testing.T) {
	current := profiledoc.Document{
		ID:   "main",
		Name: "Primary",
		Nodes: []profiledoc.Node{
			{ID: "manual", Name: "Manual", Protocol: "vless", Server: "shared.example", Port: 443, Outbound: json.RawMessage(`{"id":"manual"}`), Source: profiledoc.NodeSource{Type: "MANUAL"}},
			{ID: "old", Name: "Old", Protocol: "vless", Server: "old.example", Port: 443, Outbound: json.RawMessage(`{"id":"old"}`), Source: profiledoc.NodeSource{Type: "SUBSCRIPTION", ProviderKey: "sub"}},
			{ID: "same", Name: "Same", Protocol: "vless", Server: "same.example", Port: 443, Outbound: json.RawMessage(`{"id":"old-outbound"}`), Source: profiledoc.NodeSource{Type: "SUBSCRIPTION", ProviderKey: "sub"}},
		},
	}
	incoming := []profiledoc.Node{
		{ID: "new", Name: "New", Protocol: "vless", Server: "new.example", Port: 443, Outbound: json.RawMessage(`{"id":"new"}`), Source: profiledoc.NodeSource{Type: "SUBSCRIPTION", ProviderKey: "wrong"}},
		{ID: "same", Name: "Same updated", Protocol: "vless", Server: "same.example", Port: 443, Outbound: json.RawMessage(`{"id":"new-outbound"}`), Source: profiledoc.NodeSource{Type: "SUBSCRIPTION", ProviderKey: "sub"}},
		{ID: "manual-lookalike", Name: "Subscription twin", Protocol: "vless", Server: "shared.example", Port: 443, Outbound: json.RawMessage(`{"id":"manual"}`), Source: profiledoc.NodeSource{Type: "SUBSCRIPTION", ProviderKey: "sub"}},
	}
	next, stats := MergeSubscriptionNodes(current, profiledoc.Subscription{
		ProviderKey: "sub",
		URL:         "https://sub.example/list",
	}, incoming)
	if len(next.Nodes) != 5 {
		t.Fatalf("unexpected node count after merge: %#v", next.Nodes)
	}
	if next.Nodes[0].ID != "manual" || next.Nodes[0].Stale {
		t.Fatalf("manual node was not preserved: %#v", next.Nodes[0])
	}
	if !next.Nodes[1].Stale {
		t.Fatalf("removed subscription node was not marked stale: %#v", next.Nodes[1])
	}
	if next.Nodes[2].ID != "same" || string(next.Nodes[2].Outbound) != `{"id":"new-outbound"}` {
		t.Fatalf("subscription node was not updated in place: %#v", next.Nodes[2])
	}
	if next.Nodes[3].Source.ProviderKey != "sub" || next.Nodes[3].Source.URL != "https://sub.example/list" {
		t.Fatalf("incoming subscription source was not normalized: %#v", next.Nodes[3].Source)
	}
	if stats["added"] != 2 || stats["updated"] != 1 || stats["stale"] != 1 {
		t.Fatalf("unexpected merge stats: %#v", stats)
	}
}
