package main

import (
	"encoding/json"
	"net"
	"net/http"
	"testing"

	"github.com/youtubediscord/RKNnoVPN/daemon/internal/config"
	rootruntime "github.com/youtubediscord/RKNnoVPN/daemon/internal/runtime/root"
)

func TestActiveNodeSelectorSwitchTargetAllowsOnlyActiveNodeProjectionChange(t *testing.T) {
	oldCfg := selectorTestConfig("node-a")
	newCfg := selectorTestConfig("node-b")
	oldCfg.Node.Address = "old.example"
	newCfg.Node.Address = "new.example"
	oldCfg.Transport.Protocol = "tcp"
	newCfg.Transport.Protocol = "grpc"

	decision := activeNodeSelectorSwitchTarget(oldCfg, newCfg, rootruntime.ReloadPlan{})
	if decision.Target != "node-node-b" {
		t.Fatalf("selector switch tag = %q, want node-node-b", decision.Target)
	}
}

func TestActiveNodeSelectorSwitchTargetRejectsNonActiveNodeChange(t *testing.T) {
	oldCfg := selectorTestConfig("node-a")
	newCfg := selectorTestConfig("node-b")
	newCfg.Proxy.DNSPort++

	decision := activeNodeSelectorSwitchTarget(oldCfg, newCfg, rootruntime.ReloadPlan{})
	if decision.Target != "" || decision.Reason != "non-active-node-config-changed" {
		t.Fatalf("selector switch target should reject DNS port change, got %#v", decision)
	}
}

func TestActiveNodeSelectorSwitchTargetRejectsXraySidecarNodes(t *testing.T) {
	oldCfg := selectorTestConfig("node-a")
	oldCfg.Profile.Nodes = append(oldCfg.Profile.Nodes, selectorTestXHTTPNode())
	newCfg := selectorTestConfig("node-xhttp")
	newCfg.Profile.Nodes = append(newCfg.Profile.Nodes, selectorTestXHTTPNode())

	decision := activeNodeSelectorSwitchTarget(oldCfg, newCfg, rootruntime.ReloadPlan{})
	if decision.Target != "" || decision.Reason != "new-active-node-requires-xray-sidecar" {
		t.Fatalf("selector switch target should reject xray sidecar node, got %#v", decision)
	}
}

func TestActiveNodeSelectorSwitchTargetAllowsClearingToAuto(t *testing.T) {
	oldCfg := selectorTestConfig("node-a")
	newCfg := selectorTestConfig("")

	decision := activeNodeSelectorSwitchTarget(oldCfg, newCfg, rootruntime.ReloadPlan{})
	if decision.Target != "auto" {
		t.Fatalf("selector switch tag = %q, want auto", decision.Target)
	}
}

func TestSwitchSingboxSelectorSendsBearerPut(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	called := make(chan struct{}, 2)
	server := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() { called <- struct{}{} }()
			if r.URL.Path != "/proxies/proxy" {
				t.Errorf("path = %s, want /proxies/proxy", r.URL.Path)
			}
			if r.Header.Get("Authorization") != "Bearer test-secret" {
				t.Errorf("unexpected auth header: %q", r.Header.Get("Authorization"))
			}
			if r.Method == http.MethodGet {
				_, _ = w.Write([]byte(`{"name":"proxy","now":"node-node-b"}`))
				return
			}
			if r.Method != http.MethodPut {
				t.Errorf("method = %s, want PUT", r.Method)
			}
			var body struct {
				Name string `json:"name"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode body: %v", err)
			}
			if body.Name != "node-node-b" {
				t.Errorf("body.name = %q, want node-node-b", body.Name)
			}
			w.WriteHeader(http.StatusNoContent)
		}),
	}
	go func() {
		_ = server.Serve(listener)
	}()
	defer server.Close()

	cfg := selectorTestConfig("node-b")
	cfg.Proxy.APIPort = listener.Addr().(*net.TCPAddr).Port
	cfg.Proxy.APISecret = "test-secret"

	if err := switchSingboxSelector(cfg, "proxy", "node-node-b"); err != nil {
		t.Fatalf("switchSingboxSelector failed: %v", err)
	}
	<-called
	<-called
}

func TestSwitchSingboxSelectorVerifiesSelectedOutbound(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	server := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodGet {
				_, _ = w.Write([]byte(`{"name":"proxy","now":"node-node-a"}`))
				return
			}
			w.WriteHeader(http.StatusNoContent)
		}),
	}
	go func() {
		_ = server.Serve(listener)
	}()
	defer server.Close()

	cfg := selectorTestConfig("node-b")
	cfg.Proxy.APIPort = listener.Addr().(*net.TCPAddr).Port
	cfg.Proxy.APISecret = "test-secret"

	if err := switchSingboxSelector(cfg, "proxy", "node-node-b"); err == nil {
		t.Fatal("expected selector verification mismatch")
	}
}

func selectorTestConfig(activeNodeID string) *config.Config {
	cfg := config.DefaultConfig()
	cfg.Proxy.APIPort = 19090
	cfg.Proxy.APISecret = "test-secret"
	cfg.Profile.ID = "default"
	cfg.Profile.Name = "Default"
	cfg.Profile.ActiveNodeID = activeNodeID
	cfg.Profile.Nodes = []json.RawMessage{
		json.RawMessage(`{"id":"node-a","name":"Node A","protocol":"socks","server":"127.0.0.1","port":1080,"source":{"type":"MANUAL"}}`),
		json.RawMessage(`{"id":"node-b","name":"Node B","protocol":"socks","server":"127.0.0.2","port":1081,"source":{"type":"MANUAL"}}`),
	}
	return cfg
}

func selectorTestXHTTPNode() json.RawMessage {
	return json.RawMessage(`{
		"id":"node-xhttp",
		"name":"XHTTP",
		"server":"example.com",
		"port":443,
		"protocol":"vless",
		"source":{"type":"MANUAL"},
		"outbound":{
			"protocol":"vless",
			"settings":{
				"vnext":[{
					"address":"example.com",
					"port":443,
					"users":[{"id":"00000000-0000-0000-0000-000000000000","encryption":"none"}]
				}]
			},
			"streamSettings":{
				"network":"xhttp",
				"security":"reality",
				"realitySettings":{"serverName":"www.example.com","publicKey":"public-key"},
				"xhttpSettings":{"path":"/","mode":"auto"}
			}
		}
	}`)
}
