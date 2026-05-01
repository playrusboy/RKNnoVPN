package config

import (
	"encoding/json"
	"fmt"
	"strings"
)

func RequiresXraySidecar(profile *NodeProfile) bool {
	return profile != nil &&
		strings.EqualFold(strings.TrimSpace(profile.Protocol), "vless") &&
		strings.EqualFold(strings.TrimSpace(profile.Transport), "xhttp")
}

func RenderXraySidecarConfig(profile *NodeProfile, socksPort int) ([]byte, error) {
	if !RequiresXraySidecar(profile) {
		return nil, fmt.Errorf("xray sidecar requires VLESS over XHTTP")
	}
	if socksPort <= 0 || socksPort > 65535 {
		return nil, fmt.Errorf("xray sidecar socks port %d is invalid", socksPort)
	}
	outbound, err := xrayOutbound(profile)
	if err != nil {
		return nil, err
	}
	outbound["tag"] = "proxy"
	doc := map[string]interface{}{
		"log": map[string]interface{}{
			"loglevel": "warning",
		},
		"inbounds": []interface{}{
			map[string]interface{}{
				"tag":      "rknnovpn-xhttp-in",
				"listen":   "127.0.0.1",
				"port":     socksPort,
				"protocol": "socks",
				"settings": map[string]interface{}{
					"auth": "noauth",
					"udp":  true,
				},
			},
		},
		"outbounds": []interface{}{outbound},
	}
	return json.MarshalIndent(doc, "", "  ")
}

func xrayOutbound(profile *NodeProfile) (map[string]interface{}, error) {
	if len(profile.RawOutbound) > 0 {
		var outbound map[string]interface{}
		if err := json.Unmarshal(profile.RawOutbound, &outbound); err != nil {
			return nil, fmt.Errorf("xray sidecar: parse stored outbound: %w", err)
		}
		if !streamUsesXHTTP(outbound) {
			return nil, fmt.Errorf("xray sidecar: stored outbound is not XHTTP")
		}
		ensureVLESSUsersEncryption(outbound)
		stripXHTTPVisionFlow(outbound)
		return outbound, nil
	}

	if profile.Address == "" || profile.Port <= 0 {
		return nil, fmt.Errorf("xray sidecar: VLESS server address/port are required")
	}
	if profile.UUID == "" {
		return nil, fmt.Errorf("xray sidecar: VLESS uuid is empty")
	}
	user := map[string]interface{}{
		"id":         profile.UUID,
		"encryption": "none",
	}
	if profile.Flow != "" && !strings.EqualFold(strings.TrimSpace(profile.Transport), "xhttp") {
		user["flow"] = profile.Flow
	}
	stream := map[string]interface{}{
		"network":       "xhttp",
		"xhttpSettings": buildXHTTPSettings(profile),
	}
	security := strings.TrimSpace(profile.Extra["security"])
	if security == "" && profile.RealityPubKey != "" {
		security = "reality"
	}
	if security != "" {
		stream["security"] = security
	}
	if security == "reality" || profile.RealityPubKey != "" {
		reality := map[string]interface{}{}
		if profile.TLSServer != "" {
			reality["serverName"] = profile.TLSServer
		}
		if profile.Fingerprint != "" {
			reality["fingerprint"] = profile.Fingerprint
		}
		if profile.RealityPubKey != "" {
			reality["publicKey"] = profile.RealityPubKey
		}
		if profile.RealityShortID != "" {
			reality["shortId"] = profile.RealityShortID
		}
		stream["realitySettings"] = reality
	} else if security == "tls" {
		tls := map[string]interface{}{}
		if profile.TLSServer != "" {
			tls["serverName"] = profile.TLSServer
		}
		if profile.Fingerprint != "" {
			tls["fingerprint"] = profile.Fingerprint
		}
		stream["tlsSettings"] = tls
	}
	return map[string]interface{}{
		"protocol": "vless",
		"settings": map[string]interface{}{
			"vnext": []interface{}{
				map[string]interface{}{
					"address": profile.Address,
					"port":    profile.Port,
					"users":   []interface{}{user},
				},
			},
		},
		"streamSettings": stream,
	}, nil
}

func ensureVLESSUsersEncryption(outbound map[string]interface{}) {
	if outbound == nil {
		return
	}
	protocol, _ := outbound["protocol"].(string)
	if !strings.EqualFold(strings.TrimSpace(protocol), "vless") {
		return
	}
	settings, ok := outbound["settings"].(map[string]interface{})
	if !ok {
		return
	}
	vnext, ok := settings["vnext"].([]interface{})
	if !ok {
		return
	}
	for _, rawServer := range vnext {
		server, ok := rawServer.(map[string]interface{})
		if !ok {
			continue
		}
		users, ok := server["users"].([]interface{})
		if !ok {
			continue
		}
		for _, rawUser := range users {
			user, ok := rawUser.(map[string]interface{})
			if !ok {
				continue
			}
			if strings.TrimSpace(stringFromMap(user, "encryption")) == "" {
				user["encryption"] = "none"
			}
		}
	}
}

func streamUsesXHTTP(outbound map[string]interface{}) bool {
	stream, ok := outbound["streamSettings"].(map[string]interface{})
	if !ok {
		return false
	}
	network, _ := stream["network"].(string)
	return strings.EqualFold(strings.TrimSpace(network), "xhttp")
}

func stripXHTTPVisionFlow(outbound map[string]interface{}) {
	if outbound == nil {
		return
	}
	protocol, _ := outbound["protocol"].(string)
	if !strings.EqualFold(strings.TrimSpace(protocol), "vless") {
		return
	}
	settings, ok := outbound["settings"].(map[string]interface{})
	if !ok {
		return
	}
	vnext, ok := settings["vnext"].([]interface{})
	if !ok {
		return
	}
	for _, rawServer := range vnext {
		server, ok := rawServer.(map[string]interface{})
		if !ok {
			continue
		}
		users, ok := server["users"].([]interface{})
		if !ok {
			continue
		}
		for _, rawUser := range users {
			user, ok := rawUser.(map[string]interface{})
			if !ok {
				continue
			}
			flow, _ := user["flow"].(string)
			if strings.HasPrefix(strings.ToLower(strings.TrimSpace(flow)), "xtls-rprx-vision") {
				delete(user, "flow")
			}
		}
	}
}

func buildXHTTPSettings(profile *NodeProfile) map[string]interface{} {
	settings := map[string]interface{}{
		"path": firstNonEmpty(profile.Extra["path"], "/"),
	}
	if host := strings.TrimSpace(profile.Extra["host"]); host != "" {
		settings["host"] = host
	}
	if mode := strings.TrimSpace(profile.Extra["mode"]); mode != "" {
		settings["mode"] = mode
	}
	return settings
}
