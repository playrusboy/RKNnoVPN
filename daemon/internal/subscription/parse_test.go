package subscription

import (
	"strings"
	"testing"
)

func TestParseSubscriptionRejectsLocalPrivateAndReservedEndpoints(t *testing.T) {
	body := strings.Join([]string{
		"vless://00000000-0000-0000-0000-000000000000@example.com:443#public",
		"vless://00000000-0000-0000-0000-000000000000@127.0.0.1:10808#loopback",
		"vless://00000000-0000-0000-0000-000000000000@[::ffff:127.0.0.1]:10808#mapped-loopback",
		"trojan://secret@192.168.1.10:443#private",
		"ss://secret@100.64.0.1:8388#cgnat",
		"vless://00000000-0000-0000-0000-000000000000@proxy.local:443#local-domain",
		"vless://00000000-0000-0000-0000-000000000000@router.home.arpa:443#home-arpa",
	}, "\n")

	source := SubscriptionSource{ProviderKey: "https://sub.example/list", URL: "https://sub.example/list"}
	nodes, sub, failures, rejected := ParseSubscription(body, nil, source, 123)
	if failures != 0 {
		t.Fatalf("unexpected parse failures: %d", failures)
	}
	if len(nodes) != 1 || nodes[0].Server != "example.com" {
		t.Fatalf("expected only public node to survive, got %#v", nodes)
	}
	if sub.LastSeenNodeCount != 1 {
		t.Fatalf("subscription node count should only include accepted nodes: %#v", sub)
	}
	if len(rejected) != 6 {
		t.Fatalf("expected six rejected local/private nodes, got %#v", rejected)
	}
	for _, item := range rejected {
		if item.Code != "subscription_local_endpoint" || item.Server == "" || item.Port == 0 {
			t.Fatalf("rejection should carry stable code and endpoint metadata: %#v", item)
		}
	}
}
