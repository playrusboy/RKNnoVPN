package subscription

import (
	"encoding/base64"
	"encoding/json"
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

func TestParseVLESSSubscriptionPreservesTransportSettings(t *testing.T) {
	for _, tc := range []struct {
		name    string
		link    string
		network string
		check   func(t *testing.T, stream map[string]any)
	}{
		{
			name:    "websocket",
			link:    "vless://00000000-0000-0000-0000-000000000000@example.com:443?type=ws&security=tls&sni=front.example&path=%2Fws&host=cdn.example#ws",
			network: "ws",
			check: func(t *testing.T, stream map[string]any) {
				ws := stream["wsSettings"].(map[string]any)
				if ws["path"] != "/ws" {
					t.Fatalf("websocket path was not preserved: %#v", ws)
				}
				headers := ws["headers"].(map[string]any)
				if headers["Host"] != "cdn.example" {
					t.Fatalf("websocket host header was not preserved: %#v", ws)
				}
			},
		},
		{
			name:    "grpc",
			link:    "vless://00000000-0000-0000-0000-000000000000@example.com:443?type=grpc&security=reality&sni=front.example&pbk=public-key&sid=short&serviceName=TunService&authority=cdn.example#grpc",
			network: "grpc",
			check: func(t *testing.T, stream map[string]any) {
				grpc := stream["grpcSettings"].(map[string]any)
				if grpc["serviceName"] != "TunService" || grpc["authority"] != "cdn.example" {
					t.Fatalf("grpc settings were not preserved: %#v", grpc)
				}
			},
		},
		{
			name:    "xhttp",
			link:    "vless://00000000-0000-0000-0000-000000000000@example.com:443?type=xhttp&security=reality&sni=front.example&pbk=public-key&sid=short&path=%2Fxhttp&mode=auto#xhttp",
			network: "xhttp",
			check: func(t *testing.T, stream map[string]any) {
				xhttp := stream["xhttpSettings"].(map[string]any)
				if xhttp["path"] != "/xhttp" || xhttp["mode"] != "auto" {
					t.Fatalf("xhttp settings were not preserved: %#v", xhttp)
				}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			node, err := ParseLink(tc.link, 123)
			if err != nil {
				t.Fatalf("parse link: %v", err)
			}
			var outbound map[string]any
			if err := json.Unmarshal(node.Outbound, &outbound); err != nil {
				t.Fatalf("unmarshal outbound: %v", err)
			}
			settings := outbound["settings"].(map[string]any)
			vnext := settings["vnext"].([]any)[0].(map[string]any)
			user := vnext["users"].([]any)[0].(map[string]any)
			if user["encryption"] != "none" {
				t.Fatalf("vless encryption must default to none: %#v", user)
			}
			stream := outbound["streamSettings"].(map[string]any)
			if stream["network"] != tc.network {
				t.Fatalf("unexpected network: %#v", stream)
			}
			tc.check(t, stream)
		})
	}
}

func TestParseShadowsocksSubscriptionCredentials(t *testing.T) {
	encodedUserInfo := base64.RawURLEncoding.EncodeToString([]byte("chacha20-ietf-poly1305:encoded-secret"))
	legacyBody := base64.RawStdEncoding.EncodeToString([]byte("aes-192-gcm:legacy-secret@example.com:8388"))
	for _, tc := range []struct {
		name         string
		link         string
		wantMethod   string
		wantPassword string
		wantPlugin   string
		wantOpts     string
	}{
		{
			name:         "sip002-base64-userinfo",
			link:         "ss://" + encodedUserInfo + "@example.com:8388#encoded",
			wantMethod:   "chacha20-ietf-poly1305",
			wantPassword: "encoded-secret",
		},
		{
			name:         "sip002-plain-userinfo",
			link:         "ss://aes-128-gcm:plain-secret@example.com:8388#plain",
			wantMethod:   "aes-128-gcm",
			wantPassword: "plain-secret",
		},
		{
			name:         "query-method-password-userinfo",
			link:         "ss://query-secret@example.com:8388?method=aes-256-gcm#query",
			wantMethod:   "aes-256-gcm",
			wantPassword: "query-secret",
		},
		{
			name:         "sip002-plugin",
			link:         "ss://" + encodedUserInfo + "@example.com:8388/?plugin=v2ray-plugin%3Bserver#plugin",
			wantMethod:   "chacha20-ietf-poly1305",
			wantPassword: "encoded-secret",
			wantPlugin:   "v2ray-plugin",
			wantOpts:     "server",
		},
		{
			name:         "legacy-whole-link-base64",
			link:         "ss://" + strings.TrimRight(legacyBody, "=") + "#legacy",
			wantMethod:   "aes-192-gcm",
			wantPassword: "legacy-secret",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			node, err := ParseLink(tc.link, 123)
			if err != nil {
				t.Fatalf("parse link: %v", err)
			}
			var outbound map[string]any
			if err := json.Unmarshal(node.Outbound, &outbound); err != nil {
				t.Fatalf("unmarshal outbound: %v", err)
			}
			settings := outbound["settings"].(map[string]any)
			server := settings["servers"].([]any)[0].(map[string]any)
			if server["method"] != tc.wantMethod || server["password"] != tc.wantPassword {
				t.Fatalf("unexpected shadowsocks credentials: %#v", server)
			}
			if tc.wantPlugin != "" && (server["plugin"] != tc.wantPlugin || server["plugin_opts"] != tc.wantOpts) {
				t.Fatalf("unexpected shadowsocks plugin settings: %#v", server)
			}
		})
	}
}
