package ipc

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

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
	if !strings.Contains(replacement, "config-import") || !strings.Contains(replacement, "profile.apply") {
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
