package subscription

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	neturl "net/url"
	"strconv"
	"strings"

	profiledoc "github.com/youtubediscord/RKNnoVPN/daemon/internal/profile"
)

func ParseSubscription(body string, headers map[string]string, source SubscriptionSource, nowMillis int64) ([]profiledoc.Node, profiledoc.Subscription, int, []RejectedNode) {
	if source.ProviderKey == "" {
		source.ProviderKey = providerKeyFor(source.URL)
	}
	text := decodeSubscriptionBody(body)
	nodes := make([]profiledoc.Node, 0)
	rejected := make([]RejectedNode, 0)
	failures := 0
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		node, err := ParseLink(line, nowMillis)
		if err != nil {
			failures++
			continue
		}
		if profiledoc.IsDisallowedSubscriptionEndpoint(node.Server) {
			rejected = append(rejected, RejectedNode{
				Link:     line,
				Name:     node.Name,
				Protocol: node.Protocol,
				Server:   node.Server,
				Port:     node.Port,
				Code:     "subscription_local_endpoint",
				Reason:   fmt.Sprintf("subscription node %s:%d points to a local, private, or reserved endpoint", node.Server, node.Port),
			})
			continue
		}
		node.Source = source.NodeSource(nowMillis)
		nodes = append(nodes, node)
	}
	info := subscriptionInfoFromHeaders(headers)
	sub := profiledoc.Subscription{
		ProviderKey:       source.ProviderKey,
		URL:               source.URL,
		LastFetchedAt:     nowMillis,
		LastSeenNodeCount: len(nodes),
		UploadBytes:       info.UploadBytes,
		DownloadBytes:     info.DownloadBytes,
		TotalBytes:        info.TotalBytes,
		ExpireTimestamp:   info.ExpireTimestamp,
		ParseFailures:     failures,
	}
	return nodes, sub, failures, rejected
}

type subscriptionInfo struct {
	UploadBytes     int64
	DownloadBytes   int64
	TotalBytes      int64
	ExpireTimestamp int64
}

func subscriptionInfoFromHeaders(headers map[string]string) subscriptionInfo {
	var result subscriptionInfo
	for key, value := range headers {
		if !strings.EqualFold(key, "subscription-userinfo") {
			continue
		}
		for _, part := range strings.Split(value, ";") {
			k, v, ok := strings.Cut(strings.TrimSpace(part), "=")
			if !ok {
				continue
			}
			n, _ := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
			switch strings.ToLower(strings.TrimSpace(k)) {
			case "upload":
				result.UploadBytes = n
			case "download":
				result.DownloadBytes = n
			case "total":
				result.TotalBytes = n
			case "expire":
				result.ExpireTimestamp = n
			}
		}
	}
	return result
}

func ParseLink(raw string, nowMillis int64) (profiledoc.Node, error) {
	parsed, err := neturl.Parse(strings.TrimSpace(raw))
	if err != nil {
		return profiledoc.Node{}, err
	}
	proto := normalizeLinkProtocol(parsed.Scheme)
	if proto == "" {
		return profiledoc.Node{}, fmt.Errorf("unsupported scheme")
	}
	host := parsed.Hostname()
	port, _ := strconv.Atoi(parsed.Port())
	if host == "" || port <= 0 || port > 65535 {
		return profiledoc.Node{}, fmt.Errorf("missing host or port")
	}
	name, _ := neturl.QueryUnescape(parsed.Fragment)
	if name == "" {
		name = host
	}
	node := profiledoc.Node{
		ID:        stableNodeID(proto, host, port, parsed.User.String()),
		Name:      name,
		Protocol:  proto,
		Server:    host,
		Port:      port,
		Link:      raw,
		Group:     "Default",
		CreatedAt: nowMillis,
		Source:    profiledoc.NodeSource{Type: "MANUAL"},
	}
	node.Outbound = buildOutbound(node, parsed)
	return node, nil
}

func buildOutbound(node profiledoc.Node, parsed *neturl.URL) json.RawMessage {
	settings := map[string]interface{}{}
	userSecret := ""
	if parsed.User != nil {
		userSecret = parsed.User.Username()
	}
	switch node.Protocol {
	case "vless", "vmess":
		user := map[string]interface{}{"id": userSecret}
		if flow := parsed.Query().Get("flow"); flow != "" {
			user["flow"] = flow
		}
		settings["vnext"] = []interface{}{map[string]interface{}{
			"address": node.Server,
			"port":    node.Port,
			"users":   []interface{}{user},
		}}
	case "trojan", "shadowsocks":
		server := map[string]interface{}{
			"address":  node.Server,
			"port":     node.Port,
			"password": userSecret,
		}
		if node.Protocol == "shadowsocks" {
			method := parsed.Query().Get("method")
			if method == "" && strings.Contains(userSecret, ":") {
				method, userSecret, _ = strings.Cut(userSecret, ":")
				server["password"] = userSecret
			}
			if method == "" {
				method = "aes-128-gcm"
			}
			server["method"] = method
		}
		settings["servers"] = []interface{}{server}
	case "socks":
		settings["address"] = node.Server
		settings["port"] = node.Port
		settings["version"] = "5"
		if parsed.User != nil {
			settings["username"] = parsed.User.Username()
			if password, ok := parsed.User.Password(); ok {
				settings["password"] = password
			}
		}
	}
	outbound := map[string]interface{}{
		"protocol": node.Protocol,
		"settings": settings,
	}
	if security := parsed.Query().Get("security"); security != "" {
		stream := map[string]interface{}{"security": security}
		if sni := parsed.Query().Get("sni"); sni != "" {
			stream["tlsSettings"] = map[string]interface{}{"serverName": sni}
		}
		outbound["streamSettings"] = stream
	}
	raw, _ := json.Marshal(outbound)
	return raw
}

func decodeSubscriptionBody(body string) string {
	trimmed := strings.TrimSpace(body)
	if strings.Contains(trimmed, "://") {
		return trimmed
	}
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		if decoded, err := enc.DecodeString(trimmed); err == nil && strings.Contains(string(decoded), "://") {
			return string(decoded)
		}
	}
	return trimmed
}

func normalizeLinkProtocol(value string) string {
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

func stableNodeID(protocol, host string, port int, secret string) string {
	clean := strings.ToLower(strings.TrimSpace(protocol + "-" + host + "-" + strconv.Itoa(port)))
	clean = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			return r
		}
		return '-'
	}, clean)
	clean = strings.Trim(clean, "-")
	if clean == "" {
		clean = "node"
	}
	if secret != "" {
		sum := 0
		for _, r := range secret {
			sum = (sum*31 + int(r)) % 100000
		}
		clean = fmt.Sprintf("%s-%05d", clean, sum)
	}
	return clean
}
