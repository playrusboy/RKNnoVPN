package modulehelper

import (
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/youtubediscord/RKNnoVPN/daemon/internal/modulecontract"
)

func TestCurrentStateTreatsValidEmptyProfileAsActive(t *testing.T) {
	moduleDir := t.TempDir()
	profilePath := filepath.Join(t.TempDir(), "profile.json")
	t.Setenv("RKNNOVPN_PROFILE_PATH", profilePath)
	prepareModuleFixture(t, moduleDir)
	if err := os.WriteFile(profilePath, []byte(`{"nodes":[]}`), 0600); err != nil {
		t.Fatal(err)
	}

	state := CurrentState(moduleDir)
	if state.Status != StatusActive {
		t.Fatalf("status = %q, want %q; message=%q", state.Status, StatusActive, state.Message)
	}
	if state.ProfileNodes != 0 {
		t.Fatalf("profile nodes = %d, want 0", state.ProfileNodes)
	}
}

func TestCurrentStateReportsMissingAndInvalidProfile(t *testing.T) {
	t.Run("missing", func(t *testing.T) {
		moduleDir := t.TempDir()
		t.Setenv("RKNNOVPN_PROFILE_PATH", filepath.Join(t.TempDir(), "profile.json"))
		prepareModuleFixture(t, moduleDir)

		state := CurrentState(moduleDir)
		if state.Status != StatusMissingProfile {
			t.Fatalf("status = %q, want %q", state.Status, StatusMissingProfile)
		}
	})

	t.Run("invalid", func(t *testing.T) {
		moduleDir := t.TempDir()
		profilePath := filepath.Join(t.TempDir(), "profile.json")
		t.Setenv("RKNNOVPN_PROFILE_PATH", profilePath)
		prepareModuleFixture(t, moduleDir)
		if err := os.WriteFile(profilePath, []byte(`{"nodes":[{"source":{"type":"ALIEN"}}]}`), 0600); err != nil {
			t.Fatal(err)
		}

		state := CurrentState(moduleDir)
		if state.Status != StatusInvalidProfile {
			t.Fatalf("status = %q, want %q", state.Status, StatusInvalidProfile)
		}
	})
}

func TestCurrentStateReportsSocketAccepting(t *testing.T) {
	moduleDir := t.TempDir()
	profilePath := filepath.Join(t.TempDir(), "profile.json")
	t.Setenv("RKNNOVPN_PROFILE_PATH", profilePath)
	prepareModuleFixture(t, moduleDir)
	if err := os.WriteFile(profilePath, []byte(`{"nodes":[]}`), 0600); err != nil {
		t.Fatal(err)
	}

	paths := modulecontract.NewPaths(moduleDir)
	if state := CurrentState(moduleDir); state.SocketAccepting {
		t.Fatal("socket accepting = true before socket listener exists")
	}

	listener, err := net.Listen("unix", paths.DaemonSocket())
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	accepted := make(chan struct{})
	go func() {
		conn, err := listener.Accept()
		if err == nil {
			conn.Close()
		}
		close(accepted)
	}()

	state := CurrentState(moduleDir)
	if !state.SocketPresent {
		t.Fatal("socket present = false with unix listener")
	}
	if !state.SocketAccepting {
		t.Fatal("socket accepting = false with unix listener")
	}
	<-accepted
}

func prepareModuleFixture(t *testing.T, moduleDir string) {
	t.Helper()
	paths := modulecontract.NewPaths(moduleDir)
	for _, dir := range []string{paths.BinDir(), paths.ConfigDir(), paths.RunDir()} {
		if err := os.MkdirAll(dir, 0750); err != nil {
			t.Fatal(err)
		}
	}
	for _, path := range []string{
		filepath.Join(paths.Dir(), "service.sh"),
		filepath.Join(paths.BinDir(), "daemonctl"),
		filepath.Join(paths.BinDir(), "daemon"),
	} {
		if err := os.WriteFile(path, []byte("#!/system/bin/sh\n"), 0750); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(paths.ConfigDir(), "config.json"), []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
}
