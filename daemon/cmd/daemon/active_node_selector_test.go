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

	tag := activeNodeSelectorSwitchTarget(oldCfg, newCfg, rootruntime.ReloadPlan{})
	if tag != "node-node-b" {
		t.Fatalf("selector switch tag = %q, want node-node-b", tag)
	}
}

func TestActiveNodeSelectorSwitchTargetRejectsNonActiveNodeChange(t *testing.T) {
	oldCfg := selectorTestConfig("node-a")
	newCfg := selectorTestConfig("node-b")
	newCfg.Proxy.DNSPort++

	if tag := activeNodeSelectorSwitchTarget(oldCfg, newCfg, rootruntime.ReloadPlan{}); tag != "" {
		t.Fatalf("selector switch target should reject DNS port change, got %q", tag)
	}
}

func TestSwitchSingboxSelectorSendsBearerPut(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	called := make(chan struct{}, 1)
	server := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() { called <- struct{}{} }()
			if r.Method != http.MethodPut {
				t.Errorf("method = %s, want PUT", r.Method)
			}
			if r.URL.Path != "/proxies/proxy" {
				t.Errorf("path = %s, want /proxies/proxy", r.URL.Path)
			}
			if r.Header.Get("Authorization") != "Bearer test-secret" {
				t.Errorf("unexpected auth header: %q", r.Header.Get("Authorization"))
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
