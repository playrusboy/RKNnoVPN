package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
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

func TestPrintUsageWritesOnlyToProvidedWriter(t *testing.T) {
	var stderr bytes.Buffer

	printUsageTo(&stderr)

	if !bytes.Contains(stderr.Bytes(), []byte("daemonctl - RKNnoVPN daemon control CLI")) {
		t.Fatalf("expected usage text in provided writer, got %q", stderr.String())
	}
}

func TestPrintHelperEnvelopeIsSingleJsonFrame(t *testing.T) {
	var stdout bytes.Buffer
	oldStdout := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = writer
	defer func() {
		os.Stdout = oldStdout
	}()

	printHelperEnvelope(map[string]string{"status": "active"})
	writer.Close()
	if _, err := stdout.ReadFrom(reader); err != nil {
		t.Fatal(err)
	}

	lines := bytes.Split(bytes.TrimSpace(stdout.Bytes()), []byte{'\n'})
	if len(lines) != 1 {
		t.Fatalf("expected one JSON frame, got %d: %q", len(lines), stdout.String())
	}
	if !bytes.Contains(lines[0], []byte(`"ok":true`)) {
		t.Fatalf("expected compact helper envelope, got %q", stdout.String())
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

func TestWriteResponsePrettyErrorDoesNotUseStdout(t *testing.T) {
	line := []byte(`{"jsonrpc":"2.0","id":1,"error":{"code":-32602,"message":"bad params","data":{"ok":false,"error":{"code":"INVALID_PARAMS","message":"bad params"}}}}`)
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := writeResponse(line, false, &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("expected exit code 1, got %d", exitCode)
	}
	if stdout.Len() != 0 {
		t.Fatalf("expected pretty error response to keep stdout empty, got %q", stdout.String())
	}
	if !bytes.Contains(stderr.Bytes(), []byte("bad params")) {
		t.Fatalf("expected error response on stderr, got %q", stderr.String())
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

func TestWriteResponseWithoutResultStillPrintsJson(t *testing.T) {
	line := []byte(`{"jsonrpc":"2.0","id":1}`)
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := writeResponse(line, false, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", exitCode)
	}
	if got := stdout.String(); got != "null\n" {
		t.Fatalf("expected JSON null stdout, got %q", got)
	}
	if stderr.Len() != 0 {
		t.Fatalf("expected stderr to be empty, got %q", stderr.String())
	}
}

func TestRunBridgeProxiesMultipleFrames(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "daemon.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		reader := bufio.NewReader(conn)
		for i := 0; i < 2; i++ {
			line, err := reader.ReadBytes('\n')
			if err != nil {
				t.Errorf("read request: %v", err)
				return
			}
			var req struct {
				ID int `json:"id"`
			}
			if err := json.Unmarshal(bytes.TrimSpace(line), &req); err != nil {
				t.Errorf("decode request: %v", err)
				return
			}
			_, _ = conn.Write([]byte(fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"result":{"ok":true,"result":{"id":%d}}}`+"\n", req.ID, req.ID)))
		}
	}()

	stdin := bytes.NewBufferString(
		`{"jsonrpc":"2.0","id":1,"method":"version"}` + "\n" +
			`{"jsonrpc":"2.0","id":2,"method":"backend.status"}` + "\n",
	)
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	if code := runBridge(stdin, &stdout, &stderr, socketPath); code != 0 {
		t.Fatalf("bridge exit code %d stderr=%q", code, stderr.String())
	}
	<-done
	lines := bytes.Split(bytes.TrimSpace(stdout.Bytes()), []byte{'\n'})
	if len(lines) != 2 {
		t.Fatalf("expected two response lines, got %d: %q", len(lines), stdout.String())
	}
	if !bytes.Contains(lines[0], []byte(`"id":1`)) || !bytes.Contains(lines[1], []byte(`"id":2`)) {
		t.Fatalf("bridge responses did not preserve ids: %q", stdout.String())
	}
}

func TestRunBridgeReturnsErrorWhenSocketUnavailable(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	stdin := bytes.NewBufferString(`{"jsonrpc":"2.0","id":1,"method":"version"}` + "\n")
	socketPath := filepath.Join(t.TempDir(), "missing.sock")

	if code := runBridge(stdin, &stdout, &stderr, socketPath); code == 0 {
		t.Fatal("expected bridge to fail when daemon socket is unavailable")
	}
	if stdout.Len() != 0 {
		t.Fatalf("expected no stdout on transport failure, got %q", stdout.String())
	}
	if !bytes.Contains(stderr.Bytes(), []byte("cannot connect to daemon")) {
		t.Fatalf("expected socket failure on stderr, got %q", stderr.String())
	}
}
