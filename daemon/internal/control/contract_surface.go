package control

import (
	"github.com/youtubediscord/RKNnoVPN/daemon/internal/config"
	"github.com/youtubediscord/RKNnoVPN/daemon/internal/ipc"
)

func addIPCContractFields(target map[string]interface{}) map[string]interface{} {
	target["control_protocol_version"] = ProtocolVersion
	target["schema_version"] = config.CurrentSchemaVersion
	target["ipc_contract_version"] = ipc.ContractVersion()
	target["capabilities"] = ipc.SupportedCapabilities()
	target["supported_methods"] = ipc.SupportedMethods()
	target["apk_required_methods"] = ipc.APKRequiredMethods()
	target["error_codes"] = ipc.ErrorCodes()
	target["compatibility_policies"] = ipc.CompatibilityPolicies()
	target["operation_policies"] = ipc.OperationPolicies()
	target["methods"] = ipc.MethodContracts()
	return target
}
