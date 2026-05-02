package profile

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/netip"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/youtubediscord/RKNnoVPN/daemon/internal/config"
)

const CurrentSchemaVersion = 2

type Document struct {
	SchemaVersion int             `json:"profileSchemaVersion"`
	ID            string          `json:"id"`
	Name          string          `json:"name"`
	ActiveNodeID  string          `json:"activeNodeId,omitempty"`
	Nodes         []Node          `json:"nodes"`
	Subscriptions []Subscription  `json:"subscriptions,omitempty"`
	Runtime       RuntimeConfig   `json:"runtime"`
	Routing       RoutingConfig   `json:"routing"`
	DNS           DNSConfig       `json:"dns"`
	Health        HealthConfig    `json:"health"`
	Sharing       SharingConfig   `json:"sharing"`
	Tun           TunConfig       `json:"tun"`
	Inbounds      InboundsConfig  `json:"inbounds"`
	Extra         json.RawMessage `json:"extra,omitempty"`
}

type RuntimeConfig struct {
	BackendKind    string `json:"backendKind,omitempty"`
	FallbackPolicy string `json:"fallbackPolicy,omitempty"`
}

type RoutingConfig struct {
	Mode                        string            `json:"mode"`
	BypassRussia                bool              `json:"bypassRussia"`
	AppProxyList                []string          `json:"appProxyList,omitempty"`
	AppBypassList               []string          `json:"appBypassList,omitempty"`
	AppGroupRoutes              map[string]string `json:"appGroupRoutes,omitempty"`
	DirectDomains               []string          `json:"directDomains,omitempty"`
	ProxyDomains                []string          `json:"proxyDomains,omitempty"`
	BlockDomains                []string          `json:"blockDomains,omitempty"`
	DirectIps                   []string          `json:"directIps,omitempty"`
	ProxyIps                    []string          `json:"proxyIps,omitempty"`
	BlockIps                    []string          `json:"blockIps,omitempty"`
	AlwaysDirectAppList         []string          `json:"alwaysDirectAppList,omitempty"`
	AlwaysDirectExcludedAppList []string          `json:"alwaysDirectExcludedAppList,omitempty"`
	AlwaysDirectSystemApps      bool              `json:"alwaysDirectSystemApps"`
}

type DNSConfig struct {
	RemoteDNS   string `json:"remoteDns"`
	DirectDNS   string `json:"directDns"`
	BootstrapIP string `json:"bootstrapIp"`
	IPv6Mode    string `json:"ipv6Mode"`
	FakeDNS     bool   `json:"fakeDns"`
}

type HealthConfig struct {
	Enabled            bool     `json:"enabled"`
	IntervalSec        int      `json:"intervalSec"`
	Threshold          int      `json:"threshold"`
	CheckURL           string   `json:"checkUrl"`
	TimeoutSec         int      `json:"timeoutSec"`
	DNSProbeDomains    []string `json:"dnsProbeDomains,omitempty"`
	EgressURLs         []string `json:"egressUrls,omitempty"`
	DNSIsHardReadiness bool     `json:"dnsIsHardReadiness"`
}

type SharingConfig struct {
	Enabled    bool     `json:"enabled"`
	Interfaces []string `json:"interfaces,omitempty"`
}

// TunConfig is a reserved profile-schema field. The current root TPROXY
// runtime must not create or manage a TUN interface from these values.
type TunConfig struct {
	Enabled     bool   `json:"enabled"`
	MTU         int    `json:"mtu"`
	IPv4Address string `json:"ipv4Address"`
	IPv6        bool   `json:"ipv6"`
	AutoRoute   bool   `json:"autoRoute"`
	StrictRoute bool   `json:"strictRoute"`
}

type InboundsConfig struct {
	SocksPort int  `json:"socksPort"`
	HTTPPort  int  `json:"httpPort"`
	AllowLAN  bool `json:"allowLan"`
}

type Node struct {
	ID            string          `json:"id"`
	Name          string          `json:"name"`
	Protocol      string          `json:"protocol"`
	Server        string          `json:"server"`
	Port          int             `json:"port"`
	Link          string          `json:"link,omitempty"`
	Outbound      json.RawMessage `json:"outbound"`
	Group         string          `json:"group,omitempty"`
	OwnerPackage  string          `json:"ownerPackage,omitempty"`
	LatencyMS     *int            `json:"latencyMs,omitempty"`
	ResponseMS    *int            `json:"responseMs,omitempty"`
	ThroughputBps *int64          `json:"throughputBps,omitempty"`
	TestStatus    string          `json:"testStatus,omitempty"`
	CreatedAt     int64           `json:"createdAt,omitempty"`
	Stale         bool            `json:"stale,omitempty"`
	Source        NodeSource      `json:"source"`
	Extra         json.RawMessage `json:"extra,omitempty"`
}

type NodeSource struct {
	Type        string `json:"type"`
	URL         string `json:"url,omitempty"`
	ProviderKey string `json:"providerKey,omitempty"`
	LastSeenAt  int64  `json:"lastSeenAt,omitempty"`
}

type Subscription struct {
	ProviderKey       string `json:"providerKey"`
	URL               string `json:"url"`
	Name              string `json:"name,omitempty"`
	LastFetchedAt     int64  `json:"lastFetchedAt,omitempty"`
	LastSeenNodeCount int    `json:"lastSeenNodeCount,omitempty"`
	StaleNodeCount    int    `json:"staleNodeCount,omitempty"`
	UploadBytes       int64  `json:"uploadBytes,omitempty"`
	DownloadBytes     int64  `json:"downloadBytes,omitempty"`
	TotalBytes        int64  `json:"totalBytes,omitempty"`
	ExpireTimestamp   int64  `json:"expireTimestamp,omitempty"`
	ParseFailures     int    `json:"parseFailures,omitempty"`
}

type Warning struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type MergeStats = map[string]int

func FromConfig(cfg *config.Config) Document {
	if cfg == nil {
		cfg = config.DefaultConfig()
	}
	panel := cfg.Profile
	doc := Document{
		SchemaVersion: CurrentSchemaVersion,
		ID:            firstNonEmpty(panel.ID, "default"),
		Name:          firstNonEmpty(panel.Name, "Default"),
		ActiveNodeID:  panel.ActiveNodeID,
		Nodes:         decodeNodes(panel.Nodes),
		Subscriptions: decodeSubscriptions(panel.Subscriptions),
		Runtime: RuntimeConfig{
			BackendKind:    cfg.RuntimeV2.BackendKind,
			FallbackPolicy: cfg.RuntimeV2.FallbackPolicy,
		},
		Routing:  routingFromConfig(cfg),
		DNS:      dnsFromConfig(cfg),
		Health:   healthFromConfig(cfg),
		Sharing:  SharingConfig{Enabled: cfg.Sharing.Enabled, Interfaces: append([]string(nil), cfg.Sharing.Interfaces...)},
		Tun:      decodeTun(panel.Tun),
		Inbounds: decodeInbounds(panel.Inbounds),
		Extra:    cloneRaw(panel.Extra),
	}
	return doc
}

func DecodeStrictDocument(data []byte) (Document, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var doc Document
	if err := decoder.Decode(&doc); err != nil {
		return Document{}, err
	}
	var extra json.RawMessage
	if err := decoder.Decode(&extra); err != io.EOF {
		return Document{}, fmt.Errorf("profile document must contain a single JSON object")
	}
	return doc, nil
}

func ApplyToConfig(base *config.Config, doc Document) (*config.Config, []Warning, error) {
	if base == nil {
		base = config.DefaultConfig()
	}
	normalized, warnings, err := Normalize(doc)
	if err != nil {
		return nil, warnings, err
	}
	next := *base
	next.SchemaVersion = config.CurrentSchemaVersion
	next.Profile = panelFromDocument(normalized)
	next.RuntimeV2 = config.RuntimeV2Config{
		BackendKind:    firstNonEmpty(normalized.Runtime.BackendKind, "ROOT_TPROXY"),
		FallbackPolicy: firstNonEmpty(normalized.Runtime.FallbackPolicy, "OFFER_RESET"),
	}
	applyRoutingToConfig(&next, normalized.Routing)
	applyDNSToConfig(&next, normalized.DNS)
	next.Health = config.HealthConfig{
		Enabled:            normalized.Health.Enabled,
		IntervalSec:        normalized.Health.IntervalSec,
		Threshold:          normalized.Health.Threshold,
		URL:                firstNonEmpty(normalized.Health.CheckURL, "https://www.gstatic.com/generate_204"),
		TimeoutSec:         normalized.Health.TimeoutSec,
		DNSProbeDomains:    append([]string(nil), normalized.Health.DNSProbeDomains...),
		EgressURLs:         append([]string(nil), normalized.Health.EgressURLs...),
		DNSIsHardReadiness: normalized.Health.DNSIsHardReadiness,
	}
	next.Sharing = config.SharingConfig{
		Enabled:    normalized.Sharing.Enabled,
		Interfaces: append([]string(nil), normalized.Sharing.Interfaces...),
	}
	next.SyncFromProfileProjection(true)
	if err := next.Validate(); err != nil {
		return nil, warnings, err
	}
	if profile := next.ResolveProfile(); profile != nil && profile.Address != "" {
		if _, err := config.RenderSingboxConfig(&next, profile); err != nil {
			return nil, warnings, fmt.Errorf("render profile draft: %w", err)
		}
	}
	return &next, warnings, nil
}

func Normalize(doc Document) (Document, []Warning, error) {
	warnings := make([]Warning, 0, 2)
	if doc.SchemaVersion == 0 {
		doc.SchemaVersion = CurrentSchemaVersion
	}
	if doc.SchemaVersion > CurrentSchemaVersion {
		return doc, warnings, fmt.Errorf("profile schema %d is newer than daemon supports (%d)", doc.SchemaVersion, CurrentSchemaVersion)
	}
	doc.ID = firstNonEmpty(strings.TrimSpace(doc.ID), "default")
	doc.Name = firstNonEmpty(strings.TrimSpace(doc.Name), "Default")
	doc.Runtime.BackendKind = firstNonEmpty(strings.TrimSpace(doc.Runtime.BackendKind), "ROOT_TPROXY")
	doc.Runtime.FallbackPolicy = firstNonEmpty(strings.TrimSpace(doc.Runtime.FallbackPolicy), "OFFER_RESET")
	doc.Routing.Mode = firstNonEmpty(strings.TrimSpace(doc.Routing.Mode), "PROXY_ALL")
	doc.DNS.RemoteDNS = firstNonEmpty(strings.TrimSpace(doc.DNS.RemoteDNS), "https://1.1.1.1/dns-query")
	doc.DNS.DirectDNS = firstNonEmpty(strings.TrimSpace(doc.DNS.DirectDNS), "https://dns.google/dns-query")
	doc.DNS.BootstrapIP = firstNonEmpty(strings.TrimSpace(doc.DNS.BootstrapIP), "1.1.1.1")
	doc.DNS.IPv6Mode = firstNonEmpty(strings.TrimSpace(doc.DNS.IPv6Mode), "MIRROR")
	if doc.Health.IntervalSec == 0 {
		doc.Health.IntervalSec = 30
	}
	if doc.Health.Threshold == 0 {
		doc.Health.Threshold = 3
	}
	if doc.Health.TimeoutSec == 0 {
		doc.Health.TimeoutSec = 5
	}
	if doc.Health.CheckURL == "" {
		doc.Health.CheckURL = "https://www.gstatic.com/generate_204"
	}
	if doc.Tun.MTU == 0 {
		doc.Tun.MTU = 9000
	}
	if doc.Tun.IPv4Address == "" {
		doc.Tun.IPv4Address = "172.19.0.1/30"
	}
	if doc.Inbounds.AllowLAN {
		return doc, warnings, fmt.Errorf("profile.inbounds.allowLan is not supported")
	}
	if err := validatePort("profile.inbounds.socksPort", doc.Inbounds.SocksPort, true); err != nil {
		return doc, warnings, err
	}
	if err := validatePort("profile.inbounds.httpPort", doc.Inbounds.HTTPPort, true); err != nil {
		return doc, warnings, err
	}

	seen := make(map[string]bool, len(doc.Nodes))
	for i := range doc.Nodes {
		node := &doc.Nodes[i]
		node.ID = strings.TrimSpace(node.ID)
		if node.ID == "" {
			node.ID = fmt.Sprintf("node-%d", i+1)
		}
		if seen[node.ID] {
			return doc, warnings, fmt.Errorf("profile.nodes[%d].id duplicates %q", i, node.ID)
		}
		seen[node.ID] = true
		node.Protocol = normalizeProtocol(node.Protocol)
		node.Server = strings.TrimSpace(node.Server)
		node.Name = firstNonEmpty(strings.TrimSpace(node.Name), node.Server, node.ID)
		node.Group = firstNonEmpty(strings.TrimSpace(node.Group), "Default")
		node.OwnerPackage = strings.TrimSpace(node.OwnerPackage)
		if node.OwnerPackage != "" && !isValidAndroidPackageName(node.OwnerPackage) {
			return doc, warnings, fmt.Errorf("profile.nodes[%d].ownerPackage must be an Android package name", i)
		}
		if node.CreatedAt == 0 {
			node.CreatedAt = time.Now().UnixMilli()
		}
		if len(bytes.TrimSpace(node.Outbound)) == 0 {
			node.Outbound = []byte(`{}`)
		} else if !json.Valid(node.Outbound) {
			return doc, warnings, fmt.Errorf("profile.nodes[%d].outbound is invalid JSON", i)
		}
		if err := validatePort(fmt.Sprintf("profile.nodes[%d].port", i), node.Port, false); err != nil {
			return doc, warnings, err
		}
		node.Source.Type = strings.ToUpper(strings.TrimSpace(node.Source.Type))
		if node.Source.Type == "" {
			return doc, warnings, fmt.Errorf("profile.nodes[%d].source.type is required", i)
		}
		switch node.Source.Type {
		case "MANUAL":
			if node.Stale {
				return doc, warnings, fmt.Errorf("profile.nodes[%d].stale requires subscription source", i)
			}
		case "SUBSCRIPTION":
			node.Source.URL = strings.TrimSpace(node.Source.URL)
			node.Source.ProviderKey = strings.TrimSpace(node.Source.ProviderKey)
			if node.Source.ProviderKey == "" {
				return doc, warnings, fmt.Errorf("profile.nodes[%d].source.providerKey is required for subscription nodes", i)
			}
			if IsDisallowedSubscriptionEndpoint(node.Server) {
				return doc, warnings, fmt.Errorf("profile.nodes[%d].server must not be local, private, or reserved for subscription nodes", i)
			}
		default:
			return doc, warnings, fmt.Errorf("profile.nodes[%d].source.type must be MANUAL or SUBSCRIPTION", i)
		}
	}
	subscriptionProviderKeys := make(map[string]bool, len(doc.Subscriptions))
	for i := range doc.Subscriptions {
		sub := &doc.Subscriptions[i]
		sub.ProviderKey = strings.TrimSpace(sub.ProviderKey)
		sub.URL = strings.TrimSpace(sub.URL)
		sub.Name = strings.TrimSpace(sub.Name)
		if sub.ProviderKey == "" {
			return doc, warnings, fmt.Errorf("profile.subscriptions[%d].providerKey is required", i)
		}
		if sub.URL == "" {
			return doc, warnings, fmt.Errorf("profile.subscriptions[%d].url is required", i)
		}
		subscriptionProviderKeys[sub.ProviderKey] = true
	}
	for i, node := range doc.Nodes {
		if node.Source.Type != "SUBSCRIPTION" {
			continue
		}
		if !subscriptionProviderKeys[node.Source.ProviderKey] {
			return doc, warnings, fmt.Errorf("profile.nodes[%d].source.providerKey %q is missing from profile.subscriptions", i, node.Source.ProviderKey)
		}
	}

	liveIDs := make(map[string]bool)
	for _, node := range doc.Nodes {
		if !node.Stale {
			liveIDs[node.ID] = true
		}
	}
	if doc.ActiveNodeID != "" && !liveIDs[doc.ActiveNodeID] {
		old := doc.ActiveNodeID
		doc.ActiveNodeID = ""
		if len(doc.Nodes) > 0 {
			for _, node := range doc.Nodes {
				if !node.Stale {
					doc.ActiveNodeID = node.ID
					break
				}
			}
		}
		warnings = append(warnings, Warning{
			Code:    "active_node_repaired",
			Message: fmt.Sprintf("active node %q was missing or stale; selected %q", old, doc.ActiveNodeID),
		})
	}
	doc.Subscriptions = normalizeSubscriptions(doc.Subscriptions, doc.Nodes)
	return doc, warnings, nil
}

func MergeNodes(current Document, incoming []Node) (Document, map[string]int) {
	next := current
	byKey := map[string]int{}
	for i, node := range next.Nodes {
		if key := NodeMatchKey(node); key != "" {
			byKey[key] = i
		}
	}
	stats := map[string]int{"added": 0, "updated": 0, "unchanged": 0, "stale": 0}
	seenIncoming := map[string]bool{}
	importedActiveNodeID := ""
	for _, node := range incoming {
		key := NodeMatchKey(node)
		if key == "" || seenIncoming[key] {
			continue
		}
		seenIncoming[key] = true
		if index, ok := byKey[key]; ok {
			existing := next.Nodes[index]
			node.ID = existing.ID
			node.Name = firstNonEmpty(existing.Name, node.Name)
			node.Group = firstNonEmpty(existing.Group, node.Group)
			node.CreatedAt = existing.CreatedAt
			if string(existing.Outbound) == string(node.Outbound) {
				stats["unchanged"]++
			} else {
				stats["updated"]++
			}
			next.Nodes[index] = node
			if importedActiveNodeID == "" && !node.Stale {
				importedActiveNodeID = node.ID
			}
		} else {
			stats["added"]++
			next.Nodes = append(next.Nodes, node)
			if importedActiveNodeID == "" && !node.Stale {
				importedActiveNodeID = node.ID
			}
		}
	}
	if importedActiveNodeID != "" {
		next.ActiveNodeID = importedActiveNodeID
	} else if next.ActiveNodeID == "" || nodeByID(next.Nodes, next.ActiveNodeID) == nil || nodeByID(next.Nodes, next.ActiveNodeID).Stale {
		next.ActiveNodeID = ""
		for _, node := range next.Nodes {
			if !node.Stale {
				next.ActiveNodeID = node.ID
				break
			}
		}
	}
	return next, stats
}

func normalizeSubscriptions(subscriptions []Subscription, nodes []Node) []Subscription {
	byKey := make(map[string]Subscription)
	for _, sub := range subscriptions {
		sub.ProviderKey = strings.TrimSpace(sub.ProviderKey)
		sub.URL = strings.TrimSpace(sub.URL)
		if sub.ProviderKey == "" || sub.URL == "" {
			continue
		}
		sub.LastSeenNodeCount = 0
		sub.StaleNodeCount = 0
		byKey[sub.ProviderKey] = sub
	}
	for _, node := range nodes {
		if node.Source.Type != "SUBSCRIPTION" || node.Source.ProviderKey == "" {
			continue
		}
		sub := byKey[node.Source.ProviderKey]
		if sub.ProviderKey == "" {
			continue
		}
		if node.Stale {
			sub.StaleNodeCount++
		} else {
			sub.LastSeenNodeCount++
		}
		byKey[sub.ProviderKey] = sub
	}
	keys := make([]string, 0, len(byKey))
	for key := range byKey {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]Subscription, 0, len(keys))
	for _, key := range keys {
		result = append(result, byKey[key])
	}
	return result
}

func panelFromDocument(doc Document) config.ProfileProjectionConfig {
	nodes := make([]json.RawMessage, 0, len(doc.Nodes))
	for _, node := range doc.Nodes {
		raw, _ := json.Marshal(node)
		nodes = append(nodes, raw)
	}
	subscriptions := make([]json.RawMessage, 0, len(doc.Subscriptions))
	for _, sub := range doc.Subscriptions {
		raw, _ := json.Marshal(sub)
		subscriptions = append(subscriptions, raw)
	}
	tun, _ := json.Marshal(doc.Tun)
	inbounds, _ := json.Marshal(doc.Inbounds)
	return config.ProfileProjectionConfig{
		ID:            doc.ID,
		Name:          doc.Name,
		ActiveNodeID:  doc.ActiveNodeID,
		Nodes:         nodes,
		Subscriptions: subscriptions,
		Tun:           tun,
		Inbounds:      inbounds,
		Extra:         cloneRaw(doc.Extra),
	}
}

func routingFromConfig(cfg *config.Config) RoutingConfig {
	directDomains, directIps := splitRoutingRuleInputs(cfg.Routing.CustomDirect)
	proxyDomains, proxyIps := splitRoutingRuleInputs(cfg.Routing.CustomProxy)
	blockDomains, blockIps := splitRoutingRuleInputs(cfg.Routing.CustomBlock)
	routing := RoutingConfig{
		AppGroupRoutes:              map[string]string{},
		AppProxyList:                append([]string(nil), cfg.Routing.InactiveAppProxyList...),
		AppBypassList:               append([]string(nil), cfg.Routing.InactiveAppBypassList...),
		DirectDomains:               directDomains,
		ProxyDomains:                proxyDomains,
		BlockDomains:                blockDomains,
		DirectIps:                   directIps,
		ProxyIps:                    proxyIps,
		BlockIps:                    blockIps,
		AlwaysDirectAppList:         append([]string(nil), cfg.Routing.AlwaysDirectApps...),
		AlwaysDirectExcludedAppList: append([]string(nil), cfg.Routing.AlwaysDirectExcludedApps...),
		AlwaysDirectSystemApps:      cfg.Routing.AlwaysDirectSystemApps,
		BypassRussia:                cfg.Routing.BypassRussia,
	}
	for key, value := range cfg.Apps.AppGroups {
		routing.AppGroupRoutes[key] = value
	}
	switch cfg.Routing.Mode {
	case "all":
		routing.Mode = "PROXY_ALL"
	case "blacklist":
		routing.Mode = "PER_APP_BYPASS"
		routing.AppBypassList = append([]string(nil), cfg.Apps.Packages...)
	case "direct":
		routing.Mode = "DIRECT"
	case "rules":
		routing.Mode = "RULES"
		routing.AppProxyList = append([]string(nil), cfg.Apps.Packages...)
	default:
		routing.Mode = "PER_APP"
		routing.AppProxyList = append([]string(nil), cfg.Apps.Packages...)
	}
	return routing
}

func applyRoutingToConfig(cfg *config.Config, routing RoutingConfig) {
	cfg.Routing.CustomDirect = mergeRoutingRuleInputs(routing.DirectDomains, routing.DirectIps)
	cfg.Routing.CustomProxy = mergeRoutingRuleInputs(routing.ProxyDomains, routing.ProxyIps)
	cfg.Routing.CustomBlock = mergeRoutingRuleInputs(routing.BlockDomains, routing.BlockIps)
	cfg.Routing.AlwaysDirectApps = append([]string(nil), routing.AlwaysDirectAppList...)
	cfg.Routing.AlwaysDirectExcludedApps = append([]string(nil), routing.AlwaysDirectExcludedAppList...)
	cfg.Routing.AlwaysDirectSystemApps = routing.AlwaysDirectSystemApps
	cfg.Routing.BypassRussia = routing.BypassRussia
	cfg.Routing.InactiveAppProxyList = append([]string(nil), routing.AppProxyList...)
	cfg.Routing.InactiveAppBypassList = append([]string(nil), routing.AppBypassList...)
	cfg.Apps.AppGroups = map[string]string{}
	for key, value := range routing.AppGroupRoutes {
		cfg.Apps.AppGroups[key] = value
	}
	switch routing.Mode {
	case "PROXY_ALL":
		cfg.Routing.Mode = "all"
		cfg.Apps.Mode = "all"
		cfg.Apps.Packages = nil
	case "PER_APP_BYPASS":
		cfg.Routing.Mode = "blacklist"
		cfg.Apps.Mode = "blacklist"
		cfg.Apps.Packages = append([]string(nil), routing.AppBypassList...)
	case "DIRECT":
		cfg.Routing.Mode = "direct"
		cfg.Apps.Mode = "off"
		cfg.Apps.Packages = nil
	case "RULES":
		cfg.Routing.Mode = "rules"
		cfg.Apps.Mode = "all"
		cfg.Apps.Packages = append([]string(nil), routing.AppProxyList...)
	default:
		cfg.Routing.Mode = "whitelist"
		cfg.Apps.Mode = "whitelist"
		cfg.Apps.Packages = append([]string(nil), routing.AppProxyList...)
	}
}

func splitRoutingRuleInputs(values []string) (domains []string, ips []string) {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if strings.Contains(value, "/") {
			ips = append(ips, value)
		} else {
			domains = append(domains, value)
		}
	}
	return domains, ips
}

func mergeRoutingRuleInputs(domains []string, ips []string) []string {
	result := make([]string, 0, len(domains)+len(ips))
	result = appendTrimmedRoutingRules(result, domains)
	result = appendTrimmedRoutingRules(result, ips)
	return result
}

func appendTrimmedRoutingRules(dst []string, values []string) []string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			dst = append(dst, value)
		}
	}
	return dst
}

func dnsFromConfig(cfg *config.Config) DNSConfig {
	return DNSConfig{
		RemoteDNS:   cfg.DNS.ProxyDNS,
		DirectDNS:   cfg.DNS.DirectDNS,
		BootstrapIP: cfg.DNS.BootstrapIP,
		IPv6Mode:    strings.ToUpper(cfg.IPv6.Mode),
		FakeDNS:     cfg.DNS.FakeIP,
	}
}

func applyDNSToConfig(cfg *config.Config, dns DNSConfig) {
	cfg.DNS.ProxyDNS = dns.RemoteDNS
	cfg.DNS.DirectDNS = dns.DirectDNS
	cfg.DNS.BootstrapIP = dns.BootstrapIP
	cfg.DNS.FakeIP = dns.FakeDNS
	cfg.IPv6.Mode = strings.ToLower(dns.IPv6Mode)
	if cfg.IPv6.Mode == "" {
		cfg.IPv6.Mode = "mirror"
	}
}

func healthFromConfig(cfg *config.Config) HealthConfig {
	return HealthConfig{
		Enabled:            cfg.Health.Enabled,
		IntervalSec:        cfg.Health.IntervalSec,
		Threshold:          cfg.Health.Threshold,
		CheckURL:           cfg.Health.URL,
		TimeoutSec:         cfg.Health.TimeoutSec,
		DNSProbeDomains:    append([]string(nil), cfg.Health.DNSProbeDomains...),
		EgressURLs:         append([]string(nil), cfg.Health.EgressURLs...),
		DNSIsHardReadiness: cfg.Health.DNSIsHardReadiness,
	}
}

func decodeNodes(rawNodes []json.RawMessage) []Node {
	nodes := make([]Node, 0, len(rawNodes))
	for _, raw := range rawNodes {
		var node Node
		if err := json.Unmarshal(raw, &node); err == nil {
			nodes = append(nodes, node)
		}
	}
	return nodes
}

func decodeSubscriptions(rawSubs []json.RawMessage) []Subscription {
	subs := make([]Subscription, 0, len(rawSubs))
	for _, raw := range rawSubs {
		var sub Subscription
		if err := json.Unmarshal(raw, &sub); err == nil {
			subs = append(subs, sub)
		}
	}
	return subs
}

func decodeTun(raw json.RawMessage) TunConfig {
	tun := TunConfig{MTU: 9000, IPv4Address: "172.19.0.1/30", AutoRoute: true, StrictRoute: true}
	_ = json.Unmarshal(raw, &tun)
	return tun
}

func decodeInbounds(raw json.RawMessage) InboundsConfig {
	var inbounds InboundsConfig
	_ = json.Unmarshal(raw, &inbounds)
	inbounds.AllowLAN = false
	return inbounds
}

func NodeMatchKey(node Node) string {
	sourceType := strings.ToUpper(strings.TrimSpace(node.Source.Type))
	if sourceType == "" {
		sourceType = "MANUAL"
	}
	sourceScope := sourceType
	if sourceType == "SUBSCRIPTION" {
		sourceScope += ":" + strings.TrimSpace(node.Source.ProviderKey)
	}
	id := strings.TrimSpace(node.ID)
	if id != "" {
		return sourceScope + "|id:" + id
	}
	server := strings.ToLower(strings.TrimSpace(node.Server))
	if server == "" || node.Port == 0 {
		return ""
	}
	return strings.Join([]string{sourceScope, normalizeProtocol(node.Protocol), server, strconv.Itoa(node.Port)}, "|")
}

func nodeByID(nodes []Node, id string) *Node {
	for i := range nodes {
		if nodes[i].ID == id {
			return &nodes[i]
		}
	}
	return nil
}

func validatePort(name string, port int, zeroAllowed bool) error {
	if zeroAllowed && port == 0 {
		return nil
	}
	if port < 1 || port > 65535 {
		return fmt.Errorf("%s must be 1-65535, got %d", name, port)
	}
	return nil
}

func normalizeProtocol(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "vless", "vmess", "trojan", "socks", "hysteria2", "tuic", "wireguard":
		return strings.ToLower(strings.TrimSpace(value))
	case "ss", "shadowsocks":
		return "shadowsocks"
	case "socks4", "socks4a", "socks5":
		return "socks"
	case "hy2":
		return "hysteria2"
	case "wg":
		return "wireguard"
	default:
		return strings.ToLower(strings.TrimSpace(value))
	}
}

func isValidAndroidPackageName(value string) bool {
	parts := strings.Split(value, ".")
	if len(parts) < 2 {
		return false
	}
	for _, part := range parts {
		if part == "" {
			return false
		}
		for i, r := range part {
			if i == 0 {
				if !((r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || r == '_') {
					return false
				}
				continue
			}
			if !((r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_') {
				return false
			}
		}
	}
	return true
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func cloneRaw(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	return append(json.RawMessage(nil), raw...)
}

func HostIsLocal(host string) bool {
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified()
	}
	return strings.EqualFold(host, "localhost")
}

func IsDisallowedSubscriptionEndpoint(host string) bool {
	host = strings.Trim(strings.ToLower(strings.TrimSpace(host)), "[]")
	host = strings.TrimSuffix(host, ".")
	if host == "" || isLocalSubscriptionHostname(host) {
		return true
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	addr, ok := netip.AddrFromSlice(ip)
	if !ok {
		return true
	}
	addr = addr.Unmap()
	if addr.Is4() {
		ip = net.IP(addr.AsSlice()).To4()
	} else {
		ip = net.IP(addr.AsSlice())
	}
	if ip.IsUnspecified() ||
		ip.IsLoopback() ||
		ip.IsPrivate() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast() ||
		ip.IsMulticast() {
		return true
	}
	return isReservedSubscriptionAddr(addr)
}

func isLocalSubscriptionHostname(host string) bool {
	if host == "localhost" || host == "ip6-localhost" {
		return true
	}
	for _, suffix := range localSubscriptionHostSuffixes {
		if host == strings.TrimPrefix(suffix, ".") || strings.HasSuffix(host, suffix) {
			return true
		}
	}
	return false
}

func isReservedSubscriptionAddr(addr netip.Addr) bool {
	for _, prefix := range reservedSubscriptionPrefixes {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}

var localSubscriptionHostSuffixes = []string{
	".localhost",
	".local",
	".localdomain",
	".lan",
	".home.arpa",
}

var reservedSubscriptionPrefixes = mustParsePrefixes([]string{
	"0.0.0.0/8",
	"100.64.0.0/10",
	"192.0.0.0/24",
	"192.0.2.0/24",
	"192.88.99.0/24",
	"198.18.0.0/15",
	"198.51.100.0/24",
	"203.0.113.0/24",
	"224.0.0.0/4",
	"240.0.0.0/4",
	"::/128",
	"::1/128",
	"64:ff9b::/96",
	"100::/64",
	"2001:db8::/32",
	"fc00::/7",
	"fe80::/10",
	"ff00::/8",
})

func mustParsePrefixes(values []string) []netip.Prefix {
	result := make([]netip.Prefix, 0, len(values))
	for _, value := range values {
		prefix, err := netip.ParsePrefix(value)
		if err != nil {
			panic(err)
		}
		result = append(result, prefix)
	}
	return result
}
