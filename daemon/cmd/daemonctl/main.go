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
	"github.com/youtubediscord/RKNnoVPN/daemon/internal/modulehelper"
)

const maxFrameBytes = 2 * 1024 * 1024

var Version = "dev"

func main() {
	opts, cmd, paramArgs, err := parseArgs(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n\n", err)
		printUsage()
		os.Exit(1)
	}
	if cmd == "" {
		printUsage()
		os.Exit(1)
	}

	if cmd == "help" || cmd == "--help" || cmd == "-h" {
		printUsage()
		os.Exit(0)
	}

	if cmd == "bridge" {
		os.Exit(runBridge(os.Stdin, os.Stdout, os.Stderr, socketPathFromEnv()))
	}

	if isModuleHelperCommand(cmd) {
		runModuleHelperCommand(cmd)
		return
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
	if raw := readRawParams(paramArgs); raw != "" {
		var params json.RawMessage
		if err := json.Unmarshal([]byte(raw), &params); err != nil {
			fmt.Fprintf(os.Stderr, "error: invalid JSON params: %v\n", err)
			os.Exit(1)
		}
		req["params"] = params
	}

	// Determine socket path.
	socketPath := socketPathFromEnv()

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
	os.Exit(writeResponse(line, opts.rawOutput, os.Stdout, os.Stderr))
}

func socketPathFromEnv() string {
	socketPath := os.Getenv("RKNNOVPN_SOCKET")
	if socketPath == "" {
		socketPath = modulecontract.NewPaths("").DaemonSocket()
	}
	return socketPath
}

func runBridge(stdin io.Reader, stdout io.Writer, stderr io.Writer, socketPath string) int {
	reader := bufio.NewReader(stdin)
	writer := bufio.NewWriter(stdout)
	defer writer.Flush()

	for {
		frame, err := readFrame(reader)
		if err != nil {
			if err == io.EOF && len(frame) == 0 {
				return 0
			}
			fmt.Fprintf(stderr, "error: read bridge frame: %v\n", err)
			return 1
		}
		if len(frame) == 0 {
			continue
		}
		response, err := proxyFrame(socketPath, frame)
		if err != nil {
			fmt.Fprintf(stderr, "error: bridge proxy failed: %v\n", err)
			return 1
		}
		printRaw(writer, response)
		if err := writer.Flush(); err != nil {
			fmt.Fprintf(stderr, "error: write bridge response: %v\n", err)
			return 1
		}
	}
}

func proxyFrame(socketPath string, frame []byte) ([]byte, error) {
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("cannot connect to daemon at %s: %w", socketPath, err)
	}
	defer conn.Close()

	request := append(append([]byte{}, frame...), '\n')
	if _, err := conn.Write(request); err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	response, err := readFrame(bufio.NewReader(conn))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if len(response) == 0 {
		return nil, fmt.Errorf("daemon closed connection without response")
	}
	return response, nil
}

func writeResponse(line []byte, rawOutput bool, stdout io.Writer, stderr io.Writer) int {
	if rawOutput {
		printRaw(stdout, line)
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
		fmt.Fprintf(stderr, "error: parse response: %v\n", err)
		if !rawOutput {
			fmt.Fprintf(stderr, "raw: %s\n", string(line))
		}
		return 1
	}

	// Handle error response.
	if resp.Error != nil {
		if !rawOutput {
			fmt.Fprintf(stderr, "error [%d]: %s\n", resp.Error.Code, resp.Error.Message)
			prettyPrint(stdout, line)
		}
		return 1
	}
	if rawOutput {
		return 0
	}

	// Print result.
	if resp.Result != nil {
		prettyPrint(stdout, *resp.Result)
	} else {
		fmt.Fprintln(stdout, "ok")
	}
	return 0
}

type options struct {
	rawOutput bool
}

func parseArgs(args []string) (options, string, []string, error) {
	opts := options{
		rawOutput: envFlagEnabled("RKNNOVPN_DAEMONCTL_RAW"),
	}
	for len(args) > 0 {
		switch args[0] {
		case "--raw", "--compact":
			opts.rawOutput = true
			args = args[1:]
		case "--help", "-h":
			return opts, args[0], args[1:], nil
		default:
			if strings.HasPrefix(args[0], "-") {
				return opts, "", nil, fmt.Errorf("unknown option %q", args[0])
			}
			return opts, args[0], args[1:], nil
		}
	}
	return opts, "", nil, nil
}

func envFlagEnabled(name string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(name))) {
	case "", "0", "false", "no", "off":
		return false
	default:
		return true
	}
}

func printRaw(w io.Writer, data []byte) {
	_, _ = w.Write(data)
	_, _ = w.Write([]byte{'\n'})
}

func prettyPrint(w io.Writer, data json.RawMessage) {
	var obj interface{}
	if err := json.Unmarshal(data, &obj); err != nil {
		fmt.Fprintln(w, string(data))
		return
	}

	pretty, err := json.MarshalIndent(obj, "", "  ")
	if err != nil {
		fmt.Fprintln(w, string(data))
		return
	}
	fmt.Fprintln(w, string(pretty))
}

func printUsage() {
	fmt.Println("daemonctl - RKNnoVPN daemon control CLI")
	fmt.Println()
	fmt.Println("Usage: daemonctl [--raw|--compact] <command> [json_params]")
	fmt.Println()
	fmt.Println("Commands:")

	// Calculate max command name length for alignment.
	maxLen := 0
	for cmd := range supportedCommandSet() {
		if len(cmd) > maxLen {
			maxLen = len(cmd)
		}
	}
	for _, cmd := range orderedModuleHelperCommands() {
		if len(cmd) > maxLen {
			maxLen = len(cmd)
		}
	}

	for _, cmd := range orderedCommands() {
		desc := commandDescription(cmd)
		fmt.Printf("  %-*s  %s\n", maxLen, cmd, desc)
	}
	for _, cmd := range orderedModuleHelperCommands() {
		desc := commandDescription(cmd)
		fmt.Printf("  %-*s  %s\n", maxLen, cmd, desc)
	}

	fmt.Println()
	fmt.Println("Environment:")
	fmt.Printf("  RKNNOVPN_SOCKET  daemon socket path (default: %s)\n", modulecontract.NewPaths("").DaemonSocket())
	fmt.Println("  RKNNOVPN_DAEMONCTL_RAW  print the daemon JSON-RPC response without pretty-printing")
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
	if desc := moduleHelperCommandDescription(method); desc != "" {
		return desc
	}
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

func isModuleHelperCommand(cmd string) bool {
	_, ok := moduleHelperCommandDescriptions()[cmd]
	return ok
}

func orderedModuleHelperCommands() []string {
	methods := make([]string, 0, len(moduleHelperCommandDescriptions()))
	for method := range moduleHelperCommandDescriptions() {
		methods = append(methods, method)
	}
	sort.Strings(methods)
	return methods
}

func moduleHelperCommandDescription(method string) string {
	return moduleHelperCommandDescriptions()[method]
}

func moduleHelperCommandDescriptions() map[string]string {
	return map[string]string{
		"bridge":              "persistent raw JSON-RPC bridge over stdin/stdout",
		"module.repair":       "local root helper; start daemon repair when IPC is unavailable",
		"module.repairStatus": "local root helper; report structured app repair status",
		"module.state":        "local root helper; report module install/profile/daemon state",
	}
}

func runModuleHelperCommand(cmd string) {
	var result interface{}
	switch cmd {
	case "module.state":
		result = modulehelper.CurrentState("")
	case "module.repair":
		result = modulehelper.StartRepair("")
	case "module.repairStatus":
		result = modulehelper.CurrentRepairStatus("")
	default:
		fmt.Fprintf(os.Stderr, "error: unknown module helper command %q\n", cmd)
		os.Exit(1)
	}
	printHelperEnvelope(result)
}

func printHelperEnvelope(result interface{}) {
	data, err := json.MarshalIndent(map[string]interface{}{
		"ok":     true,
		"result": result,
	}, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: marshal helper result: %v\n", err)
		os.Exit(1)
	}
	fmt.Println(string(data))
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
