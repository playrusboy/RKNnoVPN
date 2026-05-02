package profile

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/youtubediscord/RKNnoVPN/daemon/internal/config"
)

func TestProfileDocumentProjectsConfigProfileWithoutDroppingState(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Profile.ID = "main"
	cfg.Profile.Name = "Primary"
	cfg.Profile.ActiveNodeID = "node-a"
	cfg.Profile.Nodes = []json.RawMessage{
		json.RawMessage(`{"id":"node-a","name":"A","protocol":"VLESS","server":"example.com","port":443,"outbound":{"protocol":"vless","settings":{"vnext":[{"address":"example.com","port":443,"users":[{"id":"uuid-a"}]}]}},"source":{"type":"MANUAL"}}`),
		json.RawMessage(`{"id":"node-old","name":"Old","protocol":"TROJAN","server":"old.example","port":443,"stale":true,"outbound":{"protocol":"trojan","settings":{"servers":[{"address":"old.example","port":443,"password":"p"}]}},"source":{"type":"SUBSCRIPTION","url":"https://sub.example/list","providerKey":"https://sub.example/list","lastSeenAt":1}}`),
	}
	cfg.Profile.Subscriptions = []json.RawMessage{
		json.RawMessage(`{"providerKey":"https://sub.example/list","url":"https://sub.example/list","lastFetchedAt":1,"lastSeenNodeCount":1,"staleNodeCount":1}`),
	}
	cfg.Profile.Inbounds = json.RawMessage(`{"socksPort":10808,"httpPort":10809,"allowLan":false}`)

	doc := FromConfig(cfg)
	if doc.SchemaVersion != CurrentSchemaVersion {
		t.Fatalf("profile schema version not exposed: got %d want %d", doc.SchemaVersion, CurrentSchemaVersion)
	}
	if doc.ID != "main" || doc.Name != "Primary" || doc.ActiveNodeID != "node-a" {
		t.Fatalf("profile identity not projected: %#v", doc)
	}
	if len(doc.Nodes) != 2 || len(doc.Subscriptions) != 1 {
		t.Fatalf("profile collections not projected: nodes=%d subscriptions=%d", len(doc.Nodes), len(doc.Subscriptions))
	}
	if !doc.Nodes[1].Stale || doc.Nodes[1].Source.Type != "SUBSCRIPTION" {
		t.Fatalf("stale subscription metadata was not preserved: %#v", doc.Nodes[1])
	}
	if doc.Inbounds.SocksPort != 10808 || doc.Inbounds.HTTPPort != 10809 || doc.Inbounds.AllowLAN {
		t.Fatalf("inbounds not projected safely: %#v", doc.Inbounds)
	}
}

func TestFromConfigDoesNotCreateNodeFromLegacyConfigOnlyState(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Node.Address = "legacy.example"
	cfg.Node.Port = 443
	cfg.Node.Protocol = "vless"
	cfg.Node.UUID = "00000000-0000-0000-0000-000000000000"
	cfg.Profile.Nodes = nil
	cfg.Profile.ActiveNodeID = ""

	doc := FromConfig(cfg)
	if len(doc.Nodes) != 0 || doc.ActiveNodeID != "" {
		t.Fatalf("legacy config-only node must not be projected into profile: %#v", doc)
	}
}

func TestApplyToConfigEnablesLocalClashAPIForMultiNodeProfile(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Profile.ActiveNodeID = "node-a"
	cfg.Profile.Nodes = []json.RawMessage{
		json.RawMessage(`{"id":"node-a","name":"A","protocol":"SOCKS","server":"127.0.0.1","port":1080,"outbound":{"protocol":"socks","settings":{"address":"127.0.0.1","port":1080}},"source":{"type":"MANUAL"}}`),
		json.RawMessage(`{"id":"node-b","name":"B","protocol":"SOCKS","server":"127.0.0.2","port":1081,"outbound":{"protocol":"socks","settings":{"address":"127.0.0.2","port":1081}},"source":{"type":"MANUAL"}}`),
	}
	doc := FromConfig(cfg)

	next, _, err := ApplyToConfig(cfg, doc)
	if err != nil {
		t.Fatal(err)
	}
	if next.Proxy.APIPort != config.DefaultClashAPIPort || next.Proxy.APISecret == "" {
		t.Fatalf("multi-node profile should enable local clash API with secret: %#v", next.Proxy)
	}
}

func TestApplyToConfigGeneratesClashAPISecretForExistingPort(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Proxy.APIPort = 19191
	cfg.Proxy.APISecret = ""
	cfg.Profile.ActiveNodeID = "node-a"
	cfg.Profile.Nodes = []json.RawMessage{
		json.RawMessage(`{"id":"node-a","name":"A","protocol":"SOCKS","server":"127.0.0.1","port":1080,"outbound":{"protocol":"socks","settings":{"address":"127.0.0.1","port":1080}},"source":{"type":"MANUAL"}}`),
		json.RawMessage(`{"id":"node-b","name":"B","protocol":"SOCKS","server":"127.0.0.2","port":1081,"outbound":{"protocol":"socks","settings":{"address":"127.0.0.2","port":1081}},"source":{"type":"MANUAL"}}`),
	}
	doc := FromConfig(cfg)

	next, _, err := ApplyToConfig(cfg, doc)
	if err != nil {
		t.Fatal(err)
	}
	if next.Proxy.APIPort != 19191 || next.Proxy.APISecret == "" {
		t.Fatalf("multi-node profile should preserve clash API port and generate secret: %#v", next.Proxy)
	}
}

func TestProfileRoutingProjectsRussiaAndSeparateRuleIPs(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Routing.BypassRussia = true
	cfg.Routing.CustomDirect = []string{"market.yandex.ru", "203.0.113.0/24"}
	cfg.Routing.CustomProxy = []string{"proxy.example", "198.51.100.0/24"}
	cfg.Routing.CustomBlock = []string{"ads.example", "192.0.2.0/24"}

	doc := FromConfig(cfg)
	if !doc.Routing.BypassRussia {
		t.Fatalf("bypassRussia was not projected: %#v", doc.Routing)
	}
	for _, tt := range []struct {
		name   string
		values []string
		want   string
	}{
		{"direct domain", doc.Routing.DirectDomains, "market.yandex.ru"},
		{"direct ip", doc.Routing.DirectIps, "203.0.113.0/24"},
		{"proxy domain", doc.Routing.ProxyDomains, "proxy.example"},
		{"proxy ip", doc.Routing.ProxyIps, "198.51.100.0/24"},
		{"block domain", doc.Routing.BlockDomains, "ads.example"},
		{"block ip", doc.Routing.BlockIps, "192.0.2.0/24"},
	} {
		if !containsString(tt.values, tt.want) {
			t.Fatalf("%s missing %q in %#v", tt.name, tt.want, tt.values)
		}
	}

	doc.Routing.BypassRussia = false
	doc.Routing.DirectDomains = []string{"ya.ru"}
	doc.Routing.DirectIps = []string{"5.255.255.0/24"}
	next, _, err := ApplyToConfig(cfg, doc)
	if err != nil {
		t.Fatal(err)
	}
	if next.Routing.BypassRussia {
		t.Fatalf("bypassRussia was not applied: %#v", next.Routing)
	}
	if !containsString(next.Routing.CustomDirect, "ya.ru") || !containsString(next.Routing.CustomDirect, "5.255.255.0/24") {
		t.Fatalf("direct domains and IPs were not merged into custom_direct: %#v", next.Routing.CustomDirect)
	}
}

func TestProfileRulesModePreservesForcedProxyApps(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Routing.Mode = "rules"
	cfg.Apps.Mode = "all"
	cfg.Apps.Packages = []string{"org.telegram.messenger", "com.discord"}

	doc := FromConfig(cfg)
	if doc.Routing.Mode != "RULES" {
		t.Fatalf("expected RULES mode, got %#v", doc.Routing.Mode)
	}
	for _, want := range []string{"org.telegram.messenger", "com.discord"} {
		if !containsString(doc.Routing.AppProxyList, want) {
			t.Fatalf("forced proxy app %q missing from profile: %#v", want, doc.Routing.AppProxyList)
		}
	}

	doc.Routing.AppProxyList = []string{"com.discord"}
	next, _, err := ApplyToConfig(cfg, doc)
	if err != nil {
		t.Fatal(err)
	}
	if next.Routing.Mode != "rules" || next.Apps.Mode != "all" {
		t.Fatalf("rules process routing changed modes: routing=%q apps=%q", next.Routing.Mode, next.Apps.Mode)
	}
	if len(next.Apps.Packages) != 1 || next.Apps.Packages[0] != "com.discord" {
		t.Fatalf("forced proxy apps not applied to rules mode: %#v", next.Apps.Packages)
	}
}

func TestProfileRoutingPreservesInactiveAppSelectionsAcrossModeSwitch(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Routing.Mode = "whitelist"
	cfg.Apps.Mode = "whitelist"
	cfg.Apps.Packages = []string{"org.telegram.messenger", "com.discord"}

	doc := FromConfig(cfg)
	doc.Routing.Mode = "PROXY_ALL"
	next, _, err := ApplyToConfig(cfg, doc)
	if err != nil {
		t.Fatal(err)
	}
	if next.Routing.Mode != "all" || next.Apps.Mode != "all" {
		t.Fatalf("routing mode was not switched to all: routing=%q apps=%q", next.Routing.Mode, next.Apps.Mode)
	}
	if len(next.Apps.Packages) != 0 {
		t.Fatalf("global mode must not keep active app packages: %#v", next.Apps.Packages)
	}
	if !containsString(next.Routing.InactiveAppProxyList, "org.telegram.messenger") ||
		!containsString(next.Routing.InactiveAppProxyList, "com.discord") {
		t.Fatalf("inactive proxy app selection was not preserved: %#v", next.Routing.InactiveAppProxyList)
	}

	roundTrip := FromConfig(next)
	if !containsString(roundTrip.Routing.AppProxyList, "org.telegram.messenger") ||
		!containsString(roundTrip.Routing.AppProxyList, "com.discord") {
		t.Fatalf("profile projection dropped inactive proxy apps: %#v", roundTrip.Routing.AppProxyList)
	}

	roundTrip.Routing.Mode = "PER_APP"
	restored, _, err := ApplyToConfig(next, roundTrip)
	if err != nil {
		t.Fatal(err)
	}
	if restored.Routing.Mode != "whitelist" || restored.Apps.Mode != "whitelist" {
		t.Fatalf("routing mode was not restored to whitelist: routing=%q apps=%q", restored.Routing.Mode, restored.Apps.Mode)
	}
	if !containsString(restored.Apps.Packages, "org.telegram.messenger") ||
		!containsString(restored.Apps.Packages, "com.discord") {
		t.Fatalf("proxy app selection was not restored: %#v", restored.Apps.Packages)
	}
}

func TestDecodeStrictDocumentRejectsUnknownProfileFields(t *testing.T) {
	raw := []byte(`{
		"profileSchemaVersion": 2,
		"id": "main",
		"name": "Primary",
		"nodes": [],
		"runtime": {},
		"routing": {"mode":"PER_APP"},
		"dns": {"remoteDns":"https://1.1.1.1/dns-query","directDns":"https://dns.google/dns-query","bootstrapIp":"1.1.1.1","ipv6Mode":"MIRROR","fakeDns":false},
		"health": {"enabled":true,"intervalSec":30,"threshold":3,"checkUrl":"https://www.gstatic.com/generate_204","timeoutSec":5,"dnsIsHardReadiness":false},
		"sharing": {"enabled":false},
		"tun": {"enabled":false,"mtu":9000,"ipv4Address":"172.19.0.1/30","ipv6":false,"autoRoute":true,"strictRoute":true},
		"inbounds": {"socksPort":0,"httpPort":0,"allowLan":false},
		"unexpected": true
	}`)

	if _, err := DecodeStrictDocument(raw); err == nil {
		t.Fatalf("unknown profile field should be rejected")
	}
}

func TestDecodeStrictDocumentRejectsRemovedBlockQuicField(t *testing.T) {
	raw := []byte(`{
		"profileSchemaVersion": 2,
		"id": "main",
		"name": "Primary",
		"nodes": [],
		"runtime": {},
		"routing": {"mode":"PER_APP"},
		"dns": {"remoteDns":"https://1.1.1.1/dns-query","directDns":"https://dns.google/dns-query","bootstrapIp":"1.1.1.1","ipv6Mode":"MIRROR","blockQuic":true,"fakeDns":false},
		"health": {"enabled":true,"intervalSec":30,"threshold":3,"checkUrl":"https://www.gstatic.com/generate_204","timeoutSec":5,"dnsIsHardReadiness":false},
		"sharing": {"enabled":false},
		"tun": {"enabled":false,"mtu":9000,"ipv4Address":"172.19.0.1/30","ipv6":false,"autoRoute":true,"strictRoute":true},
		"inbounds": {"socksPort":0,"httpPort":0,"allowLan":false}
	}`)

	if _, err := DecodeStrictDocument(raw); err == nil || !strings.Contains(err.Error(), "blockQuic") {
		t.Fatalf("removed blockQuic field should be rejected, got %v", err)
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func TestDecodeStrictDocumentRejectsUnknownNodeFieldsButAllowsOutboundAndExtra(t *testing.T) {
	raw := []byte(`{
		"profileSchemaVersion": 2,
		"id": "main",
		"name": "Primary",
		"activeNodeId": "node-1",
		"nodes": [{
			"id":"node-1",
			"name":"Node",
			"protocol":"vless",
			"server":"example.com",
			"port":443,
			"outbound":{"protocol":"vless","settings":{"x":1}},
			"source":{"type":"MANUAL"},
			"extra":{"ui":"ok"},
			"legacyField":"nope"
		}],
		"runtime": {},
		"routing": {"mode":"PER_APP"},
		"dns": {"remoteDns":"https://1.1.1.1/dns-query","directDns":"https://dns.google/dns-query","bootstrapIp":"1.1.1.1","ipv6Mode":"MIRROR","fakeDns":false},
		"health": {"enabled":true,"intervalSec":30,"threshold":3,"checkUrl":"https://www.gstatic.com/generate_204","timeoutSec":5,"dnsIsHardReadiness":false},
		"sharing": {"enabled":false},
		"tun": {"enabled":false,"mtu":9000,"ipv4Address":"172.19.0.1/30","ipv6":false,"autoRoute":true,"strictRoute":true},
		"inbounds": {"socksPort":0,"httpPort":0,"allowLan":false}
	}`)

	if _, err := DecodeStrictDocument(raw); err == nil {
		t.Fatalf("unknown node field should be rejected")
	}
}

func TestProfileValidationRejectsAllowLanAndRepairsStaleActive(t *testing.T) {
	doc := Document{
		ID:           "main",
		Name:         "Primary",
		ActiveNodeID: "stale",
		Inbounds:     InboundsConfig{AllowLAN: true},
		Subscriptions: []Subscription{
			{ProviderKey: "sub", URL: "https://sub.example/list"},
		},
		Nodes: []Node{
			{ID: "stale", Name: "Stale", Protocol: "vless", Server: "old.example", Port: 443, Stale: true, Outbound: json.RawMessage(`{}`), Source: NodeSource{Type: "SUBSCRIPTION", ProviderKey: "sub"}},
			{ID: "live", Name: "Live", Protocol: "vless", Server: "live.example", Port: 443, Outbound: json.RawMessage(`{}`), Source: NodeSource{Type: "MANUAL"}},
		},
	}
	if _, _, err := Normalize(doc); err == nil {
		t.Fatal("expected allowLan validation error")
	}
	doc.Inbounds.AllowLAN = false
	normalized, warnings, err := Normalize(doc)
	if err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}
	if normalized.ActiveNodeID != "live" {
		t.Fatalf("active stale node was not repaired: %#v", normalized.ActiveNodeID)
	}
	if len(warnings) == 0 || warnings[0].Code != "active_node_repaired" {
		t.Fatalf("active repair warning missing: %#v", warnings)
	}
}

func TestMergeNodesSelectsImportedManualNode(t *testing.T) {
	current := Document{
		ID:           "main",
		Name:         "Primary",
		ActiveNodeID: "old",
		Nodes: []Node{
			{
				ID:       "old",
				Name:     "Old",
				Protocol: "vless",
				Server:   "old.example",
				Port:     443,
				Outbound: json.RawMessage(`{"protocol":"vless","settings":{"vnext":[{"address":"old.example","port":443}]}}`),
				Source:   NodeSource{Type: "MANUAL"},
			},
		},
	}
	incoming := []Node{
		{
			ID:       "new",
			Name:     "New",
			Protocol: "vless",
			Server:   "new.example",
			Port:     443,
			Outbound: json.RawMessage(`{"protocol":"vless","settings":{"vnext":[{"address":"new.example","port":443}]}}`),
			Source:   NodeSource{Type: "MANUAL"},
		},
	}

	next, stats := MergeNodes(current, incoming)
	if stats["added"] != 1 {
		t.Fatalf("expected one added node, got %#v", stats)
	}
	if next.ActiveNodeID != "new" {
		t.Fatalf("imported node was not selected: %#v", next.ActiveNodeID)
	}
}

func TestNormalizeSubscriptionsRecomputesProviderCounts(t *testing.T) {
	doc := Document{
		ID: "main",
		Subscriptions: []Subscription{
			{ProviderKey: "sub", URL: "https://sub.example/list", LastSeenNodeCount: 99, StaleNodeCount: 99},
		},
		Nodes: []Node{
			{ID: "live", Name: "Live", Protocol: "vless", Server: "live.example", Port: 443, Outbound: json.RawMessage(`{}`), Source: NodeSource{Type: "SUBSCRIPTION", ProviderKey: "sub", URL: "https://sub.example/list"}},
			{ID: "stale", Name: "Stale", Protocol: "vless", Server: "old.example", Port: 443, Stale: true, Outbound: json.RawMessage(`{}`), Source: NodeSource{Type: "SUBSCRIPTION", ProviderKey: "sub", URL: "https://sub.example/list"}},
		},
	}

	normalized, _, err := Normalize(doc)
	if err != nil {
		t.Fatal(err)
	}
	if len(normalized.Subscriptions) != 1 {
		t.Fatalf("expected one subscription, got %#v", normalized.Subscriptions)
	}
	if normalized.Subscriptions[0].LastSeenNodeCount != 1 || normalized.Subscriptions[0].StaleNodeCount != 1 {
		t.Fatalf("subscription counts were not recomputed: %#v", normalized.Subscriptions[0])
	}
}

func TestNormalizeRejectsSubscriptionNodeWithLocalEndpoint(t *testing.T) {
	doc := Document{
		ID: "main",
		Subscriptions: []Subscription{
			{ProviderKey: "sub", URL: "https://sub.example/list"},
		},
		Nodes: []Node{
			{
				ID:       "local-sub",
				Name:     "Local subscription node",
				Protocol: "socks",
				Server:   "127.0.0.1",
				Port:     10808,
				Outbound: json.RawMessage(`{"protocol":"socks","settings":{"address":"127.0.0.1","port":10808}}`),
				Source:   NodeSource{Type: "SUBSCRIPTION", ProviderKey: "sub", URL: "https://sub.example/list"},
			},
		},
	}

	if _, _, err := Normalize(doc); err == nil || !strings.Contains(err.Error(), "must not be local") {
		t.Fatalf("expected local subscription endpoint rejection, got %v", err)
	}
}

func TestIsDisallowedSubscriptionEndpointCoversLocalNamesAndMappedIPs(t *testing.T) {
	tests := []struct {
		host string
		want bool
	}{
		{"example.com", false},
		{"localhost", true},
		{"localhost.", true},
		{"local", true},
		{"proxy.local", true},
		{"lan", true},
		{"router.lan", true},
		{"home.arpa", true},
		{"router.home.arpa.", true},
		{"127.0.0.1", true},
		{"::ffff:127.0.0.1", true},
		{"10.1.2.3", true},
		{"100.64.0.1", true},
		{"198.18.0.1", true},
		{"8.8.8.8", false},
	}
	for _, tt := range tests {
		if got := IsDisallowedSubscriptionEndpoint(tt.host); got != tt.want {
			t.Fatalf("IsDisallowedSubscriptionEndpoint(%q)=%v, want %v", tt.host, got, tt.want)
		}
	}
}
