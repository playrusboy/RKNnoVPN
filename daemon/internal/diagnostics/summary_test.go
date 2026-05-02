package diagnostics

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/youtubediscord/RKNnoVPN/daemon/internal/config"
	"github.com/youtubediscord/RKNnoVPN/daemon/internal/runtimev2"
)

func TestProfileSummaryReportsSkippedRuntimeNodes(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Node.Address = ""
	cfg.Node.UUID = ""
	cfg.Profile.ActiveNodeID = "good-vless"
	cfg.Profile.Nodes = []json.RawMessage{
		json.RawMessage(`{
			"id":"stale-node",
			"name":"Removed",
			"protocol":"VLESS",
			"server":"stale.example",
			"port":443,
			"stale":true
		}`),
		json.RawMessage(`{
			"id":"bad-ss",
			"name":"Bad SS",
			"protocol":"SHADOWSOCKS",
			"server":"127.0.0.1",
			"port":8388,
			"outbound":{
				"protocol":"shadowsocks",
				"settings":{
					"servers":[{"address":"127.0.0.1","port":8388,"method":"not-a-sing-box-method","password":"secret"}]
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

	summary := ProfileSummaryFromConfig(cfg, runtimev2.Status{})
	if summary.RenderableNodeCount != 1 || summary.SkippedRuntimeNodes != 2 {
		t.Fatalf("unexpected skipped node counts: %#v", summary)
	}
	joinedWarnings := strings.Join(summary.RuntimeWarnings, "\n")
	if !strings.Contains(joinedWarnings, "Removed skipped from runtime config: node is stale") {
		t.Fatalf("stale node warning missing: %#v", summary.RuntimeWarnings)
	}
	if !strings.Contains(joinedWarnings, "Bad SS skipped from runtime config: invalid sing-box outbound") {
		t.Fatalf("invalid outbound warning missing: %#v", summary.RuntimeWarnings)
	}
}
