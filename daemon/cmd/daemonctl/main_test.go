package main

import (
	"bytes"
	"testing"
)

func TestParseArgsRawFlag(t *testing.T) {
	t.Setenv("RKNNOVPN_DAEMONCTL_RAW", "")

	opts, cmd, params, err := parseArgs([]string{"--raw", "backend.status"})
	if err != nil {
		t.Fatalf("parseArgs returned error: %v", err)
	}
	if !opts.rawOutput {
		t.Fatal("expected --raw to enable raw output")
	}
	if cmd != "backend.status" {
		t.Fatalf("unexpected command: %q", cmd)
	}
	if len(params) != 0 {
		t.Fatalf("unexpected params: %#v", params)
	}
}

func TestParseArgsCompactFlagWithParams(t *testing.T) {
	t.Setenv("RKNNOVPN_DAEMONCTL_RAW", "")

	opts, cmd, params, err := parseArgs([]string{"--compact", "logs", `{"lines":100}`})
	if err != nil {
		t.Fatalf("parseArgs returned error: %v", err)
	}
	if !opts.rawOutput {
		t.Fatal("expected --compact to enable raw output")
	}
	if cmd != "logs" {
		t.Fatalf("unexpected command: %q", cmd)
	}
	if len(params) != 1 || params[0] != `{"lines":100}` {
		t.Fatalf("unexpected params: %#v", params)
	}
}

func TestParseArgsRawEnv(t *testing.T) {
	t.Setenv("RKNNOVPN_DAEMONCTL_RAW", "1")

	opts, cmd, _, err := parseArgs([]string{"backend.status"})
	if err != nil {
		t.Fatalf("parseArgs returned error: %v", err)
	}
	if !opts.rawOutput {
		t.Fatal("expected RKNNOVPN_DAEMONCTL_RAW=1 to enable raw output")
	}
	if cmd != "backend.status" {
		t.Fatalf("unexpected command: %q", cmd)
	}
}

func TestParseArgsRawEnvFalse(t *testing.T) {
	t.Setenv("RKNNOVPN_DAEMONCTL_RAW", "false")

	opts, cmd, _, err := parseArgs([]string{"backend.status"})
	if err != nil {
		t.Fatalf("parseArgs returned error: %v", err)
	}
	if opts.rawOutput {
		t.Fatal("expected false env value to keep pretty output")
	}
	if cmd != "backend.status" {
		t.Fatalf("unexpected command: %q", cmd)
	}
}

func TestParseArgsHelpFlag(t *testing.T) {
	t.Setenv("RKNNOVPN_DAEMONCTL_RAW", "")

	_, cmd, _, err := parseArgs([]string{"-h"})
	if err != nil {
		t.Fatalf("parseArgs returned error: %v", err)
	}
	if cmd != "-h" {
		t.Fatalf("unexpected command: %q", cmd)
	}
}

func TestParseArgsUnknownOption(t *testing.T) {
	t.Setenv("RKNNOVPN_DAEMONCTL_RAW", "")

	_, _, _, err := parseArgs([]string{"--wat", "backend.status"})
	if err == nil {
		t.Fatal("expected unknown option error")
	}
}

func TestWriteResponseRawPreservesErrorEnvelopeOnStdout(t *testing.T) {
	line := []byte(`{"jsonrpc":"2.0","id":1,"error":{"code":-32602,"message":"bad params","data":{"ok":false,"error":{"code":"INVALID_PARAMS","message":"bad params"}}}}`)
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := writeResponse(line, true, &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("expected exit code 1, got %d", exitCode)
	}
	if got := stdout.String(); got != string(line)+"\n" {
		t.Fatalf("raw stdout mismatch:\n got: %q\nwant: %q", got, string(line)+"\n")
	}
	if stderr.Len() != 0 {
		t.Fatalf("expected raw error response to keep stderr empty, got %q", stderr.String())
	}
}

func TestWriteResponsePrettyPrintsResultEnvelope(t *testing.T) {
	line := []byte(`{"jsonrpc":"2.0","id":1,"result":{"ok":true,"result":{"state":"running"}}}`)
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := writeResponse(line, false, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", exitCode)
	}
	if !bytes.Contains(stdout.Bytes(), []byte("\n  \"ok\": true,")) {
		t.Fatalf("expected pretty-printed result envelope, got %q", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("expected stderr to be empty, got %q", stderr.String())
	}
}
