package config

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRenderSingboxConfigAvoidsRemovedSingBox113Fields(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Node.Address = "example.com"
	cfg.Node.Port = 443
	cfg.Node.Protocol = "vless"
	cfg.Node.UUID = "00000000-0000-0000-0000-000000000000"
	cfg.Routing.BlockAds = true
	cfg.Routing.CustomBlock = []string{"ads.example"}

	var rendered map[string]any
	data, err := RenderSingboxConfig(cfg, cfg.ResolveProfile())
	if err != nil {
		t.Fatalf("render config: %v", err)
	}
	if err := json.Unmarshal(data, &rendered); err != nil {
		t.Fatalf("unmarshal config: %v", err)
	}

	for _, rawOutbound := range rendered["outbounds"].([]any) {
		outbound := rawOutbound.(map[string]any)
		switch outbound["type"] {
		case "dns", "block":
			t.Fatalf("removed sing-box 1.13 outbound rendered: %v", outbound["type"])
		}
		if outbound["tag"] == "proxy" && outbound["domain_resolver"] != "direct-dns" {
			t.Fatalf("proxy outbound with domain server must set domain_resolver: %#v", outbound)
		}
	}

	dns := rendered["dns"].(map[string]any)
	if _, ok := dns["independent_cache"]; ok {
		t.Fatal("deprecated independent_cache field rendered")
	}
	for _, rawServer := range dns["servers"].([]any) {
		server := rawServer.(map[string]any)
		if _, ok := server["type"]; !ok {
			t.Fatalf("legacy DNS server without type rendered: %#v", server)
		}
		if _, ok := server["address"]; ok {
			t.Fatalf("legacy DNS server address field rendered: %#v", server)
		}
		if _, ok := server["address_resolver"]; ok {
			t.Fatalf("legacy DNS server address_resolver field rendered: %#v", server)
		}
		if tag := server["tag"]; (tag == "direct-dns" || tag == "bootstrap-dns") && server["detour"] == "direct" {
			t.Fatalf("DNS server must not detour through empty direct outbound: %#v", server)
		}
	}

	var dnsInbound map[string]any
	for _, rawInbound := range rendered["inbounds"].([]any) {
		inbound := rawInbound.(map[string]any)
		if _, ok := inbound["sniff"]; ok {
			t.Fatalf("removed inbound sniff field rendered: %#v", inbound)
		}
		if _, ok := inbound["sniff_override_destination"]; ok {
			t.Fatalf("removed inbound sniff_override_destination field rendered: %#v", inbound)
		}
		if inbound["tag"] == "dns-in" {
			dnsInbound = inbound
		}
	}
	if dnsInbound == nil {
		t.Fatal("DNS direct inbound was not rendered")
	}
	if dnsInbound["type"] != "direct" {
		t.Fatalf("DNS inbound must use direct override listener, got %#v", dnsInbound)
	}
	if dnsInbound["override_address"] != "1.1.1.1" {
		t.Fatalf("DNS direct inbound must override destination away from itself: %#v", dnsInbound)
	}
	if dnsInbound["override_port"] != float64(53) {
		t.Fatalf("DNS direct inbound must override destination port 53: %#v", dnsInbound)
	}

	rules := rendered["route"].(map[string]any)["rules"].([]any)
	if rendered["route"].(map[string]any)["default_domain_resolver"] != "direct-dns" {
		t.Fatalf("route should define default_domain_resolver: %#v", rendered["route"])
	}
	if _, ok := rendered["route"].(map[string]any)["default_mark"]; ok {
		t.Fatalf("route must not mark sing-box's own outbound sockets on Android: %#v", rendered["route"])
	}
	if _, ok := rendered["route"].(map[string]any)["auto_detect_interface"]; ok {
		t.Fatalf("route should not require default interface during service start: %#v", rendered["route"])
	}
	if rules[0].(map[string]any)["action"] != "sniff" {
		t.Fatalf("first route rule should sniff tproxy traffic: %#v", rules[0])
	}
	if got := rules[0].(map[string]any)["inbound"].([]any); len(got) != 2 || got[0] != "tproxy-in" || got[1] != "dns-in" {
		t.Fatalf("sniff rule should cover tproxy and DNS redirect inbounds: %#v", rules[0])
	}
	if rules[1].(map[string]any)["action"] != "hijack-dns" {
		t.Fatalf("DNS route rule should use hijack-dns action: %#v", rules[1])
	}
	localRule := rules[2].(map[string]any)
	if localRule["action"] != "reject" {
		t.Fatalf("local TPROXY route should reject instead of looping: %#v", localRule)
	}
}

func TestRenderRouteUsesRuleActionsAndRemoteRuleSets(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Node.Address = "example.com"
	cfg.Node.Port = 443
	cfg.Node.Protocol = "vless"
	cfg.Node.UUID = "00000000-0000-0000-0000-000000000000"
	cfg.Routing.BypassRussia = true
	cfg.Routing.BypassChina = true
	cfg.Routing.BlockAds = true
	cfg.Routing.CustomDirect = []string{"direct.example", "203.0.113.0/24"}
	cfg.Routing.CustomProxy = []string{"proxy.example", "198.51.100.0/24"}
	cfg.Routing.AlwaysDirectApps = []string{"com.bank.app"}
	cfg.Apps.AppGroups = map[string]string{
		"com.chat.app": "Europe",
	}
	cfg.Profile.Nodes = []json.RawMessage{
		json.RawMessage(`{
			"id":"first-node",
			"name":"First",
			"group":"Europe",
			"protocol":"SOCKS",
			"server":"127.0.0.1",
			"port":1081,
			"source":{"type":"MANUAL"},
			"outbound":{"protocol":"socks","settings":{"address":"127.0.0.1","port":1081,"version":"5"}}
		}`),
	}

	var rendered map[string]any
	data, err := RenderSingboxConfig(cfg, cfg.ResolveProfile())
	if err != nil {
		t.Fatalf("render config: %v", err)
	}
	if err := json.Unmarshal(data, &rendered); err != nil {
		t.Fatalf("unmarshal config: %v", err)
	}

	route := rendered["route"].(map[string]any)
	routeRules := route["rules"].([]any)
	assertRouteRulesUseActions(t, routeRules)
	if !hasRouteRuleCIDR(routeRules, "84.201.128.0/18", "direct") {
		t.Fatalf("Yandex ASN prefixes should be routed directly with Russian bypass: %#v", routeRules)
	}
	if !hasRouteRuleCIDR(routeRules, "87.240.128.0/18", "direct") {
		t.Fatalf("VK prefixes should be routed directly with Russian bypass: %#v", routeRules)
	}

	experimental := rendered["experimental"].(map[string]any)
	cacheFile := experimental["cache_file"].(map[string]any)
	if cacheFile["enabled"] != true {
		t.Fatalf("remote rule sets require cache_file.enabled=true, got %#v", experimental)
	}
	dataWithPath, err := RenderSingboxConfigForDataDir(cfg, cfg.ResolveProfile(), "/data/adb/modules/rknnovpn")
	if err != nil {
		t.Fatalf("render config with data dir: %v", err)
	}
	var renderedWithPath map[string]any
	if err := json.Unmarshal(dataWithPath, &renderedWithPath); err != nil {
		t.Fatalf("unmarshal config with data dir: %v", err)
	}
	cacheFileWithPath := renderedWithPath["experimental"].(map[string]any)["cache_file"].(map[string]any)
	if cacheFileWithPath["path"] != "/data/adb/modules/rknnovpn/data/cache.db" {
		t.Fatalf("cache_file.path must be writable module data path, got %#v", cacheFileWithPath)
	}

	sets := route["rule_set"].([]any)
	byTag := map[string]map[string]any{}
	for _, rawSet := range sets {
		set := rawSet.(map[string]any)
		byTag[set["tag"].(string)] = set
	}
	for _, tag := range []string{"geoip-ru", "geoip-cn", "geosite-cn", "geosite-category-ads-all"} {
		set := byTag[tag]
		if set == nil {
			t.Fatalf("missing remote rule-set %s in %#v", tag, sets)
		}
		if set["type"] != "remote" || set["format"] != "binary" {
			t.Fatalf("rule-set %s must be remote binary: %#v", tag, set)
		}
		if !strings.HasSuffix(set["url"].(string), "/"+tag+".srs") {
			t.Fatalf("rule-set %s must point at its .srs artifact: %#v", tag, set)
		}
		if set["download_detour"] != "proxy" {
			t.Fatalf("rule-set %s should download through proxy: %#v", tag, set)
		}
	}
	if byTag["geosite-ru"] != nil {
		t.Fatalf("geosite-ru is not a canonical SagerNet rule-set and must not be rendered: %#v", sets)
	}
}

func TestRenderRoutesProxyEndpointsDirectly(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Node.Address = "fallback.example"
	cfg.Node.Port = 443
	cfg.Node.Protocol = "vless"
	cfg.Node.UUID = "00000000-0000-0000-0000-000000000000"
	cfg.Profile.Nodes = []json.RawMessage{
		json.RawMessage(`{
			"id":"yandex-cloud-node",
			"name":"Yandex Cloud node",
			"protocol":"VLESS",
			"server":"84.201.148.180",
			"port":443,
			"outbound":{
				"protocol":"vless",
				"settings":{"vnext":[{"address":"84.201.148.180","port":443,"users":[{"id":"11111111-1111-1111-1111-111111111111","encryption":"none"}]}]}
			}
		}`),
		json.RawMessage(`{
			"id":"domain-node",
			"name":"Domain node",
			"protocol":"Trojan",
			"server":"Proxy.Example",
			"port":443,
			"outbound":{
				"protocol":"trojan",
				"settings":{"servers":[{"address":"Proxy.Example","port":443,"password":"secret"}]}
			}
		}`),
		json.RawMessage(`{
			"id":"stale-node",
			"name":"Stale node",
			"protocol":"VLESS",
			"server":"stale.example",
			"port":443,
			"stale":true,
			"outbound":{
				"protocol":"vless",
				"settings":{"vnext":[{"address":"stale.example","port":443,"users":[{"id":"22222222-2222-2222-2222-222222222222","encryption":"none"}]}]}
			}
		}`),
	}

	var rendered map[string]any
	data, err := RenderSingboxConfig(cfg, cfg.ResolveProfile())
	if err != nil {
		t.Fatalf("render config: %v", err)
	}
	if err := json.Unmarshal(data, &rendered); err != nil {
		t.Fatalf("unmarshal config: %v", err)
	}

	routeRules := rendered["route"].(map[string]any)["rules"].([]any)
	if !hasRouteRuleCIDR(routeRules, "84.201.148.180/32", "direct") {
		t.Fatalf("proxy server IP must be routed directly before final proxy: %#v", routeRules)
	}
	if !hasRouteRuleDomain(routeRules, "proxy.example", "direct") {
		t.Fatalf("proxy server domain must be routed directly before final proxy: %#v", routeRules)
	}
	if hasRouteRuleDomain(routeRules, "stale.example", "direct") {
		t.Fatalf("stale proxy endpoint must not be rendered as a live direct rule: %#v", routeRules)
	}

	dnsRules := rendered["dns"].(map[string]any)["rules"].([]any)
	if !hasDNSRuleDomainServer(dnsRules, "proxy.example", "direct-dns") {
		t.Fatalf("proxy server domain must resolve through direct DNS: %#v", dnsRules)
	}
}

func hasRouteRuleCIDR(rules []any, cidr string, outbound string) bool {
	return hasRouteRuleCIDRWithOutbound(rules, cidr, outbound, "")
}

func hasRouteRuleCIDRWithOutbound(rules []any, cidr string, outbound string, inheritedOutbound string) bool {
	for _, rawRule := range rules {
		rule := rawRule.(map[string]any)
		effectiveOutbound := inheritedOutbound
		if value, ok := rule["outbound"].(string); ok && value != "" {
			effectiveOutbound = value
		}
		if effectiveOutbound == outbound {
			for _, rawCIDR := range ruleList(rule["ip_cidr"]) {
				if rawCIDR == cidr {
					return true
				}
			}
		}
		if nested, ok := rule["rules"].([]any); ok && hasRouteRuleCIDRWithOutbound(nested, cidr, outbound, effectiveOutbound) {
			return true
		}
	}
	return false
}

func hasRouteRuleDomain(rules []any, domain string, outbound string) bool {
	return hasRouteRuleDomainWithOutbound(rules, domain, outbound, "")
}

func hasRouteRuleDomainWithOutbound(rules []any, domain string, outbound string, inheritedOutbound string) bool {
	for _, rawRule := range rules {
		rule := rawRule.(map[string]any)
		effectiveOutbound := inheritedOutbound
		if value, ok := rule["outbound"].(string); ok && value != "" {
			effectiveOutbound = value
		}
		if effectiveOutbound == outbound {
			for _, rawDomain := range ruleList(rule["domain"]) {
				if rawDomain == domain {
					return true
				}
			}
		}
		if nested, ok := rule["rules"].([]any); ok && hasRouteRuleDomainWithOutbound(nested, domain, outbound, effectiveOutbound) {
			return true
		}
	}
	return false
}

func hasDNSRuleDomainServer(rules []any, domain string, server string) bool {
	for _, rawRule := range rules {
		rule := rawRule.(map[string]any)
		if rule["server"] != server {
			continue
		}
		for _, rawDomain := range ruleList(rule["domain"]) {
			if rawDomain == domain {
				return true
			}
		}
	}
	return false
}

func ruleList(value any) []string {
	rawValues, ok := value.([]any)
	if !ok {
		return nil
	}
	values := make([]string, 0, len(rawValues))
	for _, rawValue := range rawValues {
		value, ok := rawValue.(string)
		if ok {
			values = append(values, value)
		}
	}
	return values
}

func assertRouteRulesUseActions(t *testing.T, rules []any) {
	t.Helper()
	for _, rawRule := range rules {
		rule := rawRule.(map[string]any)
		if _, hasOutbound := rule["outbound"]; hasOutbound {
			if rule["action"] != "route" {
				t.Fatalf("route rule with outbound must use action=route: %#v", rule)
			}
		}
		if nested, ok := rule["rules"].([]any); ok {
			assertRouteRulesUseActions(t, nested)
		}
	}
}

func TestRenderOmitsClashAPIByDefault(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Node.Address = "example.com"
	cfg.Node.Port = 443
	cfg.Node.Protocol = "vless"
	cfg.Node.UUID = "00000000-0000-0000-0000-000000000000"

	var rendered map[string]any
	data, err := RenderSingboxConfig(cfg, cfg.ResolveProfile())
	if err != nil {
		t.Fatalf("render config: %v", err)
	}
	if err := json.Unmarshal(data, &rendered); err != nil {
		t.Fatalf("unmarshal config: %v", err)
	}
	if experimental, ok := rendered["experimental"].(map[string]any); ok {
		if _, hasClashAPI := experimental["clash_api"]; hasClashAPI {
			t.Fatalf("clash API must be production-off by default, got experimental=%#v", experimental)
		}
	}
}

func TestRenderAppliesDNSIPv6Mode(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Node.Address = "example.com"
	cfg.Node.Port = 443
	cfg.Node.Protocol = "vless"
	cfg.Node.UUID = "00000000-0000-0000-0000-000000000000"
	cfg.IPv6.Mode = "disable"

	var rendered map[string]any
	data, err := RenderSingboxConfig(cfg, cfg.ResolveProfile())
	if err != nil {
		t.Fatalf("render config: %v", err)
	}
	if err := json.Unmarshal(data, &rendered); err != nil {
		t.Fatalf("unmarshal config: %v", err)
	}

	dns := rendered["dns"].(map[string]any)
	if dns["strategy"] != "ipv4_only" {
		t.Fatalf("IPv6 disabled should render ipv4_only DNS strategy: %#v", dns)
	}
	firstRule := dns["rules"].([]any)[0].(map[string]any)
	if firstRule["action"] != "predefined" {
		t.Fatalf("IPv6 disabled should synthesize empty AAAA responses: %#v", firstRule)
	}
	queryTypes := firstRule["query_type"].([]any)
	if len(queryTypes) != 1 || queryTypes[0] != "AAAA" {
		t.Fatalf("unexpected IPv6 DNS suppression rule: %#v", firstRule)
	}

	cfg.IPv6.Mode = "prefer_ipv4"
	data, err = RenderSingboxConfig(cfg, cfg.ResolveProfile())
	if err != nil {
		t.Fatalf("render config: %v", err)
	}
	if err := json.Unmarshal(data, &rendered); err != nil {
		t.Fatalf("unmarshal config: %v", err)
	}
	dns = rendered["dns"].(map[string]any)
	if dns["strategy"] != "prefer_ipv4" {
		t.Fatalf("prefer_ipv4 should render DNS strategy: %#v", dns)
	}
}

func TestRenderPreservesDNSIPv6ModeWithFakeIP(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Node.Address = "example.com"
	cfg.Node.Port = 443
	cfg.Node.Protocol = "vless"
	cfg.Node.UUID = "00000000-0000-0000-0000-000000000000"
	cfg.DNS.FakeIP = true
	cfg.IPv6.Mode = "disable"

	var rendered map[string]any
	data, err := RenderSingboxConfig(cfg, cfg.ResolveProfile())
	if err != nil {
		t.Fatalf("render config: %v", err)
	}
	if err := json.Unmarshal(data, &rendered); err != nil {
		t.Fatalf("unmarshal config: %v", err)
	}

	dns := rendered["dns"].(map[string]any)
	if dns["strategy"] != "ipv4_only" {
		t.Fatalf("FakeIP should preserve IPv6 DNS strategy: %#v", dns)
	}
	rules := dns["rules"].([]any)
	if rules[0].(map[string]any)["action"] != "predefined" {
		t.Fatalf("AAAA suppression should run before FakeIP: %#v", rules)
	}
	if !hasDNSRuleServer(rules, "fakeip") {
		t.Fatalf("FakeIP DNS rule should remain after IPv6 suppression: %#v", rules)
	}
}

func hasDNSRuleServer(rules []any, server string) bool {
	for _, rawRule := range rules {
		rule := rawRule.(map[string]any)
		if rule["server"] == server {
			return true
		}
	}
	return false
}

func TestRenderAddsClashAPIWhenExplicitlyEnabled(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Proxy.APIPort = 9090
	cfg.Proxy.APISecret = "test-secret"
	cfg.Node.Address = "example.com"
	cfg.Node.Port = 443
	cfg.Node.Protocol = "vless"
	cfg.Node.UUID = "00000000-0000-0000-0000-000000000000"

	var rendered map[string]any
	data, err := RenderSingboxConfig(cfg, cfg.ResolveProfile())
	if err != nil {
		t.Fatalf("render config: %v", err)
	}
	if err := json.Unmarshal(data, &rendered); err != nil {
		t.Fatalf("unmarshal config: %v", err)
	}
	experimental := rendered["experimental"].(map[string]any)
	clashAPI := experimental["clash_api"].(map[string]any)
	if clashAPI["external_controller"] != "127.0.0.1:9090" {
		t.Fatalf("unexpected clash API controller: %#v", clashAPI)
	}
	if clashAPI["secret"] != "test-secret" {
		t.Fatalf("clash API must render configured secret: %#v", clashAPI)
	}
}

func TestRenderRejectsClashAPIWithoutSecret(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Proxy.APIPort = 9090
	cfg.Node.Address = "example.com"
	cfg.Node.Port = 443
	cfg.Node.Protocol = "vless"
	cfg.Node.UUID = "00000000-0000-0000-0000-000000000000"

	if _, err := RenderSingboxConfig(cfg, cfg.ResolveProfile()); err == nil || !strings.Contains(err.Error(), "api_secret") {
		t.Fatalf("expected missing api_secret error, got %v", err)
	}
}

func TestRenderSocksOutboundDoesNotInheritTransport(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Node.Address = "127.0.0.1"
	cfg.Node.Port = 1080
	cfg.Node.Protocol = "socks"
	cfg.Node.Username = "user"
	cfg.Node.Password = "pass"
	cfg.Node.SocksVersion = "5"
	cfg.Transport.Protocol = "reality"
	cfg.Transport.Extra = map[string]string{"security": "reality", "public_key": "bad"}

	var rendered map[string]any
	data, err := RenderSingboxConfig(cfg, cfg.ResolveProfile())
	if err != nil {
		t.Fatalf("render config: %v", err)
	}
	if err := json.Unmarshal(data, &rendered); err != nil {
		t.Fatalf("unmarshal config: %v", err)
	}

	outbound := rendered["outbounds"].([]any)[0].(map[string]any)
	if outbound["type"] != "socks" {
		t.Fatalf("expected socks outbound, got %#v", outbound["type"])
	}
	if outbound["server"] != "127.0.0.1" || outbound["server_port"].(float64) != 1080 {
		t.Fatalf("unexpected socks server fields: %#v", outbound)
	}
	if outbound["version"] != "5" || outbound["username"] != "user" || outbound["password"] != "pass" {
		t.Fatalf("unexpected socks auth fields: %#v", outbound)
	}
	if _, ok := outbound["transport"]; ok {
		t.Fatalf("socks outbound must not inherit V2Ray transport: %#v", outbound)
	}
}

func TestRenderGRPCTransportOmitsXrayOnlyFields(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Node.Address = "example.com"
	cfg.Node.Port = 443
	cfg.Node.Protocol = "vless"
	cfg.Node.UUID = "00000000-0000-0000-0000-000000000000"
	cfg.Transport.Protocol = "grpc"
	cfg.Transport.TLSServer = "google.com"
	cfg.Transport.Extra = map[string]string{
		"security":     "reality",
		"public_key":   "public-key",
		"short_id":     "short-id",
		"service_name": "TunService",
		"mode":         "\ufffd",
		"authority":    "google.com",
	}

	var rendered map[string]any
	data, err := RenderSingboxConfig(cfg, cfg.ResolveProfile())
	if err != nil {
		t.Fatalf("render config: %v", err)
	}
	if err := json.Unmarshal(data, &rendered); err != nil {
		t.Fatalf("unmarshal config: %v", err)
	}

	outbound := rendered["outbounds"].([]any)[0].(map[string]any)
	transport := outbound["transport"].(map[string]any)
	if transport["type"] != "grpc" || transport["service_name"] != "TunService" {
		t.Fatalf("unexpected grpc transport: %#v", transport)
	}
	if _, ok := transport["mode"]; ok {
		t.Fatalf("sing-box grpc transport must not render xray mode: %#v", transport)
	}
	if _, ok := transport["authority"]; ok {
		t.Fatalf("sing-box grpc transport must not render xray authority: %#v", transport)
	}
}

func TestRenderRealityGRPCStoredNodeKeepsGRPCTransport(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Node.Address = ""
	cfg.Node.UUID = ""
	cfg.Profile.ActiveNodeID = "grpc-reality-node"
	cfg.Profile.Nodes = []json.RawMessage{
		json.RawMessage(`{
			"id":"grpc-reality-node",
			"name":"gRPC Reality",
			"protocol":"VLESS",
			"server":"example.com",
			"port":443,
			"outbound":{
				"protocol":"vless",
				"settings":{
					"vnext":[{
						"address":"example.com",
						"port":443,
						"users":[{
							"id":"00000000-0000-0000-0000-000000000000",
							"encryption":"none"
						}]
					}]
				},
				"streamSettings":{
					"network":"grpc",
					"security":"reality",
					"realitySettings":{
						"serverName":"google.com",
						"fingerprint":"chrome",
						"publicKey":"public-key",
						"shortId":"short-id"
					},
					"grpcSettings":{
						"serviceName":"TunService",
						"mode":"gun",
						"authority":"google.com"
					}
				}
			}
		}`),
	}

	var rendered map[string]any
	data, err := RenderSingboxConfig(cfg, cfg.ResolveProfile())
	if err != nil {
		t.Fatalf("render config: %v", err)
	}
	if err := json.Unmarshal(data, &rendered); err != nil {
		t.Fatalf("unmarshal config: %v", err)
	}

	outbound := rendered["outbounds"].([]any)[0].(map[string]any)
	transport := outbound["transport"].(map[string]any)
	if transport["type"] != "grpc" || transport["service_name"] != "TunService" {
		t.Fatalf("unexpected grpc reality transport: %#v", transport)
	}
	tls := outbound["tls"].(map[string]any)
	reality := tls["reality"].(map[string]any)
	if tls["enabled"] != true || reality["enabled"] != true || reality["public_key"] != "public-key" {
		t.Fatalf("reality tls not rendered: %#v", tls)
	}
}

func TestRenderQUICTransportOmitsXrayOnlyFields(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Node.Address = "example.com"
	cfg.Node.Port = 443
	cfg.Node.Protocol = "vless"
	cfg.Node.UUID = "00000000-0000-0000-0000-000000000000"
	cfg.Transport.Protocol = "quic"
	cfg.Transport.Extra = map[string]string{
		"quic_security": "aes-128-gcm",
		"key":           "secret",
		"header_type":   "srtp",
	}

	var rendered map[string]any
	data, err := RenderSingboxConfig(cfg, cfg.ResolveProfile())
	if err != nil {
		t.Fatalf("render config: %v", err)
	}
	if err := json.Unmarshal(data, &rendered); err != nil {
		t.Fatalf("unmarshal config: %v", err)
	}

	outbound := rendered["outbounds"].([]any)[0].(map[string]any)
	transport := outbound["transport"].(map[string]any)
	if len(transport) != 1 || transport["type"] != "quic" {
		t.Fatalf("sing-box quic transport must only render type: %#v", transport)
	}
}

func TestRenderRejectsUnsupportedV2RayTransport(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Node.Address = "example.com"
	cfg.Node.Port = 443
	cfg.Node.Protocol = "vless"
	cfg.Node.UUID = "00000000-0000-0000-0000-000000000000"
	cfg.Transport.Protocol = "kcp"

	_, err := RenderSingboxConfig(cfg, cfg.ResolveProfile())
	if err == nil || !strings.Contains(err.Error(), "does not support V2Ray kcp transport") {
		t.Fatalf("expected unsupported kcp transport error, got %v", err)
	}
}

func TestRenderWireGuardOutboundWithoutKernelInterface(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Node.Address = "203.0.113.1"
	cfg.Node.Port = 51820
	cfg.Node.Protocol = "wireguard"
	cfg.Node.WGPrivateKey = "private-key"
	cfg.Node.WGPeerPublicKey = "peer-public-key"
	cfg.Node.WGPresharedKey = "psk"
	cfg.Node.WGLocalAddress = []string{"10.7.0.2/32", "fd42::2/128"}
	cfg.Node.WGMTU = 1280
	cfg.Node.WGReserved = []int{1, 2, 3}

	var rendered map[string]any
	data, err := RenderSingboxConfig(cfg, cfg.ResolveProfile())
	if err != nil {
		t.Fatalf("render config: %v", err)
	}
	if err := json.Unmarshal(data, &rendered); err != nil {
		t.Fatalf("unmarshal config: %v", err)
	}

	outbound := rendered["outbounds"].([]any)[0].(map[string]any)
	if outbound["type"] != "wireguard" {
		t.Fatalf("expected wireguard outbound, got %#v", outbound)
	}
	if outbound["server"] != "203.0.113.1" || outbound["server_port"].(float64) != 51820 {
		t.Fatalf("unexpected wireguard endpoint: %#v", outbound)
	}
	if outbound["private_key"] != "private-key" || outbound["peer_public_key"] != "peer-public-key" {
		t.Fatalf("wireguard keys were not rendered: %#v", outbound)
	}
	if _, ok := outbound["transport"]; ok {
		t.Fatalf("wireguard outbound must not render v2ray transport: %#v", outbound)
	}
	if _, ok := outbound["interface_name"]; ok {
		t.Fatalf("wireguard outbound must not request a kernel interface: %#v", outbound)
	}
}

func TestRenderProfileNodesAsURLTestOutbounds(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Node.Address = ""
	cfg.Node.UUID = ""
	cfg.Profile.ActiveNodeID = "second-node"
	cfg.Profile.Nodes = []json.RawMessage{
		json.RawMessage(`{
			"id":"first-node",
			"name":"First",
			"protocol":"VLESS",
			"server":"one.example",
			"port":443,
			"outbound":{
				"protocol":"vless",
				"settings":{
					"vnext":[{
						"address":"one.example",
						"port":443,
						"users":[{
							"id":"00000000-0000-0000-0000-000000000001",
							"encryption":"none"
						}]
					}]
				},
				"streamSettings":{
					"network":"tcp",
					"security":"tls",
					"tlsSettings":{"serverName":"one.example","fingerprint":"chrome"}
				}
			}
		}`),
		json.RawMessage(`{
			"id":"second-node",
			"name":"Second",
			"protocol":"SOCKS",
			"server":"127.0.0.1",
			"port":1080,
			"outbound":{
				"protocol":"socks",
				"settings":{
					"address":"127.0.0.1",
					"port":1080,
					"version":"5"
				}
			}
		}`),
	}

	var rendered map[string]any
	data, err := RenderSingboxConfig(cfg, cfg.ResolveProfile())
	if err != nil {
		t.Fatalf("render config: %v", err)
	}
	if err := json.Unmarshal(data, &rendered); err != nil {
		t.Fatalf("unmarshal config: %v", err)
	}

	outbounds := rendered["outbounds"].([]any)
	if len(outbounds) != 5 {
		t.Fatalf("expected two nodes + urltest + selector + direct, got %#v", outbounds)
	}

	firstNode := outbounds[0].(map[string]any)
	if firstNode["domain_resolver"] != "direct-dns" {
		t.Fatalf("domain node should use direct-dns resolver: %#v", firstNode)
	}
	secondNode := outbounds[1].(map[string]any)
	if _, ok := secondNode["domain_resolver"]; ok {
		t.Fatalf("IP node should not set domain_resolver: %#v", secondNode)
	}

	urltest := outbounds[2].(map[string]any)
	if urltest["type"] != "urltest" || urltest["tag"] != "auto" {
		t.Fatalf("expected auto urltest outbound, got %#v", urltest)
	}
	tags := urltest["outbounds"].([]any)
	if len(tags) != 2 || tags[0] != "node-first-node" || tags[1] != "node-second-node" {
		t.Fatalf("unexpected urltest outbounds: %#v", tags)
	}
	selector := outbounds[3].(map[string]any)
	if selector["type"] != "selector" || selector["tag"] != "proxy" {
		t.Fatalf("expected proxy selector outbound, got %#v", selector)
	}
	selectorTags := selector["outbounds"].([]any)
	if len(selectorTags) != 3 || selectorTags[0] != "auto" || selectorTags[1] != "node-first-node" || selectorTags[2] != "node-second-node" {
		t.Fatalf("unexpected selector outbounds: %#v", selectorTags)
	}
	if selector["default"] != "node-second-node" {
		t.Fatalf("expected active node to be selector default, got %#v", selector["default"])
	}

	route := rendered["route"].(map[string]any)
	if route["final"] != "proxy" {
		t.Fatalf("route final should target selector proxy, got %#v", route["final"])
	}
}

func TestRenderPanelShadowsocksFallsBackToShareLinkSecret(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Node.Address = ""
	cfg.Node.UUID = ""
	cfg.Profile.Nodes = []json.RawMessage{
		json.RawMessage(`{
			"id":"ss-node",
			"name":"SS",
			"protocol":"SHADOWSOCKS",
			"server":"127.0.0.1",
			"port":8388,
			"link":"ss://aes-128-gcm:secret@127.0.0.1:8388#SS",
			"outbound":{
				"protocol":"shadowsocks",
				"settings":{
					"servers":[{
						"address":"127.0.0.1",
						"port":8388,
						"method":"aes-128-gcm",
						"password":""
					}]
				}
			}
		}`),
	}

	var rendered map[string]any
	data, err := RenderSingboxConfig(cfg, cfg.ResolveProfile())
	if err != nil {
		t.Fatalf("render config: %v", err)
	}
	if err := json.Unmarshal(data, &rendered); err != nil {
		t.Fatalf("unmarshal config: %v", err)
	}

	outbounds := rendered["outbounds"].([]any)
	proxy := outbounds[0].(map[string]any)
	if proxy["type"] != "shadowsocks" || proxy["password"] != "secret" {
		t.Fatalf("expected shadowsocks password from share link, got %#v", proxy)
	}
}

func TestRenderAutoSkipsInvalidPanelNode(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Node.Address = ""
	cfg.Node.UUID = ""
	cfg.Profile.Nodes = []json.RawMessage{
		json.RawMessage(`{
			"id":"bad-ss",
			"name":"Bad SS",
			"protocol":"SHADOWSOCKS",
			"server":"127.0.0.1",
			"port":8388,
			"outbound":{
				"protocol":"shadowsocks",
				"settings":{
					"servers":[{"address":"127.0.0.1","port":8388,"method":"aes-128-gcm","password":""}]
				}
			}
		}`),
		json.RawMessage(`{
			"id":"good-vless",
			"name":"Good VLESS",
			"protocol":"VLESS",
			"server":"one.example",
			"port":443,
			"outbound":{
				"protocol":"vless",
				"settings":{
					"vnext":[{
						"address":"one.example",
						"port":443,
						"users":[{"id":"00000000-0000-0000-0000-000000000001","encryption":"none"}]
					}]
				}
			}
		}`),
	}

	var rendered map[string]any
	data, err := RenderSingboxConfig(cfg, cfg.ResolveProfile())
	if err != nil {
		t.Fatalf("render config: %v", err)
	}
	if err := json.Unmarshal(data, &rendered); err != nil {
		t.Fatalf("unmarshal config: %v", err)
	}

	outbounds := rendered["outbounds"].([]any)
	if len(outbounds) != 2 {
		t.Fatalf("expected one usable proxy plus direct, got %#v", outbounds)
	}
	proxy := outbounds[0].(map[string]any)
	if proxy["type"] != "vless" || proxy["tag"] != "proxy" {
		t.Fatalf("expected valid VLESS proxy after skipping invalid SS, got %#v", proxy)
	}
}

func TestRenderActiveInvalidPanelNodeFails(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Node.Address = ""
	cfg.Node.UUID = ""
	cfg.Profile.ActiveNodeID = "bad-ss"
	cfg.Profile.Nodes = []json.RawMessage{
		json.RawMessage(`{
			"id":"bad-ss",
			"name":"Bad SS",
			"protocol":"SHADOWSOCKS",
			"server":"127.0.0.1",
			"port":8388,
			"outbound":{
				"protocol":"shadowsocks",
				"settings":{
					"servers":[{"address":"127.0.0.1","port":8388,"method":"aes-128-gcm","password":""}]
				}
			}
		}`),
		json.RawMessage(`{
			"id":"good-vless",
			"name":"Good VLESS",
			"protocol":"VLESS",
			"server":"one.example",
			"port":443,
			"outbound":{
				"protocol":"vless",
				"settings":{
					"vnext":[{
						"address":"one.example",
						"port":443,
						"users":[{"id":"00000000-0000-0000-0000-000000000001","encryption":"none"}]
					}]
				}
			}
		}`),
	}

	_, err := RenderSingboxConfig(cfg, cfg.ResolveProfile())
	if err == nil || !strings.Contains(err.Error(), "active node") {
		t.Fatalf("expected active invalid node error, got %v", err)
	}
}

func TestRenderPanelNodeGroupsAsSelectorOutbounds(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Node.Address = ""
	cfg.Node.UUID = ""
	cfg.Profile.Nodes = []json.RawMessage{
		json.RawMessage(`{
			"id":"first-node",
			"name":"First",
			"group":"Europe",
			"protocol":"SOCKS",
			"server":"127.0.0.1",
			"port":1081,
			"outbound":{
				"protocol":"socks",
				"settings":{"address":"127.0.0.1","port":1081,"version":"5"}
			}
		}`),
		json.RawMessage(`{
			"id":"second-node",
			"name":"Second",
			"group":"Europe",
			"protocol":"SOCKS",
			"server":"127.0.0.1",
			"port":1082,
			"outbound":{
				"protocol":"socks",
				"settings":{"address":"127.0.0.1","port":1082,"version":"5"}
			}
		}`),
	}

	var rendered map[string]any
	data, err := RenderSingboxConfig(cfg, cfg.ResolveProfile())
	if err != nil {
		t.Fatalf("render config: %v", err)
	}
	if err := json.Unmarshal(data, &rendered); err != nil {
		t.Fatalf("unmarshal config: %v", err)
	}

	byTag := map[string]map[string]any{}
	for _, rawOutbound := range rendered["outbounds"].([]any) {
		outbound := rawOutbound.(map[string]any)
		if tag, ok := outbound["tag"].(string); ok {
			byTag[tag] = outbound
		}
	}

	globalSelector := byTag["proxy"]
	globalTags := globalSelector["outbounds"].([]any)
	if len(globalTags) != 4 || globalTags[0] != "auto" || globalTags[1] != "node-first-node" || globalTags[2] != "node-second-node" || globalTags[3] != "group-europe" {
		t.Fatalf("global selector should expose group selector after raw node tags: %#v", globalTags)
	}

	groupAuto := byTag["group-europe-auto"]
	if groupAuto["type"] != "urltest" {
		t.Fatalf("expected group urltest outbound, got %#v", groupAuto)
	}
	groupAutoTags := groupAuto["outbounds"].([]any)
	if len(groupAutoTags) != 2 || groupAutoTags[0] != "node-first-node" || groupAutoTags[1] != "node-second-node" {
		t.Fatalf("unexpected group urltest members: %#v", groupAutoTags)
	}

	groupSelector := byTag["group-europe"]
	if groupSelector["type"] != "selector" || groupSelector["default"] != "group-europe-auto" {
		t.Fatalf("unexpected group selector: %#v", groupSelector)
	}
	groupSelectorTags := groupSelector["outbounds"].([]any)
	if len(groupSelectorTags) != 3 || groupSelectorTags[0] != "group-europe-auto" || groupSelectorTags[1] != "node-first-node" || groupSelectorTags[2] != "node-second-node" {
		t.Fatalf("unexpected group selector members: %#v", groupSelectorTags)
	}
}

func TestRenderAppGroupRouteRules(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Node.Address = ""
	cfg.Node.UUID = ""
	cfg.Routing.CustomDirect = []string{"example.org"}
	cfg.Routing.AlwaysDirectApps = []string{"com.rknnovpn.panel"}
	cfg.Apps.AppGroups = map[string]string{
		"com.chat.app":  "Europe",
		"com.video.app": "Europe",
		"com.unknown":   "Missing",
	}
	cfg.Profile.Nodes = []json.RawMessage{
		json.RawMessage(`{
			"id":"first-node",
			"name":"First",
			"group":"Europe",
			"protocol":"SOCKS",
			"server":"127.0.0.1",
			"port":1081,
			"outbound":{
				"protocol":"socks",
				"settings":{"address":"127.0.0.1","port":1081,"version":"5"}
			}
		}`),
		json.RawMessage(`{
			"id":"second-node",
			"name":"Second",
			"group":"Europe",
			"protocol":"SOCKS",
			"server":"127.0.0.1",
			"port":1082,
			"outbound":{
				"protocol":"socks",
				"settings":{"address":"127.0.0.1","port":1082,"version":"5"}
			}
		}`),
	}

	var rendered map[string]any
	data, err := RenderSingboxConfig(cfg, cfg.ResolveProfile())
	if err != nil {
		t.Fatalf("render config: %v", err)
	}
	if err := json.Unmarshal(data, &rendered); err != nil {
		t.Fatalf("unmarshal config: %v", err)
	}

	rules := rendered["route"].(map[string]any)["rules"].([]any)
	var directRule map[string]any
	var groupRule map[string]any
	for _, rawRule := range rules {
		rule := rawRule.(map[string]any)
		switch rule["outbound"] {
		case "direct":
			if packages, ok := rule["package_name"].([]any); ok && len(packages) == 1 && packages[0] == "com.rknnovpn.panel" {
				directRule = rule
			}
		case "group-europe":
			groupRule = rule
		}
	}
	if directRule == nil {
		t.Fatalf("expected always-direct package route, got %#v", rules)
	}
	if groupRule == nil {
		t.Fatalf("expected app group route, got %#v", rules)
	}
	packages := groupRule["package_name"].([]any)
	if len(packages) != 2 || packages[0] != "com.chat.app" || packages[1] != "com.video.app" {
		t.Fatalf("unexpected app group packages: %#v", packages)
	}
}

func TestRenderOmitsInternalStatusHTTPInboundByDefault(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Node.Address = "example.com"
	cfg.Node.Port = 443
	cfg.Node.Protocol = "vless"
	cfg.Node.UUID = "00000000-0000-0000-0000-000000000000"

	var rendered map[string]any
	data, err := RenderSingboxConfig(cfg, cfg.ResolveProfile())
	if err != nil {
		t.Fatalf("render config: %v", err)
	}
	if err := json.Unmarshal(data, &rendered); err != nil {
		t.Fatalf("unmarshal config: %v", err)
	}

	inbounds := rendered["inbounds"].([]any)
	for _, rawInbound := range inbounds {
		inbound := rawInbound.(map[string]any)
		if inbound["tag"] == "status-http-in" {
			t.Fatalf("status-http-in inbound must be disabled by default: %#v", inbound)
		}
	}
}

func TestRenderAddsInternalStatusHTTPInboundWhenExplicitlyEnabled(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Node.Address = "example.com"
	cfg.Node.Port = 443
	cfg.Node.Protocol = "vless"
	cfg.Node.UUID = "00000000-0000-0000-0000-000000000000"
	cfg.Profile.Inbounds = json.RawMessage(`{"httpPort":10809}`)

	var rendered map[string]any
	data, err := RenderSingboxConfig(cfg, cfg.ResolveProfile())
	if err != nil {
		t.Fatalf("render config: %v", err)
	}
	if err := json.Unmarshal(data, &rendered); err != nil {
		t.Fatalf("unmarshal config: %v", err)
	}

	inbounds := rendered["inbounds"].([]any)
	found := false
	for _, rawInbound := range inbounds {
		inbound := rawInbound.(map[string]any)
		if inbound["tag"] == "status-http-in" {
			found = true
			if inbound["type"] != "http" {
				t.Fatalf("unexpected helper inbound type: %#v", inbound)
			}
			if inbound["listen"] != "127.0.0.1" {
				t.Fatalf("helper inbound must stay localhost-only: %#v", inbound)
			}
			if inbound["listen_port"].(float64) != 10809 {
				t.Fatalf("unexpected helper inbound port: %#v", inbound)
			}
		}
	}
	if !found {
		t.Fatal("status-http-in inbound was not rendered")
	}
}

func TestRenderAddsInternalStatusSOCKSInboundWhenExplicitlyEnabled(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Node.Address = "example.com"
	cfg.Node.Port = 443
	cfg.Node.Protocol = "vless"
	cfg.Node.UUID = "00000000-0000-0000-0000-000000000000"
	cfg.Profile.Inbounds = json.RawMessage(`{"socksPort":10808}`)

	var rendered map[string]any
	data, err := RenderSingboxConfig(cfg, cfg.ResolveProfile())
	if err != nil {
		t.Fatalf("render config: %v", err)
	}
	if err := json.Unmarshal(data, &rendered); err != nil {
		t.Fatalf("unmarshal config: %v", err)
	}

	inbounds := rendered["inbounds"].([]any)
	found := false
	for _, rawInbound := range inbounds {
		inbound := rawInbound.(map[string]any)
		if inbound["tag"] == "status-socks-in" {
			found = true
			if inbound["type"] != "socks" {
				t.Fatalf("unexpected helper inbound type: %#v", inbound)
			}
			if inbound["listen"] != "127.0.0.1" {
				t.Fatalf("helper inbound must stay localhost-only by default: %#v", inbound)
			}
			if inbound["listen_port"].(float64) != 10808 {
				t.Fatalf("unexpected helper inbound port: %#v", inbound)
			}
		}
	}
	if !found {
		t.Fatal("status-socks-in inbound was not rendered")
	}
}

func TestRenderHelperInboundsIgnoreAllowLAN(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Node.Address = "example.com"
	cfg.Node.Port = 443
	cfg.Node.Protocol = "vless"
	cfg.Node.UUID = "00000000-0000-0000-0000-000000000000"
	cfg.Profile.Inbounds = json.RawMessage(`{"httpPort":10809,"socksPort":10808,"allowLan":true}`)

	var rendered map[string]any
	data, err := RenderSingboxConfig(cfg, cfg.ResolveProfile())
	if err != nil {
		t.Fatalf("render config: %v", err)
	}
	if err := json.Unmarshal(data, &rendered); err != nil {
		t.Fatalf("unmarshal config: %v", err)
	}

	inbounds := rendered["inbounds"].([]any)
	for _, wantTag := range []string{"status-http-in", "status-socks-in"} {
		found := false
		for _, rawInbound := range inbounds {
			inbound := rawInbound.(map[string]any)
			if inbound["tag"] != wantTag {
				continue
			}
			found = true
			if inbound["listen"] != "127.0.0.1" {
				t.Fatalf("%s escaped localhost despite allowLan: %#v", wantTag, inbound)
			}
		}
		if !found {
			t.Fatalf("%s inbound was not rendered", wantTag)
		}
	}
}

func TestRenderSkipsStaleProfileNodes(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Node.Address = "fallback.example"
	cfg.Node.Port = 443
	cfg.Node.Protocol = "vless"
	cfg.Node.UUID = "00000000-0000-0000-0000-000000000000"
	cfg.Profile.ActiveNodeID = "stale-node"
	cfg.Profile.Nodes = []json.RawMessage{json.RawMessage(`{
		"id":"stale-node",
		"name":"Removed by subscription",
		"protocol":"VLESS",
		"server":"stale.example",
		"port":443,
		"stale":true,
		"outbound":{
			"protocol":"vless",
			"settings":{"vnext":[{"address":"stale.example","port":443,"users":[{"id":"11111111-1111-1111-1111-111111111111","encryption":"none"}]}]}
		}
	}`)}

	profiles := ProfilesFromConfigNodes(cfg)
	if len(profiles) != 0 {
		t.Fatalf("stale profile nodes must not become runtime profiles: %#v", profiles)
	}
}
