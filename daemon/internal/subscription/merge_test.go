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
	oldNode := nodeByIDForTest(next.Nodes, "old")
	if oldNode == nil || !oldNode.Stale {
		t.Fatalf("removed subscription node was not marked stale: %#v", next.Nodes)
	}
	sameNode := nodeByIDForTest(next.Nodes, "same")
	if sameNode == nil || string(sameNode.Outbound) != `{"id":"new-outbound"}` {
		t.Fatalf("subscription node was not updated in place: %#v", next.Nodes)
	}
	newNode := nodeByIDForTest(next.Nodes, "new")
	if newNode == nil || newNode.Source.ProviderKey != "sub" || newNode.Source.URL != "https://sub.example/list" {
		t.Fatalf("incoming subscription source was not normalized: %#v", next.Nodes)
	}
	if stats["added"] != 2 || stats["updated"] != 1 || stats["stale"] != 1 {
		t.Fatalf("unexpected merge stats: %#v", stats)
	}
}

func TestMergeSubscriptionNodesUsesIncomingProviderOrder(t *testing.T) {
	current := profiledoc.Document{
		ID:   "main",
		Name: "Primary",
		Nodes: []profiledoc.Node{
			{ID: "manual", Name: "Manual", Protocol: "vless", Server: "manual.example", Port: 443, Outbound: json.RawMessage(`{}`), Source: profiledoc.NodeSource{Type: "MANUAL"}},
			{ID: "old-a", Name: "Old A", Protocol: "vless", Server: "a.example", Port: 443, Outbound: json.RawMessage(`{"id":"old-a"}`), Source: profiledoc.NodeSource{Type: "SUBSCRIPTION", ProviderKey: "sub"}},
			{ID: "old-b", Name: "Old B", Protocol: "vless", Server: "b.example", Port: 443, Outbound: json.RawMessage(`{"id":"old-b"}`), Source: profiledoc.NodeSource{Type: "SUBSCRIPTION", ProviderKey: "sub"}},
			{ID: "other", Name: "Other", Protocol: "vless", Server: "other.example", Port: 443, Outbound: json.RawMessage(`{}`), Source: profiledoc.NodeSource{Type: "SUBSCRIPTION", ProviderKey: "other"}},
		},
	}
	incoming := []profiledoc.Node{
		{ID: "old-b", Name: "Provider B", Protocol: "vless", Server: "b.example", Port: 443, Outbound: json.RawMessage(`{"id":"new-b"}`), Source: profiledoc.NodeSource{Type: "SUBSCRIPTION", ProviderKey: "sub"}},
		{ID: "new-c", Name: "Provider C", Protocol: "vless", Server: "c.example", Port: 443, Outbound: json.RawMessage(`{"id":"new-c"}`), Source: profiledoc.NodeSource{Type: "SUBSCRIPTION", ProviderKey: "sub"}},
		{ID: "old-a", Name: "Provider A", Protocol: "vless", Server: "a.example", Port: 443, Outbound: json.RawMessage(`{"id":"new-a"}`), Source: profiledoc.NodeSource{Type: "SUBSCRIPTION", ProviderKey: "sub"}},
	}

	next, stats := MergeSubscriptionNodes(current, profiledoc.Subscription{
		ProviderKey: "sub",
		URL:         "https://sub.example/list",
	}, incoming)

	gotIDs := []string{}
	for _, node := range next.Nodes {
		gotIDs = append(gotIDs, node.ID)
	}
	wantIDs := []string{"manual", "old-b", "new-c", "old-a", "other"}
	if len(gotIDs) != len(wantIDs) {
		t.Fatalf("unexpected nodes: got %v want %v", gotIDs, wantIDs)
	}
	for i := range wantIDs {
		if gotIDs[i] != wantIDs[i] {
			t.Fatalf("provider order mismatch: got %v want %v", gotIDs, wantIDs)
		}
	}
	if stats["added"] != 1 || stats["updated"] != 2 || stats["stale"] != 0 {
		t.Fatalf("unexpected merge stats: %#v", stats)
	}
}

func nodeByIDForTest(nodes []profiledoc.Node, id string) *profiledoc.Node {
	for i := range nodes {
		if nodes[i].ID == id {
			return &nodes[i]
		}
	}
	return nil
}
