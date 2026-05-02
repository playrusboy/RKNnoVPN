package profile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/youtubediscord/RKNnoVPN/daemon/internal/config"
	"github.com/youtubediscord/RKNnoVPN/daemon/internal/modulecontract"
)

func TestPathUsesPersistentStateForInstalledModule(t *testing.T) {
	configPath := filepath.Join(modulecontract.DefaultModuleDir, modulecontract.ConfigDirName, "config.json")
	want := filepath.Join(modulecontract.DefaultStateDir, "profile.json")

	if got := Path(configPath); got != want {
		t.Fatalf("Path(%q) = %q, want %q", configPath, got, want)
	}
	if got := LegacyPath(configPath); got == want {
		t.Fatalf("LegacyPath should keep module-local profile path, got %q", got)
	}
}

func TestPathKeepsLocalTestProfilesBesideConfig(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config", "config.json")
	want := filepath.Join(filepath.Dir(configPath), "profile.json")

	if got := Path(configPath); got != want {
		t.Fatalf("Path(%q) = %q, want %q", configPath, got, want)
	}
}

func TestLoadConfigProjectionCopiesModuleProfileToPersistentState(t *testing.T) {
	dataDir := t.TempDir()
	moduleConfigDir := filepath.Join(dataDir, "modules", "rknnovpn", "config")
	stateDir := filepath.Join(dataDir, "rknnovpn-data")
	configPath := filepath.Join(moduleConfigDir, "config.json")
	profilePath := filepath.Join(stateDir, "profile.json")
	legacyProfilePath := filepath.Join(moduleConfigDir, "profile.json")

	t.Setenv("RKNNOVPN_PROFILE_PATH", profilePath)
	cfg := config.DefaultConfig()
	if err := cfg.Save(configPath); err != nil {
		t.Fatal(err)
	}
	doc := FromConfig(cfg)
	doc.ID = "persisted"
	doc.Name = "Persisted"
	if err := Save(legacyProfilePath, doc); err != nil {
		t.Fatal(err)
	}

	loaded, err := LoadConfigProjection(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.ProfileFound || loaded.ProfilePath != profilePath {
		t.Fatalf("unexpected loaded profile metadata: %#v", loaded)
	}
	if _, err := os.Stat(profilePath); err != nil {
		t.Fatalf("persistent profile was not written: %v", err)
	}
}

func TestLoadConfigProjectionDeletesUnsupportedProfileAndInitializesFresh(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	profilePath := filepath.Join(dir, "profile.json")
	t.Setenv("RKNNOVPN_PROFILE_PATH", profilePath)

	cfg := config.DefaultConfig()
	cfg.Node.Address = "legacy.example"
	cfg.Node.Port = 443
	cfg.Node.Protocol = "vless"
	cfg.Node.UUID = "00000000-0000-0000-0000-000000000000"
	if err := cfg.Save(configPath); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(profilePath, []byte(`{
		"profileSchemaVersion": 2,
		"id": "old",
		"name": "Old",
		"nodes": [],
		"runtime": {},
		"routing": {"mode":"PER_APP"},
		"dns": {"remoteDns":"https://1.1.1.1/dns-query","directDns":"https://dns.google/dns-query","bootstrapIp":"1.1.1.1","ipv6Mode":"MIRROR","blockQuic":true,"fakeDns":false},
		"health": {"enabled":true,"intervalSec":30,"threshold":3,"checkUrl":"https://www.gstatic.com/generate_204","timeoutSec":5,"dnsIsHardReadiness":false},
		"sharing": {"enabled":false},
		"tun": {"enabled":false,"mtu":9000,"ipv4Address":"172.19.0.1/30","ipv6":false,"autoRoute":true,"strictRoute":true},
		"inbounds": {"socksPort":0,"httpPort":0,"allowLan":false}
	}`), 0600); err != nil {
		t.Fatal(err)
	}

	loaded, err := LoadConfigProjection(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.ProfileReset || !strings.Contains(loaded.ResetReason, "blockQuic") {
		t.Fatalf("expected unsupported profile reset with blockQuic reason, got %#v", loaded)
	}
	if loaded.Config.Node.Address != "" {
		t.Fatalf("fresh profile should clear legacy config-only node, got %#v", loaded.Config.Node)
	}
	data, err := os.ReadFile(profilePath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "blockQuic") {
		t.Fatalf("old profile format was not deleted before fresh init: %s", data)
	}
	doc, found, err := Load(profilePath)
	if err != nil {
		t.Fatal(err)
	}
	if !found || len(doc.Nodes) != 0 || doc.ActiveNodeID != "" {
		t.Fatalf("fresh profile was not initialized cleanly: found=%v doc=%#v", found, doc)
	}
}
