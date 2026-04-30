package netstack

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/youtubediscord/RKNnoVPN/daemon/internal/modulecontract"
)

type ExecScriptFunc func(scriptPath string, command string, env map[string]string) error
type ExecCommandFunc func(name string, args ...string) (string, error)

type Step struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}

type Report struct {
	Operation       string   `json:"operation"`
	Status          string   `json:"status"`
	Steps           []Step   `json:"steps"`
	Errors          []string `json:"errors,omitempty"`
	Warnings        []string `json:"warnings,omitempty"`
	Leftovers       []string `json:"leftovers,omitempty"`
	RollbackApplied bool     `json:"rollbackApplied,omitempty"`
}

type Error struct {
	Operation string
	Code      string
	Report    Report
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if len(e.Report.Errors) == 0 {
		return e.Operation + " failed"
	}
	return fmt.Sprintf("%s failed: %s", e.Operation, strings.Join(e.Report.Errors, "; "))
}

type Manager struct {
	dataDir     string
	env         map[string]string
	execScript  ExecScriptFunc
	execCommand ExecCommandFunc
}

func New(dataDir string, env map[string]string, execScript ExecScriptFunc) Manager {
	return Manager{
		dataDir:    dataDir,
		env:        env,
		execScript: execScript,
	}
}

func (m Manager) WithExecCommand(execCommand ExecCommandFunc) Manager {
	m.execCommand = execCommand
	return m
}

func (m Manager) Apply() Report {
	report := Report{Operation: "apply", Status: "ok"}
	m.cleanupInto(&report, true)
	if report.Status == "failed" {
		return report
	}

	if err := m.run("iptables", "start"); err != nil {
		report.addFailure("iptables-start", err)
		report.RollbackApplied = true
		m.cleanupInto(&report, true)
		return report
	}
	report.addOK("iptables-start", "")

	if err := m.run("dns", "start"); err != nil {
		report.addFailure("dns-start", err)
		report.RollbackApplied = true
		m.cleanupInto(&report, true)
		return report
	}
	report.addOK("dns-start", "")
	return report
}

func (m Manager) Cleanup() Report {
	report := Report{Operation: "cleanup", Status: "ok"}
	m.cleanupInto(&report, true)
	return report
}

func (m Manager) Verify() Report {
	report := Report{Operation: "verify", Status: "ok"}

	if err := m.run("iptables", "status"); err != nil {
		report.addFailure("iptables-status", err)
		return report
	}
	report.addOK("iptables-status", "")

	if err := m.run("dns", "status"); err != nil {
		report.addFailure("dns-status", err)
		return report
	}
	report.addOK("dns-status", "")
	return report
}

func (m Manager) VerifyCleanup() Report {
	report := Report{Operation: "verify-cleanup", Status: "ok"}
	findings := m.collectCleanupFindings()
	leftovers := findings.leftovers
	report.Leftovers = leftovers
	report.Warnings = findings.warnings
	if len(leftovers) == 0 {
		report.addOK("verify-cleanup", "")
		return report
	}
	detail := strings.Join(leftovers, "; ")
	report.Status = "failed"
	report.Steps = append(report.Steps, Step{
		Name:   "verify-cleanup",
		Status: "failed",
		Detail: detail,
	})
	report.Errors = append(report.Errors, detail)
	return report
}

func (m Manager) CollectLeftovers() []string {
	return m.collectCleanupFindings().leftovers
}

type cleanupFindings struct {
	leftovers []string
	warnings  []string
}

func (m Manager) collectCleanupFindings() cleanupFindings {
	if m.execCommand == nil {
		return cleanupFindings{leftovers: []string{"exec command function is nil"}}
	}

	findings := cleanupFindings{
		leftovers: make([]string, 0),
		warnings:  make([]string, 0),
	}
	add := func(format string, args ...interface{}) {
		findings.leftovers = append(findings.leftovers, fmt.Sprintf(format, args...))
	}
	warn := func(format string, args ...interface{}) {
		findings.warnings = append(findings.warnings, fmt.Sprintf(format, args...))
	}

	for _, spec := range []struct {
		bin   string
		table string
	}{
		{bin: "iptables", table: "raw"},
		{bin: "iptables", table: "mangle"},
		{bin: "iptables", table: "nat"},
		{bin: "iptables", table: "filter"},
		{bin: "ip6tables", table: "raw"},
		{bin: "ip6tables", table: "mangle"},
		{bin: "ip6tables", table: "nat"},
		{bin: "ip6tables", table: "filter"},
		{bin: "iptables-legacy", table: "raw"},
		{bin: "iptables-legacy", table: "mangle"},
		{bin: "iptables-legacy", table: "nat"},
		{bin: "iptables-legacy", table: "filter"},
		{bin: "ip6tables-legacy", table: "raw"},
		{bin: "ip6tables-legacy", table: "mangle"},
		{bin: "ip6tables-legacy", table: "nat"},
		{bin: "ip6tables-legacy", table: "filter"},
		{bin: "iptables-nft", table: "raw"},
		{bin: "iptables-nft", table: "mangle"},
		{bin: "iptables-nft", table: "nat"},
		{bin: "iptables-nft", table: "filter"},
		{bin: "ip6tables-nft", table: "raw"},
		{bin: "ip6tables-nft", table: "mangle"},
		{bin: "ip6tables-nft", table: "nat"},
		{bin: "ip6tables-nft", table: "filter"},
	} {
		out, err := m.execCommand(spec.bin, "-w", "100", "-t", spec.table, "-S")
		if err != nil {
			if isMissingCommandError(err) || isMissingKernelTableOutput(out) {
				continue
			}
			if strings.TrimSpace(out) != "" {
				add("%s %s check failed: %v: %s", spec.bin, spec.table, err, firstLine(out))
			} else {
				add("%s %s check failed: %v", spec.bin, spec.table, err)
			}
			continue
		}
		if line := firstLineContaining(out, "RKNNOVPN"); line != "" {
			add("%s %s rule remains: %s", spec.bin, spec.table, line)
		}
	}

	mark := strings.ToLower(m.envValue("FWMARK"))
	routeTable := m.envValue("ROUTE_TABLE")
	routeTableV6 := m.envValue("ROUTE_TABLE_V6")
	for _, spec := range []struct {
		name  string
		args  []string
		table string
	}{
		{name: "ip rule", args: []string{"rule", "show"}, table: routeTable},
		{name: "ip -6 rule", args: []string{"-6", "rule", "show"}, table: routeTableV6},
	} {
		out, err := m.execCommand("ip", spec.args...)
		if err != nil {
			add("%s check failed: %v", spec.name, err)
			continue
		}
		for _, line := range splitLines(out) {
			if RuleLineMatches(line, mark, spec.table) {
				add("%s remains: %s", spec.name, strings.TrimSpace(line))
				break
			}
		}
	}

	for _, spec := range []struct {
		name string
		args []string
	}{
		{name: "ip route table " + routeTable, args: []string{"route", "show", "table", routeTable}},
		{name: "ip -6 route table " + routeTableV6, args: []string{"-6", "route", "show", "table", routeTableV6}},
	} {
		out, err := m.execCommand("ip", spec.args...)
		if err != nil {
			if isMissingRouteTableOutput(out) {
				continue
			}
			if strings.TrimSpace(out) == "" {
				add("%s check failed: %v", spec.name, err)
			} else {
				add("%s check failed: %v: %s", spec.name, err, firstLine(out))
			}
			continue
		}
		if strings.TrimSpace(out) != "" {
			add("%s still has routes: %s", spec.name, firstLine(out))
		}
	}

	if out, _ := m.execCommand("pidof", "sing-box"); strings.TrimSpace(out) != "" {
		for _, rawPID := range strings.Fields(out) {
			pid, err := strconv.Atoi(rawPID)
			if err != nil || pid <= 0 {
				continue
			}
			owned, ownerDetail := m.processIsOwnedSingBox(pid)
			if owned {
				add("RKNnoVPN sing-box process still running: pid=%d %s", pid, ownerDetail)
			} else {
				warn("other sing-box process detected during cleanup verification: pid=%d %s", pid, ownerDetail)
			}
		}
	}

	for _, port := range m.effectiveLocalPorts() {
		for _, listener := range m.findLocalListeners(port) {
			if listener.PID > 0 {
				owned, ownerDetail := m.processIsOwnedSingBox(listener.PID)
				if owned {
					add("RKNnoVPN %s port %d still listening on %s: pid=%d %s", listener.Proto, port, listener.Address, listener.PID, ownerDetail)
				} else {
					warn("other process owns %s port %d on %s: pid=%d %s", listener.Proto, port, listener.Address, listener.PID, ownerDetail)
				}
				continue
			}
			add("%s port %d still listening on %s (owner unknown)", listener.Proto, port, listener.Address)
		}
	}

	for _, path := range modulecontract.NewPaths(m.dataDir).RuntimeSnapshotFiles() {
		if _, err := os.Stat(path); err == nil {
			add("stale runtime file remains: %s", path)
		}
	}

	return findings
}

func (r Report) Err() error {
	if len(r.Errors) == 0 {
		return nil
	}
	code := "NETSTACK_FAILED"
	for _, step := range r.Steps {
		if step.Status != "failed" {
			continue
		}
		switch step.Name {
		case "iptables-start":
			code = "RULES_NOT_APPLIED"
		case "dns-start":
			code = "DNS_APPLY_FAILED"
		case "dns-stop", "iptables-stop":
			code = "NETSTACK_CLEANUP_FAILED"
		case "dns-status", "iptables-status":
			code = "NETSTACK_VERIFY_FAILED"
		}
		break
	}
	return &Error{Operation: r.Operation, Code: code, Report: r}
}

func (r *Report) addOK(name string, detail string) {
	r.Steps = append(r.Steps, Step{Name: name, Status: "ok", Detail: detail})
}

func (r *Report) addAlreadyClean(name string, detail string) {
	r.Steps = append(r.Steps, Step{Name: name, Status: "already_clean", Detail: detail})
}

func (r *Report) addFailure(name string, err error) {
	r.Status = "failed"
	detail := ""
	if err != nil {
		detail = err.Error()
	}
	r.Steps = append(r.Steps, Step{Name: name, Status: "failed", Detail: detail})
	r.Errors = append(r.Errors, name+": "+detail)
}

func (m Manager) cleanupInto(report *Report, tolerateMissing bool) {
	for _, name := range []string{"dns", "iptables"} {
		stepName := name + "-stop"
		err := m.run(name, "stop")
		switch {
		case err == nil:
			report.addOK(stepName, "")
		case tolerateMissing && isMissingScriptError(err):
			report.addAlreadyClean(stepName, err.Error())
		default:
			report.addFailure(stepName, err)
		}
	}
}

func (m Manager) run(name string, command string) error {
	if m.execScript == nil {
		return fmt.Errorf("exec script function is nil")
	}
	paths := modulecontract.NewPaths(m.dataDir)
	switch name {
	case "dns":
		return m.execScript(paths.DNSScript(), command, m.env)
	case "iptables":
		return m.execScript(paths.IPTablesScript(), command, m.env)
	default:
		return fmt.Errorf("unknown netstack script %q", name)
	}
}

func isMissingScriptError(err error) bool {
	if err == nil {
		return false
	}
	lower := strings.ToLower(err.Error())
	return strings.Contains(lower, "script not found:") ||
		strings.Contains(lower, "no such file or directory")
}

func isMissingKernelTableOutput(output string) bool {
	lower := strings.ToLower(output)
	return strings.Contains(lower, "table does not exist") ||
		strings.Contains(lower, "can't initialize") ||
		strings.Contains(lower, "does not exist")
}

func isMissingCommandError(err error) bool {
	if err == nil {
		return false
	}
	lower := strings.ToLower(err.Error())
	return strings.Contains(lower, "executable file not found") ||
		strings.Contains(lower, "no such file or directory")
}

func isMissingRouteTableOutput(output string) bool {
	lower := strings.ToLower(output)
	return strings.Contains(lower, "fib table does not exist") ||
		strings.Contains(lower, "no such process") ||
		strings.Contains(lower, "no such file")
}

// RuleLineMatches reports whether an `ip rule show` line belongs to the
// RKNnoVPN fwmark/table contract. It intentionally avoids substring matches
// so unrelated marks such as 0x20230 do not look like RKNnoVPN leftovers.
func RuleLineMatches(line string, mark string, table string) bool {
	fields := strings.Fields(strings.ToLower(line))
	wantMark := strings.ToLower(strings.TrimSpace(mark))
	wantTable := strings.TrimSpace(table)
	markOK := false
	tableOK := false
	for i, field := range fields {
		if field == "fwmark" && wantMark != "" && i+1 < len(fields) {
			got := fields[i+1]
			if got == wantMark || strings.HasPrefix(got, wantMark+"/") {
				markOK = true
			}
		}
		if (field == "lookup" || field == "table") && wantTable != "" && i+1 < len(fields) {
			if fields[i+1] == wantTable {
				tableOK = true
			}
		}
	}
	return markOK && tableOK
}

func (m Manager) effectiveLocalPorts() []int {
	ports := []int{
		valueOrDefaultInt(m.envInt("TPROXY_PORT"), 10853),
		valueOrDefaultInt(m.envInt("DNS_PORT"), 10856),
		m.envInt("API_PORT"),
		m.envInt("SOCKS_PORT"),
		m.envInt("HTTP_PORT"),
	}
	for _, field := range strings.Fields(m.envValue("CHAIN_PROXY_PORTS")) {
		port, err := strconv.Atoi(field)
		if err == nil {
			ports = append(ports, port)
		}
	}
	seen := make(map[int]bool, len(ports))
	result := make([]int, 0, len(ports))
	for _, port := range ports {
		if port <= 0 || seen[port] {
			continue
		}
		seen[port] = true
		result = append(result, port)
	}
	return result
}

func (m Manager) envValue(key string) string {
	if m.env == nil {
		return ""
	}
	return strings.TrimSpace(m.env[key])
}

func (m Manager) envInt(key string) int {
	value, _ := strconv.Atoi(m.envValue(key))
	return value
}

func valueOrDefaultInt(value int, fallback int) int {
	if value == 0 {
		return fallback
	}
	return value
}

type localListener struct {
	Proto   string
	Address string
	Port    int
	PID     int
}

func (m Manager) findLocalListeners(port int) []localListener {
	listeners := m.findSSLocalListeners(port)
	if len(listeners) > 0 {
		return listeners
	}
	return m.findProcNetLocalListeners(port)
}

func (m Manager) findSSLocalListeners(port int) []localListener {
	out, err := m.execCommand("ss", "-H", "-lntup")
	if err != nil {
		out, err = m.execCommand("ss", "-lntup")
	}
	if err != nil {
		return nil
	}
	var listeners []localListener
	for _, line := range splitLines(out) {
		listener, ok := parseSSListenerLine(line, port)
		if ok {
			listeners = append(listeners, listener)
		}
	}
	return listeners
}

func parseSSListenerLine(line string, port int) (localListener, bool) {
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return localListener{}, false
	}
	proto := strings.ToLower(fields[0])
	if !strings.HasPrefix(proto, "tcp") && !strings.HasPrefix(proto, "udp") {
		return localListener{}, false
	}
	for _, field := range fields[1:] {
		address, gotPort, ok := parseAddressPortToken(field)
		if !ok || gotPort != port || !isLocalListenerAddress(address) {
			continue
		}
		return localListener{
			Proto:   proto,
			Address: address,
			Port:    gotPort,
			PID:     parseSSPID(line),
		}, true
	}
	return localListener{}, false
}

func parseAddressPortToken(token string) (string, int, bool) {
	token = strings.TrimSpace(token)
	token = strings.Trim(token, "[]")
	if token == "" || strings.HasSuffix(token, ":*") {
		return "", 0, false
	}
	idx := strings.LastIndex(token, ":")
	if idx < 0 || idx == len(token)-1 {
		return "", 0, false
	}
	port, err := strconv.Atoi(token[idx+1:])
	if err != nil {
		return "", 0, false
	}
	address := strings.Trim(token[:idx], "[]")
	if address == "" {
		address = "*"
	}
	return address, port, true
}

func parseSSPID(line string) int {
	const marker = "pid="
	idx := strings.Index(line, marker)
	if idx < 0 {
		return 0
	}
	start := idx + len(marker)
	end := start
	for end < len(line) && line[end] >= '0' && line[end] <= '9' {
		end++
	}
	pid, _ := strconv.Atoi(line[start:end])
	return pid
}

func (m Manager) findProcNetLocalListeners(port int) []localListener {
	var listeners []localListener
	for _, spec := range []struct {
		path  string
		proto string
		ipv6  bool
	}{
		{path: "/proc/net/tcp", proto: "tcp", ipv6: false},
		{path: "/proc/net/tcp6", proto: "tcp6", ipv6: true},
		{path: "/proc/net/udp", proto: "udp", ipv6: false},
		{path: "/proc/net/udp6", proto: "udp6", ipv6: true},
	} {
		out, err := m.execCommand("cat", spec.path)
		if err != nil {
			continue
		}
		for _, line := range splitLines(out) {
			listener, ok := parseProcNetListenerLine(line, spec.proto, spec.ipv6, port)
			if ok {
				listeners = append(listeners, listener)
			}
		}
	}
	return listeners
}

func parseProcNetListenerLine(line string, proto string, ipv6 bool, port int) (localListener, bool) {
	fields := strings.Fields(line)
	if len(fields) < 4 || fields[0] == "sl" {
		return localListener{}, false
	}
	local := fields[1]
	idx := strings.LastIndex(local, ":")
	if idx < 0 || idx == len(local)-1 {
		return localListener{}, false
	}
	gotPort64, err := strconv.ParseInt(local[idx+1:], 16, 32)
	if err != nil || int(gotPort64) != port {
		return localListener{}, false
	}
	state := strings.ToUpper(fields[3])
	if strings.HasPrefix(proto, "tcp") && state != "0A" {
		return localListener{}, false
	}
	addressHex := strings.ToUpper(local[:idx])
	address, ok := procNetLocalAddress(addressHex, ipv6)
	if !ok {
		return localListener{}, false
	}
	return localListener{Proto: proto, Address: address, Port: port}, true
}

func procNetLocalAddress(hexAddress string, ipv6 bool) (string, bool) {
	if !ipv6 {
		switch hexAddress {
		case "00000000":
			return "0.0.0.0", true
		case "0100007F":
			return "127.0.0.1", true
		default:
			return "", false
		}
	}
	switch hexAddress {
	case "00000000000000000000000000000000":
		return "::", true
	case "00000000000000000000000001000000":
		return "::1", true
	default:
		return "", false
	}
}

func isLocalListenerAddress(address string) bool {
	switch strings.ToLower(strings.Trim(address, "[]")) {
	case "*", "0.0.0.0", "::", ":::", "[::]", "127.0.0.1", "::1", "localhost":
		return true
	default:
		return false
	}
}

func (m Manager) processIsOwnedSingBox(pid int) (bool, string) {
	paths := modulecontract.NewPaths(m.dataDir)
	expectedBin := filepathClean(paths.BinDir() + "/sing-box")
	expectedConfig := filepathClean(paths.RenderedConfigDir() + "/singbox.json")
	hotSwapConfig := filepathClean(paths.RenderedConfigDir() + "/singbox.hotswap.json")

	exeOut, exeErr := m.execCommand("readlink", fmt.Sprintf("/proc/%d/exe", pid))
	cmdOut, cmdErr := m.execCommand("cat", fmt.Sprintf("/proc/%d/cmdline", pid))
	exe := filepathClean(strings.TrimSpace(exeOut))
	cmdline := filepathClean(strings.ReplaceAll(cmdOut, "\x00", " "))
	cmdTokens := cleanedCmdlineTokens(cmdOut)

	binOK := exe == expectedBin || stringSliceContains(cmdTokens, expectedBin)
	configOK := stringSliceContains(cmdTokens, expectedConfig) || stringSliceContains(cmdTokens, hotSwapConfig)
	if binOK && configOK {
		return true, fmt.Sprintf("exe=%s config=%s", nonEmpty(exe, "<unknown>"), expectedConfig)
	}

	detailParts := make([]string, 0, 2)
	if exeErr == nil && exe != "" {
		detailParts = append(detailParts, "exe="+exe)
	}
	if cmdErr == nil && cmdline != "" {
		detailParts = append(detailParts, "cmdline="+cmdline)
	}
	if len(detailParts) == 0 {
		return false, "owner unavailable"
	}
	return false, strings.Join(detailParts, " ")
}

func filepathClean(path string) string {
	return strings.TrimRight(strings.TrimSpace(path), "/")
}

func cleanedCmdlineTokens(cmdline string) []string {
	raw := strings.Fields(strings.ReplaceAll(cmdline, "\x00", " "))
	tokens := make([]string, 0, len(raw))
	for _, token := range raw {
		tokens = append(tokens, filepathClean(token))
	}
	return tokens
}

func stringSliceContains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func nonEmpty(value string, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func firstLineContaining(text string, needle string) string {
	for _, line := range splitLines(text) {
		if strings.Contains(line, needle) {
			return strings.TrimSpace(line)
		}
	}
	return ""
}

func firstLine(text string) string {
	for _, line := range splitLines(text) {
		line = strings.TrimSpace(line)
		if line != "" {
			return line
		}
	}
	return ""
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}
