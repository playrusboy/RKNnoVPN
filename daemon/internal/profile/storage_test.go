package profile

import (
	"os"
	"path/filepath"
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
