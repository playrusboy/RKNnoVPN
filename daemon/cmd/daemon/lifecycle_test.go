package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRemovePIDIfOwnedKeepsNewDaemonPID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daemon.pid")
	if err := writePID(path, 2002); err != nil {
		t.Fatalf("write new pid: %v", err)
	}
	if err := removePIDIfOwned(path, 1001); err != nil {
		t.Fatalf("remove old pid: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("pid file should remain: %v", err)
	}
	if string(data) != "2002\n" {
		t.Fatalf("pid file changed: %q", data)
	}
}

func TestRemovePIDIfOwnedRemovesOwnPID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daemon.pid")
	if err := writePID(path, 1001); err != nil {
		t.Fatalf("write pid: %v", err)
	}
	if err := removePIDIfOwned(path, 1001); err != nil {
		t.Fatalf("remove owned pid: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("pid file should be removed, err=%v", err)
	}
}

func TestRefreshPIDIfMissingOrOwnedRestoresMissingPID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daemon.pid")
	if err := refreshPIDIfMissingOrOwned(path, 2002); err != nil {
		t.Fatalf("refresh missing pid: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("pid file should be restored: %v", err)
	}
	if string(data) != "2002\n" {
		t.Fatalf("pid file = %q", data)
	}
}

func TestRefreshPIDIfMissingOrOwnedKeepsOtherPID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daemon.pid")
	if err := writePID(path, 3003); err != nil {
		t.Fatalf("write other pid: %v", err)
	}
	if err := refreshPIDIfMissingOrOwned(path, 2002); err != nil {
		t.Fatalf("refresh pid: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read pid: %v", err)
	}
	if string(data) != "3003\n" {
		t.Fatalf("other pid should remain, got %q", data)
	}
}
