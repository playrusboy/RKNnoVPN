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
	raw = strings.TrimSpace(raw)
	parsed, err := neturl.Parse(raw)
	if err != nil {
		return profiledoc.Node{}, err
	}
	proto := normalizeLinkProtocol(parsed.Scheme)
	if proto == "" {
		return profiledoc.Node{}, fmt.Errorf("unsupported scheme")
	}
	if proto == "shadowsocks" {
		return parseShadowsocksNode(raw, parsed, nowMillis)
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

func parseShadowsocksNode(raw string, parsed *neturl.URL, nowMillis int64) (profiledoc.Node, error) {
	schemeSep := strings.Index(raw, "://")
	if schemeSep < 0 {
		return profiledoc.Node{}, fmt.Errorf("unsupported shadowsocks uri")
	}
	body := strings.SplitN(raw[schemeSep+3:], "#", 2)[0]

	var method, password, host, plugin, pluginOpts string
	var port int
	if at := strings.LastIndex(body, "@"); at >= 0 {
		userInfo := body[:at]
		hostAndQuery := body[at+1:]
		hostPort := strings.TrimSuffix(strings.SplitN(hostAndQuery, "?", 2)[0], "/")
		var query neturl.Values
		if queryIndex := strings.Index(hostAndQuery, "?"); queryIndex >= 0 {
			query, _ = neturl.ParseQuery(hostAndQuery[queryIndex+1:])
			plugin, pluginOpts = splitShadowsocksPlugin(query.Get("plugin"))
		}
		parsedHost, parsedPort, ok := parseShadowsocksHostPort(hostPort)
		if !ok {
			return profiledoc.Node{}, fmt.Errorf("missing host or port")
		}
		parsedMethod, parsedPassword, ok := parseShadowsocksUserInfo(userInfo)
		if !ok {
			parsedPassword, _ = neturl.QueryUnescape(userInfo)
			parsedMethod = normalizeShadowsocksMethod(query.Get("method"))
			if parsedMethod == "" {
				parsedMethod = "aes-128-gcm"
			}
			if parsedPassword == "" {
				return profiledoc.Node{}, fmt.Errorf("invalid shadowsocks credentials")
			}
		}
		method, password, host, port = parsedMethod, parsedPassword, parsedHost, parsedPort
	} else {
		decoded := decodeShadowsocksUserInfo(strings.SplitN(body, "?", 2)[0])
		if decoded == "" {
			return profiledoc.Node{}, fmt.Errorf("invalid shadowsocks legacy credentials")
		}
		at := strings.LastIndex(decoded, "@")
		if at < 0 {
			return profiledoc.Node{}, fmt.Errorf("invalid shadowsocks legacy endpoint")
		}
		parsedMethod, parsedPassword, ok := splitShadowsocksMethodPassword(decoded[:at])
		if !ok {
			return profiledoc.Node{}, fmt.Errorf("invalid shadowsocks legacy credentials")
		}
		parsedHost, parsedPort, ok := parseShadowsocksHostPort(decoded[at+1:])
		if !ok {
			return profiledoc.Node{}, fmt.Errorf("missing host or port")
		}
		method, password, host, port = parsedMethod, parsedPassword, parsedHost, parsedPort
	}

	name, _ := neturl.QueryUnescape(parsed.Fragment)
	if name == "" {
		name = host
	}
	node := profiledoc.Node{
		ID:        stableNodeID("shadowsocks", host, port, method+":"+password),
		Name:      name,
		Protocol:  "shadowsocks",
		Server:    host,
		Port:      port,
		Link:      raw,
		Group:     "Default",
		CreatedAt: nowMillis,
		Source:    profiledoc.NodeSource{Type: "MANUAL"},
	}
	node.Outbound = buildShadowsocksOutbound(host, port, method, password, plugin, pluginOpts)
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
		if node.Protocol == "vless" {
			user["encryption"] = valueOrDefault(strings.TrimSpace(parsed.Query().Get("encryption")), "none")
		}
		if flow := parsed.Query().Get("flow"); flow != "" {
			user["flow"] = flow
		}
		settings["vnext"] = []interface{}{map[string]interface{}{
			"address": node.Server,
			"port":    node.Port,
			"users":   []interface{}{user},
		}}
	case "trojan":
		server := map[string]interface{}{
			"address":  node.Server,
			"port":     node.Port,
			"password": userSecret,
		}
		settings["servers"] = []interface{}{server}
	case "shadowsocks":
		method, password := parseShadowsocksCredentials(parsed)
		plugin, pluginOpts := splitShadowsocksPlugin(parsed.Query().Get("plugin"))
		settings["servers"] = []interface{}{shadowsocksServerSettings(node.Server, node.Port, method, password, plugin, pluginOpts)}
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
	if stream := buildStreamSettings(parsed.Query()); len(stream) > 0 {
		outbound["streamSettings"] = stream
	}
	raw, _ := json.Marshal(outbound)
	return raw
}

func buildShadowsocksOutbound(host string, port int, method string, password string, plugin string, pluginOpts string) json.RawMessage {
	outbound := map[string]interface{}{
		"protocol": "shadowsocks",
		"settings": map[string]interface{}{
			"servers": []interface{}{shadowsocksServerSettings(host, port, method, password, plugin, pluginOpts)},
		},
	}
	raw, _ := json.Marshal(outbound)
	return raw
}

func shadowsocksServerSettings(host string, port int, method string, password string, plugin string, pluginOpts string) map[string]interface{} {
	server := map[string]interface{}{
		"address":  host,
		"port":     port,
		"method":   method,
		"password": password,
	}
	if plugin != "" {
		server["plugin"] = plugin
	}
	if pluginOpts != "" {
		server["plugin_opts"] = pluginOpts
	}
	return server
}

func buildStreamSettings(query neturl.Values) map[string]interface{} {
	stream := map[string]interface{}{}
	network := strings.ToLower(strings.TrimSpace(query.Get("type")))
	if network != "" {
		stream["network"] = network
	}
	security := strings.ToLower(strings.TrimSpace(query.Get("security")))
	if security != "" {
		stream["security"] = security
	}
	tlsSettings := map[string]interface{}{}
	if sni := strings.TrimSpace(firstQuery(query, "sni", "servername")); sni != "" {
		tlsSettings["serverName"] = sni
	}
	if fp := strings.TrimSpace(firstQuery(query, "fp", "fingerprint")); fp != "" {
		tlsSettings["fingerprint"] = fp
	}
	switch security {
	case "reality":
		if pbk := strings.TrimSpace(firstQuery(query, "pbk", "publicKey")); pbk != "" {
			tlsSettings["publicKey"] = pbk
		}
		if sid := strings.TrimSpace(firstQuery(query, "sid", "shortId")); sid != "" {
			tlsSettings["shortId"] = sid
		}
		if spx := strings.TrimSpace(firstQuery(query, "spx", "spiderX")); spx != "" {
			tlsSettings["spiderX"] = spx
		}
		stream["realitySettings"] = tlsSettings
	case "tls":
		if len(tlsSettings) > 0 {
			stream["tlsSettings"] = tlsSettings
		}
	}
	switch network {
	case "ws":
		ws := map[string]interface{}{}
		if path := strings.TrimSpace(query.Get("path")); path != "" {
			ws["path"] = path
		} else {
			ws["path"] = "/"
		}
		if host := strings.TrimSpace(query.Get("host")); host != "" {
			ws["headers"] = map[string]interface{}{"Host": host}
		}
		stream["wsSettings"] = ws
	case "grpc":
		grpc := map[string]interface{}{}
		if serviceName := strings.TrimSpace(firstQuery(query, "serviceName", "service_name")); serviceName != "" {
			grpc["serviceName"] = serviceName
		}
		if mode := strings.TrimSpace(query.Get("mode")); mode != "" {
			grpc["mode"] = mode
		}
		if authority := strings.TrimSpace(query.Get("authority")); authority != "" {
			grpc["authority"] = authority
		}
		stream["grpcSettings"] = grpc
	case "http", "h2":
		httpSettings := map[string]interface{}{}
		if path := strings.TrimSpace(query.Get("path")); path != "" {
			httpSettings["path"] = path
		} else {
			httpSettings["path"] = "/"
		}
		if host := strings.TrimSpace(query.Get("host")); host != "" {
			httpSettings["host"] = splitCSV(host)
		}
		stream["httpSettings"] = httpSettings
	case "httpupgrade":
		httpUpgrade := map[string]interface{}{}
		if path := strings.TrimSpace(query.Get("path")); path != "" {
			httpUpgrade["path"] = path
		} else {
			httpUpgrade["path"] = "/"
		}
		if host := strings.TrimSpace(query.Get("host")); host != "" {
			httpUpgrade["host"] = host
		}
		stream["httpupgradeSettings"] = httpUpgrade
	case "xhttp":
		xhttp := map[string]interface{}{}
		if path := strings.TrimSpace(query.Get("path")); path != "" {
			xhttp["path"] = path
		} else {
			xhttp["path"] = "/"
		}
		if host := strings.TrimSpace(query.Get("host")); host != "" {
			xhttp["host"] = host
		}
		if mode := strings.TrimSpace(query.Get("mode")); mode != "" {
			xhttp["mode"] = mode
		}
		if extra := strings.TrimSpace(query.Get("extra")); extra != "" {
			var value interface{}
			if err := json.Unmarshal([]byte(extra), &value); err == nil {
				xhttp["extra"] = value
			}
		}
		stream["xhttpSettings"] = xhttp
	}
	return stream
}

func splitCSV(value string) []string {
	result := []string{}
	for _, item := range strings.Split(value, ",") {
		item = strings.TrimSpace(item)
		if item != "" {
			result = append(result, item)
		}
	}
	return result
}

func valueOrDefault(value string, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}

func parseShadowsocksCredentials(parsed *neturl.URL) (string, string) {
	if parsed.User == nil {
		return "aes-128-gcm", ""
	}
	userInfo := parsed.User.String()
	if decoded := decodeShadowsocksUserInfo(userInfo); decoded != "" {
		if method, password, ok := splitSupportedShadowsocksMethodPassword(decoded); ok {
			return method, password
		}
	}
	username := parsed.User.Username()
	if password, ok := parsed.User.Password(); ok {
		return normalizeShadowsocksMethod(username), password
	}
	if method, password, ok := splitShadowsocksMethodPassword(username); ok {
		return method, password
	}
	if method := normalizeShadowsocksMethod(parsed.Query().Get("method")); method != "" {
		return method, username
	}
	return "aes-128-gcm", username
}

func parseShadowsocksUserInfo(userInfo string) (string, string, bool) {
	if decoded := decodeShadowsocksUserInfo(userInfo); decoded != "" {
		if method, password, ok := splitSupportedShadowsocksMethodPassword(decoded); ok {
			return method, password, true
		}
	}
	decoded, _ := neturl.QueryUnescape(userInfo)
	return splitShadowsocksMethodPassword(decoded)
}

func decodeShadowsocksUserInfo(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	for _, enc := range []*base64.Encoding{base64.RawURLEncoding, base64.URLEncoding, base64.RawStdEncoding, base64.StdEncoding} {
		if decoded, err := enc.DecodeString(value); err == nil {
			return string(decoded)
		}
	}
	return ""
}

func splitSupportedShadowsocksMethodPassword(value string) (string, string, bool) {
	method, password, ok := splitShadowsocksMethodPassword(value)
	if !ok || !isSupportedShadowsocksMethod(method) {
		return "", "", false
	}
	return method, password, true
}

func splitShadowsocksMethodPassword(value string) (string, string, bool) {
	method, password, ok := strings.Cut(value, ":")
	method = normalizeShadowsocksMethod(method)
	if !ok || method == "" || password == "" {
		return "", "", false
	}
	return method, password, true
}

func splitShadowsocksPlugin(raw string) (string, string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", ""
	}
	plugin, opts, found := strings.Cut(raw, ";")
	if !found {
		return strings.TrimSpace(plugin), ""
	}
	return strings.TrimSpace(plugin), opts
}

func parseShadowsocksHostPort(value string) (string, int, bool) {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "[") {
		closeBracket := strings.Index(value, "]")
		if closeBracket < 0 {
			return "", 0, false
		}
		host := value[1:closeBracket]
		portValue := strings.TrimPrefix(value[closeBracket+1:], ":")
		port, err := strconv.Atoi(portValue)
		return host, port, host != "" && err == nil && port > 0 && port <= 65535
	}
	host, portRaw, ok := strings.Cut(value, ":")
	if !ok {
		return "", 0, false
	}
	port, err := strconv.Atoi(portRaw)
	return host, port, host != "" && err == nil && port > 0 && port <= 65535
}

func normalizeShadowsocksMethod(method string) string {
	return strings.ToLower(strings.TrimSpace(method))
}

func isSupportedShadowsocksMethod(method string) bool {
	switch normalizeShadowsocksMethod(method) {
	case "2022-blake3-aes-128-gcm",
		"2022-blake3-aes-256-gcm",
		"2022-blake3-chacha20-poly1305",
		"none",
		"aes-128-gcm",
		"aes-192-gcm",
		"aes-256-gcm",
		"chacha20-ietf-poly1305",
		"xchacha20-ietf-poly1305",
		"aes-128-ctr",
		"aes-192-ctr",
		"aes-256-ctr",
		"aes-128-cfb",
		"aes-192-cfb",
		"aes-256-cfb",
		"rc4-md5",
		"chacha20-ietf",
		"xchacha20":
		return true
	default:
		return false
	}
}

func firstQuery(query neturl.Values, keys ...string) string {
	for _, key := range keys {
		if value := query.Get(key); value != "" {
			return value
		}
	}
	return ""
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
