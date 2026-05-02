package control

import (
	"crypto/sha256"
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"

	"github.com/youtubediscord/RKNnoVPN/daemon/internal/config"
	"github.com/youtubediscord/RKNnoVPN/daemon/internal/ipc"
)

func addIPCContractFields(target map[string]interface{}) map[string]interface{} {
	target["control_protocol_version"] = ProtocolVersion
	target["schema_version"] = config.CurrentSchemaVersion
	target["ipc_contract_version"] = ipc.ContractVersion()
	target["contract_hash"] = ipc.ContractHash()
	target["capabilities"] = ipc.SupportedCapabilities()
	target["supported_methods"] = ipc.SupportedMethods()
	target["apk_required_methods"] = ipc.APKRequiredMethods()
	target["error_codes"] = ipc.ErrorCodes()
	target["compatibility_policies"] = ipc.CompatibilityPolicies()
	target["operation_policies"] = ipc.OperationPolicies()
	target["methods"] = ipc.MethodContracts()
	return target
}

func addCompatibilityIdentityFields(target map[string]interface{}, daemonVersion string, moduleVersion string, daemonPID int, socketInode string) map[string]interface{} {
	target["daemon_pid"] = daemonPID
	target["socket_inode"] = socketInode
	target["compatibility_fingerprint"] = CompatibilityFingerprint(
		daemonVersion,
		moduleVersion,
		ipc.ContractHash(),
		daemonPID,
		socketInode,
	)
	return target
}

func CompatibilityFingerprint(daemonVersion string, moduleVersion string, contractHash string, daemonPID int, socketInode string) string {
	parts := []string{
		strings.TrimSpace(daemonVersion),
		strings.TrimSpace(moduleVersion),
		strconv.Itoa(ProtocolVersion),
		strconv.Itoa(config.CurrentSchemaVersion),
		strconv.Itoa(ipc.ContractVersion()),
		strings.TrimSpace(contractHash),
		strconv.Itoa(daemonPID),
		strings.TrimSpace(socketInode),
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return fmt.Sprintf("%x", sum[:])
}

func DaemonIdentity(socketPath string) (int, string) {
	return os.Getpid(), socketInode(socketPath)
}

func socketInode(socketPath string) string {
	if strings.TrimSpace(socketPath) == "" {
		return ""
	}
	info, err := os.Stat(socketPath)
	if err != nil {
		return ""
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return ""
	}
	return strconv.FormatUint(stat.Ino, 10)
}
