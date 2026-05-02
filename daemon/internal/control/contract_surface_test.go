package control

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCompatibilityFingerprintChangesWithProcessIdentity(t *testing.T) {
	base := CompatibilityFingerprint("v2.0.0", "v2.0.0", "hash", 100, "200")
	if same := CompatibilityFingerprint("v2.0.0", "v2.0.0", "hash", 100, "200"); same != base {
		t.Fatalf("fingerprint must be stable for same inputs: %q != %q", same, base)
	}
	if changed := CompatibilityFingerprint("v2.0.0", "v2.0.0", "hash", 101, "200"); changed == base {
		t.Fatalf("fingerprint must change when daemon pid changes")
	}
	if changed := CompatibilityFingerprint("v2.0.0", "v2.0.0", "hash", 100, "201"); changed == base {
		t.Fatalf("fingerprint must change when socket inode changes")
	}
}

func TestDaemonIdentityIncludesSocketInode(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "daemon.sock")
	if err := os.WriteFile(socketPath, []byte("socket placeholder"), 0600); err != nil {
		t.Fatal(err)
	}
	pid, inode := DaemonIdentity(socketPath)
	if pid != os.Getpid() {
		t.Fatalf("daemon pid = %d, want %d", pid, os.Getpid())
	}
	if inode == "" {
		t.Fatalf("expected socket inode")
	}
}
