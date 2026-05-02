package ipc

import (
	"encoding/json"
	"net"
	"os"
	"slices"
	"strings"
	"testing"
)

func TestRemoveStaleSocketRefusesNonSocket(t *testing.T) {
	path := t.TempDir() + "/daemon.sock"
	if err := os.WriteFile(path, []byte("not a socket"), 0600); err != nil {
		t.Fatalf("write placeholder: %v", err)
	}

	err := removeStaleSocket(path)

	if err == nil || !strings.Contains(err.Error(), "refuse to remove non-socket") {
		t.Fatalf("expected non-socket refusal, got %v", err)
	}
	if _, statErr := os.Stat(path); statErr != nil {
		t.Fatalf("non-socket path should remain: %v", statErr)
	}
}

func TestRemoveStaleSocketRemovesSocket(t *testing.T) {
	path := t.TempDir() + "/daemon.sock"
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatalf("listen unix: %v", err)
	}
	if err := ln.Close(); err != nil {
		t.Fatalf("close listener: %v", err)
	}

	if err := removeStaleSocket(path); err != nil {
		t.Fatalf("remove stale socket: %v", err)
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("socket should be removed, err=%v", err)
	}
}

func TestMethodNotFoundForLegacyConfigImportReturnsCanonicalHint(t *testing.T) {
	server := NewServer(t.TempDir() + "/daemon.sock")
	raw := []byte(`{"jsonrpc":"2.0","id":7,"method":"config.import","params":{"schema_version":5}}`)

	response := server.processRequest(raw)

	if response.Error == nil {
		t.Fatal("expected method not found error")
	}
	if response.Error.Code != CodeMethodNotFound {
		t.Fatalf("expected method not found, got %#v", response.Error)
	}
	data, ok := response.Error.Data.(Envelope)
	if !ok {
		t.Fatalf("expected envelope error data, got %T", response.Error.Data)
	}
	envelopeError, ok := data.Error.(EnvelopeError)
	if !ok {
		t.Fatalf("expected envelope error, got %T", data.Error)
	}
	details, ok := envelopeError.Details.(map[string]interface{})
	if !ok {
		t.Fatalf("expected method details, got %T", envelopeError.Details)
	}
	if details["requestedMethod"] != "config.import" {
		t.Fatalf("unexpected requested method detail: %#v", details)
	}
	replacement, _ := details["replacement"].(string)
	if !strings.Contains(replacement, "config-import") || !strings.Contains(replacement, "profile.importNodes") {
		t.Fatalf("expected canonical replacement hint, got %#v", replacement)
	}
	methods, ok := details["supportedMethods"].([]string)
	if !ok {
		t.Fatalf("expected supported methods detail, got %T", details["supportedMethods"])
	}
	if slices.Contains(methods, "config.import") || !slices.Contains(methods, "config-import") {
		t.Fatalf("supported methods should advertise canonical methods only: %#v", methods)
	}
	if _, err := json.Marshal(response); err != nil {
		t.Fatalf("method not found response must marshal: %v", err)
	}
}
