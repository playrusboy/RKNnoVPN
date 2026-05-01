package config

import (
	"encoding/json"
	"testing"
)

func TestRenderXraySidecarConfigAddsMissingVLESSEncryption(t *testing.T) {
	profile := &NodeProfile{
		Protocol:  "vless",
		Address:   "example.com",
		Port:      443,
		UUID:      "00000000-0000-0000-0000-000000000000",
		Transport: "xhttp",
		RawOutbound: json.RawMessage(`{
			"protocol":"vless",
			"settings":{
				"vnext":[{
					"address":"example.com",
					"port":443,
					"users":[{
						"id":"00000000-0000-0000-0000-000000000000"
					}]
				}]
			},
			"streamSettings":{
				"network":"xhttp",
				"security":"reality",
				"realitySettings":{"publicKey":"public-key"},
				"xhttpSettings":{"path":"/api","mode":"auto"}
			}
		}`),
	}

	data, err := RenderXraySidecarConfig(profile, XraySidecarSocksPort)
	if err != nil {
		t.Fatalf("render xray sidecar: %v", err)
	}
	var rendered map[string]any
	if err := json.Unmarshal(data, &rendered); err != nil {
		t.Fatalf("unmarshal rendered config: %v", err)
	}
	outbound := rendered["outbounds"].([]any)[0].(map[string]any)
	settings := outbound["settings"].(map[string]any)
	vnext := settings["vnext"].([]any)[0].(map[string]any)
	user := vnext["users"].([]any)[0].(map[string]any)
	if user["encryption"] != "none" {
		t.Fatalf("xray sidecar must add vless encryption=none for stored xhttp nodes: %#v", user)
	}
}

func TestRenderStoredVLESSLinkFallbackRestoresTransportSettings(t *testing.T) {
	for _, tc := range []struct {
		name          string
		link          string
		rawStreamJSON string
		wantTransport string
		check         func(t *testing.T, transport map[string]any)
	}{
		{
			name:          "websocket",
			link:          "vless://00000000-0000-0000-0000-000000000000@example.com:443?type=ws&security=tls&sni=front.example&path=%2Fws&host=cdn.example#ws",
			rawStreamJSON: `"streamSettings":{"network":"ws","security":"tls","tlsSettings":{"serverName":"front.example"}}`,
			wantTransport: "ws",
			check: func(t *testing.T, transport map[string]any) {
				if transport["path"] != "/ws" {
					t.Fatalf("websocket path was not restored: %#v", transport)
				}
				headers := transport["headers"].(map[string]any)
				if headers["Host"] != "cdn.example" {
					t.Fatalf("websocket host was not restored: %#v", transport)
				}
			},
		},
		{
			name:          "grpc",
			link:          "vless://00000000-0000-0000-0000-000000000000@example.com:443?type=grpc&security=reality&sni=front.example&pbk=public-key&sid=short&serviceName=TunService#grpc",
			rawStreamJSON: `"streamSettings":{"network":"grpc","security":"reality","realitySettings":{"serverName":"front.example","publicKey":"public-key","shortId":"short"}}`,
			wantTransport: "grpc",
			check: func(t *testing.T, transport map[string]any) {
				if transport["service_name"] != "TunService" {
					t.Fatalf("grpc service name was not restored: %#v", transport)
				}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.Node.Address = ""
			cfg.Node.UUID = ""
			cfg.Profile.ActiveNodeID = tc.name + "-node"
			cfg.Profile.Nodes = []json.RawMessage{json.RawMessage(`{
				"id":"` + tc.name + `-node",
				"name":"` + tc.name + `",
				"protocol":"VLESS",
				"server":"example.com",
				"port":443,
				"link":"` + tc.link + `",
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
					` + tc.rawStreamJSON + `
				}
			}`)}

			data, err := RenderSingboxConfig(cfg, cfg.ResolveProfile())
			if err != nil {
				t.Fatalf("render sing-box config: %v", err)
			}
			var rendered map[string]any
			if err := json.Unmarshal(data, &rendered); err != nil {
				t.Fatalf("unmarshal rendered config: %v", err)
			}
			outbound := rendered["outbounds"].([]any)[0].(map[string]any)
			transport := outbound["transport"].(map[string]any)
			if transport["type"] != tc.wantTransport {
				t.Fatalf("unexpected transport: %#v", transport)
			}
			tc.check(t, transport)
		})
	}
}
