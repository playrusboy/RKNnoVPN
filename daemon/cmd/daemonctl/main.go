package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"sort"
	"strings"

	"github.com/youtubediscord/RKNnoVPN/daemon/internal/ipc"
	"github.com/youtubediscord/RKNnoVPN/daemon/internal/modulecontract"
)

const maxFrameBytes = 16 * 1024 * 1024

var Version = "v2.2.10"

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	cmd := os.Args[1]

	if cmd == "help" || cmd == "--help" || cmd == "-h" {
		printUsage()
		os.Exit(0)
	}

	if _, ok := supportedCommandSet()[cmd]; !ok {
		fmt.Fprintf(os.Stderr, "error: unknown command %q\n\n", cmd)
		printUsage()
		os.Exit(1)
	}

	// Build JSON-RPC request.
	req := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  cmd,
	}

	// Parse params either from argv or stdin (for large payloads that would
	// otherwise overflow shell/argv limits).
	if raw := readRawParams(os.Args[2:]); raw != "" {
		var params json.RawMessage
		if err := json.Unmarshal([]byte(raw), &params); err != nil {
			fmt.Fprintf(os.Stderr, "error: invalid JSON params: %v\n", err)
			os.Exit(1)
		}
		req["params"] = params
	}

	// Determine socket path.
	socketPath := os.Getenv("RKNNOVPN_SOCKET")
	if socketPath == "" {
		socketPath = modulecontract.NewPaths("").DaemonSocket()
	}

	// Connect to daemon.
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: cannot connect to daemon at %s: %v\n", socketPath, err)
		fmt.Fprintf(os.Stderr, "hint: is daemon running?\n")
		os.Exit(1)
	}
	defer conn.Close()

	// Send request.
	reqBytes, err := json.Marshal(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: marshal request: %v\n", err)
		os.Exit(1)
	}
	reqBytes = append(reqBytes, '\n')

	if _, err := conn.Write(reqBytes); err != nil {
		fmt.Fprintf(os.Stderr, "error: send request: %v\n", err)
		os.Exit(1)
	}

	// Read response.
	reader := bufio.NewReader(conn)
	line, err := readFrame(reader)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: read response: %v\n", err)
		os.Exit(1)
	}
	if len(line) == 0 {
		fmt.Fprintf(os.Stderr, "error: daemon closed connection without response\n")
		os.Exit(1)
	}

	// Parse response.
	var resp struct {
		JSONRPC string           `json:"jsonrpc"`
		ID      int              `json:"id"`
		Result  *json.RawMessage `json:"result,omitempty"`
		Error   *struct {
			Code    int              `json:"code"`
			Message string           `json:"message"`
			Data    *json.RawMessage `json:"data,omitempty"`
		} `json:"error,omitempty"`
	}

	if err := json.Unmarshal(line, &resp); err != nil {
		fmt.Fprintf(os.Stderr, "error: parse response: %v\n", err)
		fmt.Fprintf(os.Stderr, "raw: %s\n", string(line))
		os.Exit(1)
	}

	// Handle error response.
	if resp.Error != nil {
		fmt.Fprintf(os.Stderr, "error [%d]: %s\n", resp.Error.Code, resp.Error.Message)
		prettyPrint(line)
		os.Exit(1)
	}

	// Print result.
	if resp.Result != nil {
		prettyPrint(*resp.Result)
	} else {
		fmt.Println("ok")
	}
}

func prettyPrint(data json.RawMessage) {
	var obj interface{}
	if err := json.Unmarshal(data, &obj); err != nil {
		fmt.Println(string(data))
		return
	}

	pretty, err := json.MarshalIndent(obj, "", "  ")
	if err != nil {
		fmt.Println(string(data))
		return
	}
	fmt.Println(string(pretty))
}

func printUsage() {
	fmt.Println("daemonctl - RKNnoVPN daemon control CLI")
	fmt.Println()
	fmt.Println("Usage: daemonctl <command> [json_params]")
	fmt.Println()
	fmt.Println("Commands:")

	// Calculate max command name length for alignment.
	maxLen := 0
	for cmd := range supportedCommandSet() {
		if len(cmd) > maxLen {
			maxLen = len(cmd)
		}
	}

	for _, cmd := range orderedCommands() {
		desc := commandDescription(cmd)
		fmt.Printf("  %-*s  %s\n", maxLen, cmd, desc)
	}

	fmt.Println()
	fmt.Println("Environment:")
	fmt.Printf("  RKNNOVPN_SOCKET  daemon socket path (default: %s)\n", modulecontract.NewPaths("").DaemonSocket())
	fmt.Println()
	fmt.Println("Examples:")
	printExamples()
}

func supportedCommandSet() map[string]bool {
	methods := ipc.SupportedMethods()
	result := make(map[string]bool, len(methods))
	for _, method := range methods {
		result[method] = true
	}
	return result
}

func orderedCommands() []string {
	contracts := ipc.MethodContracts()
	methods := make([]string, 0, len(contracts))
	for _, contract := range contracts {
		methods = append(methods, contract.Method)
	}
	sort.Strings(methods)
	return methods
}

func commandDescription(method string) string {
	for _, contract := range ipc.MethodContracts() {
		if contract.Method != method {
			continue
		}
		parts := []string{}
		if contract.Request != "" && contract.Result != "" {
			parts = append(parts, fmt.Sprintf("%s -> %s", contract.Request, contract.Result))
		}
		if contract.Mutating {
			parts = append(parts, "mutating")
		}
		if contract.Async {
			parts = append(parts, "async")
		}
		if contract.Operation != nil && contract.Operation.Type != "" {
			parts = append(parts, "operation="+contract.Operation.Type)
		}
		if contract.Capability != "" {
			parts = append(parts, "capability="+contract.Capability)
		}
		if len(parts) > 0 {
			return strings.Join(parts, "; ")
		}
	}
	return "IPC contract method"
}

func printExamples() {
	contracts := methodContractMap()
	for _, method := range orderedCommands() {
		contract, ok := contracts[method]
		if !ok {
			continue
		}
		fmt.Println("  " + commandExample(contract))
	}
}

func methodContractMap() map[string]ipc.MethodContract {
	contracts := ipc.MethodContracts()
	result := make(map[string]ipc.MethodContract, len(contracts))
	for _, contract := range contracts {
		result[contract.Method] = contract
	}
	return result
}

func commandExample(contract ipc.MethodContract) string {
	if contract.Request == "" || contract.Request == "empty" {
		return "daemonctl " + contract.Method
	}
	if params, ok := ipc.RequestExample(contract.Request); ok && len(params) > 0 {
		return fmt.Sprintf("daemonctl %s '%s'", contract.Method, string(params))
	}
	return fmt.Sprintf("daemonctl %s '<%s JSON>'", contract.Method, contract.Request)
}

func readRawParams(args []string) string {
	if len(args) > 0 {
		return strings.TrimSpace(strings.Join(args, " "))
	}
	if os.Getenv("RKNNOVPN_STDIN_PARAMS") != "1" {
		return ""
	}

	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func readFrame(reader *bufio.Reader) ([]byte, error) {
	var frame []byte
	for {
		chunk, isPrefix, err := reader.ReadLine()
		if err != nil {
			if err == io.EOF && len(frame) > 0 {
				return frame, nil
			}
			return nil, err
		}
		if len(frame)+len(chunk) > maxFrameBytes {
			return nil, fmt.Errorf("frame exceeds %d bytes", maxFrameBytes)
		}
		frame = append(frame, chunk...)
		if !isPrefix {
			return frame, nil
		}
	}
}
