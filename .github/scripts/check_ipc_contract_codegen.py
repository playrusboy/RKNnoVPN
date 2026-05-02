#!/usr/bin/env python3
import argparse
import json
import pathlib
import sys


REPO_ROOT = pathlib.Path(__file__).resolve().parents[2]
MANIFEST = REPO_ROOT / "daemon/internal/ipc/contract_manifest.json"
IPC_PROTOCOL = REPO_ROOT / "daemon/internal/ipc/protocol.go"
IPC_SERVER = REPO_ROOT / "daemon/internal/ipc/server.go"
OUTPUT = REPO_ROOT / "app/app/src/main/kotlin/com/rknnovpn/panel/ipc/GeneratedDaemonContract.kt"
DAEMON_CLIENT = REPO_ROOT / "app/app/src/main/kotlin/com/rknnovpn/panel/ipc/DaemonClient.kt"
DAEMON_CLIENT_RESULT = REPO_ROOT / "app/app/src/main/kotlin/com/rknnovpn/panel/ipc/DaemonClientResult.kt"
DAEMONCTL_EXECUTOR = REPO_ROOT / "app/app/src/main/kotlin/com/rknnovpn/panel/ipc/DaemonctlExecutor.kt"
DAEMON_CONTROL_WIRING = REPO_ROOT / "daemon/cmd/daemon/control_wiring.go"
DAEMON_DIAGNOSTICS_CONTROL_WIRING = REPO_ROOT / "daemon/cmd/daemon/diagnostics_control_wiring.go"
DAEMON_RUNTIME_CONTROL_WIRING = REPO_ROOT / "daemon/cmd/daemon/runtime_control_wiring.go"
DAEMON_RUNTIME_DESIRED = REPO_ROOT / "daemon/cmd/daemon/runtime_v2_desired.go"
DAEMON_UPDATE_CONTROL_WIRING = REPO_ROOT / "daemon/cmd/daemon/update_control_wiring.go"
CONTROL_REGISTRY = REPO_ROOT / "daemon/internal/control/registry.go"
CONTROL_MUTATION_ENVELOPE = REPO_ROOT / "daemon/internal/control/mutation_envelope.go"
CONTROL_PROFILE_HANDLERS = REPO_ROOT / "daemon/internal/control/profile_handlers.go"
CONTROL_DIAGNOSTICS_HANDLERS = REPO_ROOT / "daemon/internal/control/diagnostics_handlers.go"
CONTROL_RUNTIME_ERRORS = REPO_ROOT / "daemon/internal/control/runtime_errors.go"
CONTROL_UPDATE_HANDLERS = REPO_ROOT / "daemon/internal/control/update_handlers.go"
RUNTIME_ERROR_HELPERS = REPO_ROOT / "daemon/internal/runtimeerr/errors.go"
ROOT_RUNTIME_ERROR_HELPERS = REPO_ROOT / "daemon/internal/runtime/root/errors.go"
DAEMON_CLIENT_MODELS = REPO_ROOT / "app/app/src/main/kotlin/com/rknnovpn/panel/ipc/DaemonClientModels.kt"
DAEMON_RESPONSE_PARSERS = REPO_ROOT / "app/app/src/main/kotlin/com/rknnovpn/panel/ipc/DaemonResponseParsers.kt"
USER_MESSAGE_FORMATTER = REPO_ROOT / "app/app/src/main/kotlin/com/rknnovpn/panel/i18n/UserMessageFormatter.kt"
APPLY_TRANSACTION = REPO_ROOT / "daemon/internal/apply/transaction.go"
RUNTIME_STATE_STORE = REPO_ROOT / "daemon/internal/runtimev2/state_store.go"
INSTALL_STATE_STORE = REPO_ROOT / "daemon/internal/updater/install_state.go"
INSTALL_TRANSACTION = REPO_ROOT / "daemon/internal/updater/install_transaction.go"
CONTROL_RUNTIME_HANDLERS = REPO_ROOT / "daemon/internal/control/runtime_handlers.go"
APP_KOTLIN_ROOT = REPO_ROOT / "app/app/src/main/kotlin"
BOOTSTRAP_METHODS = {"backend.status", "compat.check", "ipc.contract", "version"}
CONFIG_TRANSACTION_REQUIRED_STAGES = {
    "validate",
    "render",
    "persist-draft",
    "runtime-apply",
    "verify",
    "commit-generation",
    "cleanup",
}
NON_ASYNC_MUTATING_RESULT_SURFACES = {
    "backend.applyDesiredState": "runtime-status",
    "update-download": "operation-envelope",
}


def validate_manifest(source: dict) -> list[str]:
    errors: list[str] = []
    error_codes = source.get("errorCodes", [])
    if not isinstance(error_codes, list) or not error_codes:
        errors.append("errorCodes must be a non-empty list")
        error_codes = []
    error_code_set = set(error_codes)
    if len(error_code_set) != len(error_codes):
        errors.append("errorCodes contains duplicates")
    compatibility_policies = source.get("compatibilityPolicies", [])
    if not isinstance(compatibility_policies, list) or not compatibility_policies:
        errors.append("compatibilityPolicies must be a non-empty list")
        compatibility_policies = []
    compatibility_policy_set = set(compatibility_policies)
    if len(compatibility_policy_set) != len(compatibility_policies):
        errors.append("compatibilityPolicies contains duplicates")
    if "" in compatibility_policy_set:
        errors.append("compatibilityPolicies must not contain an empty policy")
    request_examples = source.get("requestExamples", {})
    if not isinstance(request_examples, dict):
        errors.append("requestExamples must be an object")
        request_examples = {}
    operation_policies = source.get("operationPolicies", {})
    if not isinstance(operation_policies, dict) or not operation_policies:
        errors.append("operationPolicies must be a non-empty object")
        operation_policies = {}
    for operation_type, policy in operation_policies.items():
        if not operation_type:
            errors.append("operationPolicies contains an empty operation type")
        if not isinstance(policy, dict):
            errors.append(f"operation policy {operation_type} must be an object")
            policy = {}
        stages = policy.get("stages", [])
        if not stages:
            errors.append(f"operation policy {operation_type} is missing stages")
        elif len(set(stages)) != len(stages):
            errors.append(f"operation policy {operation_type} contains duplicate stages")
        policy_error_codes = policy.get("errorCodes", []) if isinstance(policy, dict) else []
        if not policy_error_codes:
            errors.append(f"operation policy {operation_type} is missing error codes")
        elif len(set(policy_error_codes)) != len(policy_error_codes):
            errors.append(f"operation policy {operation_type} contains duplicate error codes")
        for code in policy_error_codes:
            if code not in error_code_set:
                errors.append(f"operation policy {operation_type} references undeclared error code {code}")
    capabilities = source.get("capabilities", [])
    if not isinstance(capabilities, list):
        errors.append("capabilities must be a list")
        capabilities = []
    capability_set = set(capabilities)
    required_methods = set(source.get("apkRequiredMethods", []))
    methods = source.get("methods", [])
    if not isinstance(methods, list):
        return errors + ["methods must be a list"]
    seen_methods: set[str] = set()
    used_operation_policies: set[str] = set()
    for item in methods:
        method = item.get("method", "")
        if not method:
            errors.append("method entry is missing method")
            continue
        if method in seen_methods:
            errors.append(f"duplicate IPC method {method}")
        seen_methods.add(method)
        capability = item.get("capability", "")
        if not capability:
            errors.append(f"{method} is missing capability")
        elif capability not in capability_set:
            errors.append(f"{method} capability {capability} is not declared in capabilities")
        compatibility = item.get("compatibility", "")
        if compatibility and compatibility not in compatibility_policy_set:
            errors.append(f"{method} compatibility policy {compatibility} is not declared in compatibilityPolicies")
        if not item.get("request"):
            errors.append(f"{method} is missing request schema")
        if not item.get("result"):
            errors.append(f"{method} is missing result schema")
        declared_error_codes = item.get("errorCodes", [])
        operation = item.get("operation")
        if item.get("mutating") and not operation:
            errors.append(f"{method} is mutating but missing operation metadata")
        if item.get("async") and (not operation or not operation.get("asyncResultVia")):
            errors.append(f"{method} is async but missing operation.asyncResultVia")
        if operation:
            operation_type = operation.get("type", "")
            if not operation_type:
                errors.append(f"{method} operation is missing type")
            stages = operation.get("stages", [])
            policy = operation_policies.get(operation_type)
            if operation_type and not policy:
                errors.append(f"{method} operation type {operation_type} has no policy in manifest")
            if not stages and not policy:
                errors.append(f"{method} operation is missing stages")
            if policy:
                if stages and stages != policy.get("stages", []):
                    expected = ", ".join(policy.get("stages", []))
                    actual = ", ".join(stages)
                    errors.append(f"{method} operation stages drifted: got [{actual}], expected [{expected}]")
                if declared_error_codes and declared_error_codes != policy.get("errorCodes", []):
                    expected = ", ".join(policy.get("errorCodes", []))
                    actual = ", ".join(declared_error_codes)
                    errors.append(f"{method} error codes drifted: got [{actual}], expected [{expected}]")
                used_operation_policies.add(operation_type)
            error_codes = policy.get("errorCodes", []) if policy else declared_error_codes
        else:
            error_codes = declared_error_codes
        if not error_codes:
            errors.append(f"{method} is missing error codes")
        for code in error_codes:
            if code not in error_code_set:
                errors.append(f"{method} declares unknown error code {code}")
    for operation_type in sorted(set(operation_policies) - used_operation_policies):
        errors.append(f"operation policy {operation_type} is not used by any method")
    for method in sorted(required_methods - seen_methods):
        errors.append(f"apkRequiredMethods contains undeclared method {method}")
    errors.extend(validate_request_examples(source, request_examples))
    errors.extend(check_public_error_code_surface(error_codes))
    errors.extend(check_config_transaction_actions(source))
    errors.extend(check_non_async_mutating_surfaces(source))
    errors.extend(check_update_download_operation_surface(source))
    errors.extend(check_state_file_surfaces())
    errors.extend(check_apk_error_code_surface())
    errors.extend(check_profile_operation_surface())
    errors.extend(check_diagnostics_state_surface())
    errors.extend(check_runtime_error_surface())
    errors.extend(check_method_not_found_surface(source))
    return errors


def check_public_error_code_surface(error_codes: list[str]) -> list[str]:
    if not IPC_PROTOCOL.exists():
        return [f"{IPC_PROTOCOL.relative_to(REPO_ROOT)} is missing"]
    protocol = IPC_PROTOCOL.read_text(encoding="utf-8")
    errors: list[str] = []
    required_snippets = [
        "func PublicErrorCodeNames() []string",
        "var publicErrorCodeNames = []string{",
        "var rpcErrorCodeNames = map[int]string{",
        "ErrorNameResetInProgress",
    ]
    for snippet in required_snippets:
        if snippet not in protocol:
            errors.append(f"public error code surface missing {snippet!r} in {IPC_PROTOCOL.relative_to(REPO_ROOT)}")
    for code in error_codes:
        const_name = "ErrorName" + "".join(part.title() for part in code.lower().split("_"))
        if const_name not in protocol:
            errors.append(f"public error code {code} is missing Go constant {const_name}")
    if "CodeProxyNotRunning" in protocol or "CodeProxyAlready" in protocol:
        errors.append("legacy proxy RPC error codes must not be restored")
    return errors


def validate_request_examples(source: dict, request_examples: dict) -> list[str]:
    manifest_requests = {item.get("request") for item in source.get("methods", []) if item.get("request")}
    manifest_requests.add("empty")
    errors: list[str] = []
    for request, example in sorted(request_examples.items()):
        if not request:
            errors.append("requestExamples contains an empty request schema")
        if request not in manifest_requests:
            errors.append(f"requestExamples contains undeclared request schema {request}")
        try:
            json.dumps(example)
        except TypeError as exc:
            errors.append(f"requestExamples {request} is not JSON-serializable: {exc}")
    return errors


def check_config_transaction_actions(source: dict) -> list[str]:
    if not APPLY_TRANSACTION.exists():
        return [f"{APPLY_TRANSACTION.relative_to(REPO_ROOT)} is missing"]
    transaction = APPLY_TRANSACTION.read_text(encoding="utf-8")
    mapped_operation_types = set(_go_switch_cases(transaction, "RuntimeOperationForType"))
    transaction_operation_types = _config_transaction_operation_types(source)
    expected_operation_types = {
        item.get("operation", {}).get("type")
        for item in source.get("methods", [])
        if item.get("operation", {}).get("type") in transaction_operation_types
    }
    return [
        f"RuntimeOperationForType is missing config transaction operation type {operation_type}"
        for operation_type in sorted(expected_operation_types - mapped_operation_types)
    ]


def check_non_async_mutating_surfaces(source: dict) -> list[str]:
    methods = {
        item.get("method")
        for item in source.get("methods", [])
        if item.get("method") and item.get("mutating") and not item.get("async")
    }
    expected = set(NON_ASYNC_MUTATING_RESULT_SURFACES)
    errors: list[str] = []
    for method in sorted(methods - expected):
        errors.append(
            f"non-async mutating method {method} must declare an explicit result surface in NON_ASYNC_MUTATING_RESULT_SURFACES"
        )
    for method in sorted(expected - methods):
        errors.append(
            f"NON_ASYNC_MUTATING_RESULT_SURFACES contains {method}, but it is not a non-async mutating IPC method"
        )
    for method, surface in sorted(NON_ASYNC_MUTATING_RESULT_SURFACES.items()):
        if surface not in {"runtime-status", "operation-envelope"}:
            errors.append(f"non-async mutating method {method} has unknown result surface {surface}")
    return errors


def check_update_download_operation_surface(source: dict) -> list[str]:
    update_download = next(
        (item for item in source.get("methods", []) if item.get("method") == "update-download"),
        None,
    )
    if not update_download or update_download.get("operation", {}).get("type") != "update-download":
        return []
    errors: list[str] = []
    required_files = [
        CONTROL_MUTATION_ENVELOPE,
        CONTROL_UPDATE_HANDLERS,
        DAEMON_CLIENT_MODELS,
        DAEMON_RESPONSE_PARSERS,
    ]
    for path in required_files:
        if not path.exists():
            errors.append(f"{path.relative_to(REPO_ROOT)} is missing")
    if errors:
        return errors
    envelope = CONTROL_MUTATION_ENVELOPE.read_text(encoding="utf-8")
    handlers = CONTROL_UPDATE_HANDLERS.read_text(encoding="utf-8")
    models = DAEMON_CLIENT_MODELS.read_text(encoding="utf-8")
    parsers = DAEMON_RESPONSE_PARSERS.read_text(encoding="utf-8")
    required_snippets = {
        CONTROL_MUTATION_ENVELOPE: [
            'const operationType = "update-download"',
            "func updateDownloadOperation(",
            "func updateDownloadFailedStage(",
        ],
        CONTROL_UPDATE_HANDLERS: [
            "func updateDownloadResult(",
            "func updateDownloadErrorData(",
            '"operation": updateDownloadOperation("downloaded"',
            '"operation": updateDownloadOperation("failed"',
        ],
        DAEMON_CLIENT_MODELS: [
            "data class UpdateDownloadInfo(",
            "val operation: JsonElement? = null",
        ],
        DAEMON_RESPONSE_PARSERS: [
            "internal fun parseUpdateDownloadInfo(",
            'operation = obj["operation"]',
        ],
    }
    sources = {
        CONTROL_MUTATION_ENVELOPE: envelope,
        CONTROL_UPDATE_HANDLERS: handlers,
        DAEMON_CLIENT_MODELS: models,
        DAEMON_RESPONSE_PARSERS: parsers,
    }
    for path, snippets in required_snippets.items():
        source = sources[path]
        for snippet in snippets:
            if snippet not in source:
                errors.append(
                    f"update-download operation surface missing {snippet!r} in {path.relative_to(REPO_ROOT)}"
                )
    return errors


def check_state_file_surfaces() -> list[str]:
    errors: list[str] = []
    required_files = [RUNTIME_STATE_STORE, INSTALL_STATE_STORE, CONTROL_RUNTIME_HANDLERS]
    for path in required_files:
        if not path.exists():
            errors.append(f"{path.relative_to(REPO_ROOT)} is missing")
    if errors:
        return errors
    runtime_store = RUNTIME_STATE_STORE.read_text(encoding="utf-8")
    install_store = INSTALL_STATE_STORE.read_text(encoding="utf-8")
    runtime_handlers = CONTROL_RUNTIME_HANDLERS.read_text(encoding="utf-8")
    required_snippets = {
        RUNTIME_STATE_STORE: [
            'const runtimeStateFileName = "runtime_state.json"',
            "func RuntimeStatePath(",
            "func WriteRuntimeState(",
            "func ReadRuntimeState(",
        ],
        INSTALL_STATE_STORE: [
            'const installStateFileName = "install_state.json"',
            "func InstallStatePath(",
            "func NewInstallTracker(",
            "func NewDownloadTracker(",
            "func ReadInstallState(",
            "InstallStatePath(dataDir)",
        ],
        CONTROL_RUNTIME_HANDLERS: [
            "func (h RuntimeHandlers) statusWithUpdateInstallState(",
            "updater.ReadInstallState(h.DataDir)",
            "status.UpdateInstall = &runtimev2.UpdateInstallState{",
            'Status: "unknown"',
            'Code:   "UPDATE_INSTALL_STATE_INVALID"',
        ],
        CONTROL_DIAGNOSTICS_HANDLERS: [
            '"runtime_state":     diagnostics.StatFile(runtimev2.RuntimeStatePath(dataDir), false)',
            '"install_state":     diagnostics.StatFile(updater.InstallStatePath(dataDir), false)',
        ],
    }
    sources = {
        RUNTIME_STATE_STORE: runtime_store,
        INSTALL_STATE_STORE: install_store,
        CONTROL_RUNTIME_HANDLERS: runtime_handlers,
        CONTROL_DIAGNOSTICS_HANDLERS: CONTROL_DIAGNOSTICS_HANDLERS.read_text(encoding="utf-8") if CONTROL_DIAGNOSTICS_HANDLERS.exists() else "",
    }
    for path, snippets in required_snippets.items():
        source = sources[path]
        for snippet in snippets:
            if snippet not in source:
                errors.append(f"state file surface missing {snippet!r} in {path.relative_to(REPO_ROOT)}")
    return errors


def check_apk_error_code_surface() -> list[str]:
    errors: list[str] = []
    required_files = [DAEMON_CLIENT_RESULT, DAEMON_CLIENT, USER_MESSAGE_FORMATTER]
    for path in required_files:
        if not path.exists():
            errors.append(f"{path.relative_to(REPO_ROOT)} is missing")
    if errors:
        return errors
    client_result = DAEMON_CLIENT_RESULT.read_text(encoding="utf-8")
    client = DAEMON_CLIENT.read_text(encoding="utf-8")
    formatter = USER_MESSAGE_FORMATTER.read_text(encoding="utf-8")
    required_snippets = {
        DAEMON_CLIENT_RESULT: [
            "internal object DaemonClientErrorCodes",
            "const val CONFIG_ERROR = -32003",
            "const val RUNTIME_BUSY = -32004",
            "const val COMPATIBILITY = -32090",
        ],
        DAEMON_CLIENT: [
            "DaemonClientErrorCodes.CONFIG_ERROR",
            "DaemonClientErrorCodes.COMPATIBILITY",
        ],
        USER_MESSAGE_FORMATTER: [
            "import com.rknnovpn.panel.ipc.DaemonClientErrorCodes",
            "DaemonClientErrorCodes.COMPATIBILITY",
            "DaemonClientErrorCodes.RUNTIME_BUSY",
        ],
    }
    sources = {
        DAEMON_CLIENT_RESULT: client_result,
        DAEMON_CLIENT: client,
        USER_MESSAGE_FORMATTER: formatter,
    }
    for path, snippets in required_snippets.items():
        source = sources[path]
        for snippet in snippets:
            if snippet not in source:
                errors.append(f"APK error code surface missing {snippet!r} in {path.relative_to(REPO_ROOT)}")
    for path in [DAEMON_CLIENT, USER_MESSAGE_FORMATTER]:
        source = sources[path]
        for literal in ["-32003", "-32004", "-32090"]:
            if literal in source:
                errors.append(f"APK error code literal {literal} must live in DaemonClientErrorCodes, not {path.relative_to(REPO_ROOT)}")
    return errors


def check_profile_operation_surface() -> list[str]:
    errors: list[str] = []
    removed_daemon_file = REPO_ROOT / "daemon/cmd/daemon/profile_apply.go"
    if removed_daemon_file.exists():
        errors.append("daemon/cmd/daemon/profile_apply.go must not be restored; profile apply ownership lives in internal/control")
    required_files = [CONTROL_PROFILE_HANDLERS, CONTROL_MUTATION_ENVELOPE]
    for path in required_files:
        if not path.exists():
            errors.append(f"{path.relative_to(REPO_ROOT)} is missing")
    if errors:
        return errors
    profile_handlers = CONTROL_PROFILE_HANDLERS.read_text(encoding="utf-8")
    envelope = CONTROL_MUTATION_ENVELOPE.read_text(encoding="utf-8")
    required_snippets = {
        CONTROL_PROFILE_HANDLERS: [
            "CurrentConfig         CurrentConfigFunc",
            "PersistConfigMutation PersistConfigMutationFunc",
            "RuntimeStatus         RuntimeStatusFunc",
            "profiledoc.FromConfig(current)",
            "ProfileValidationRPCError(",
            "ProfileRPCErrorSaved(",
            "ProfileSuccess(",
        ],
        CONTROL_MUTATION_ENVELOPE: [
            "func ProfileValidationRPCError(",
            "func ProfileRPCErrorSaved(",
            "func ProfileSuccess(",
            "func ProfileDesiredGeneration(",
            'github.com/youtubediscord/RKNnoVPN/daemon/internal/runtimeerr',
            'runtimeerr.Code(err, "PROFILE_APPLY_FAILED")',
            "runtimeerr.ResetReportFromError(err)",
        ],
    }
    sources = {
        CONTROL_PROFILE_HANDLERS: profile_handlers,
        CONTROL_MUTATION_ENVELOPE: envelope,
    }
    for path, snippets in required_snippets.items():
        source = sources[path]
        for snippet in snippets:
            if snippet not in source:
                errors.append(f"profile operation surface missing {snippet!r} in {path.relative_to(REPO_ROOT)}")
    forbidden = [
        "control.ProfileOperation(",
        "rootruntime.RuntimeErrorCode(",
        "rootruntime.ResetReportFromError(",
        "func desiredGeneration(",
        "CurrentProfileFunc",
        "CurrentProfile",
        "ApplyProfileFunc",
        "ApplyProfile       ApplyProfileFunc",
    ]
    for snippet in forbidden:
        if snippet in profile_handlers:
            errors.append(f"profile handlers must not use legacy profile apply callback/envelope wrapper: found {snippet!r}")
    return errors


def check_runtime_error_surface() -> list[str]:
    errors: list[str] = []
    if ROOT_RUNTIME_ERROR_HELPERS.exists():
        errors.append("daemon/internal/runtime/root/errors.go must not be restored; shared runtime error helpers live in internal/runtimeerr")
    required_files = [
        CONTROL_RUNTIME_ERRORS,
        RUNTIME_ERROR_HELPERS,
        DAEMON_RUNTIME_CONTROL_WIRING,
        DAEMON_RUNTIME_DESIRED,
        DAEMON_UPDATE_CONTROL_WIRING,
    ]
    for path in required_files:
        if not path.exists():
            errors.append(f"{path.relative_to(REPO_ROOT)} is missing")
    if errors:
        return errors
    runtime_errors = CONTROL_RUNTIME_ERRORS.read_text(encoding="utf-8")
    runtime_error_helpers = RUNTIME_ERROR_HELPERS.read_text(encoding="utf-8")
    runtime_wiring = DAEMON_RUNTIME_CONTROL_WIRING.read_text(encoding="utf-8")
    runtime_desired = DAEMON_RUNTIME_DESIRED.read_text(encoding="utf-8")
    update_wiring = DAEMON_UPDATE_CONTROL_WIRING.read_text(encoding="utf-8")
    required_snippets = {
        CONTROL_RUNTIME_ERRORS: [
            "func RuntimeRPCError(",
            "type DesiredStateApplyError struct",
            "func DesiredStateApplyRPCError(",
            "runtimev2.OperationBusyError",
            "ipc.CodeRuntimeBusy",
            "ipc.CodeConfigError",
        ],
        RUNTIME_ERROR_HELPERS: [
            "func Code(err error, fallback string) string",
            "func WithResetReport(",
            "func ResetReportFromError(",
            "runtimev2.OperationBusyError",
            "netstack.Error",
        ],
        CONTROL_RUNTIME_HANDLERS: [
            "RuntimeRPCError(err)",
            "DesiredStateApplyRPCError(err)",
        ],
        CONTROL_UPDATE_HANDLERS: [
            "RuntimeRPCError(err)",
            "RuntimeRPCError(runtimev2.NewRuntimeBusyError(*status.ActiveOperation))",
        ],
        DAEMON_RUNTIME_DESIRED: [
            "control.DesiredStateApplyError{",
            "control.DesiredStateApplyStageValidate",
            "control.DesiredStateApplyStagePersist",
        ],
    }
    sources = {
        CONTROL_RUNTIME_ERRORS: runtime_errors,
        RUNTIME_ERROR_HELPERS: runtime_error_helpers,
        CONTROL_RUNTIME_HANDLERS: CONTROL_RUNTIME_HANDLERS.read_text(encoding="utf-8") if CONTROL_RUNTIME_HANDLERS.exists() else "",
        CONTROL_UPDATE_HANDLERS: CONTROL_UPDATE_HANDLERS.read_text(encoding="utf-8") if CONTROL_UPDATE_HANDLERS.exists() else "",
        DAEMON_RUNTIME_CONTROL_WIRING: runtime_wiring,
        DAEMON_RUNTIME_DESIRED: runtime_desired,
        DAEMON_UPDATE_CONTROL_WIRING: update_wiring,
    }
    for path, snippets in required_snippets.items():
        source = sources[path]
        for snippet in snippets:
            if snippet not in source:
                errors.append(f"runtime error surface missing {snippet!r} in {path.relative_to(REPO_ROOT)}")
    if "func (d *daemon) rpcErrorFromRuntimeError(" in runtime_wiring:
        errors.append("daemon runtime control wiring must not own generic runtime RPC error mapping")
    forbidden_sources = {
        CONTROL_RUNTIME_HANDLERS: CONTROL_RUNTIME_HANDLERS.read_text(encoding="utf-8") if CONTROL_RUNTIME_HANDLERS.exists() else "",
        CONTROL_UPDATE_HANDLERS: CONTROL_UPDATE_HANDLERS.read_text(encoding="utf-8") if CONTROL_UPDATE_HANDLERS.exists() else "",
        DAEMON_RUNTIME_CONTROL_WIRING: runtime_wiring,
        DAEMON_RUNTIME_DESIRED: runtime_desired,
        DAEMON_UPDATE_CONTROL_WIRING: update_wiring,
    }
    for path, source in forbidden_sources.items():
        for snippet in [
            "RuntimeError ",
            "RuntimeError:",
            "h.runtimeError(",
            "func (h RuntimeHandlers) runtimeError(",
            "func (h UpdateHandlers) runtimeError(",
            "RuntimeErrorCode              func(",
            "DesiredStateApplyError:",
            "func (d *daemon) rpcErrorFromDesiredStateApplyError(",
            "type desiredStateApplyError struct",
        ]:
            if snippet in source:
                errors.append(f"{path.relative_to(REPO_ROOT)} must not own desired-state RPC error mapping: found {snippet!r}")
    if 'github.com/youtubediscord/RKNnoVPN/daemon/internal/runtime/root' in sources[CONTROL_UPDATE_HANDLERS]:
        errors.append("control update handlers must not import root runtime; root-specific hooks belong in daemon wiring")
    if 'github.com/youtubediscord/RKNnoVPN/daemon/internal/runtime/root' in CONTROL_MUTATION_ENVELOPE.read_text(encoding="utf-8"):
        errors.append("control mutation envelope must use runtimeerr, not root runtime")
    if INSTALL_TRANSACTION.exists():
        install_transaction = INSTALL_TRANSACTION.read_text(encoding="utf-8")
        if 'github.com/youtubediscord/RKNnoVPN/daemon/internal/runtimeerr' not in install_transaction:
            errors.append("update install transaction must use runtimeerr for stable stop-runtime error codes")
        for snippet in ["RuntimeErrorCode              func(", "installRuntimeErrorCode("]:
            if snippet in install_transaction:
                errors.append(f"update install transaction must not keep runtime error callback wrapper {snippet!r}")
    return errors


def check_method_not_found_surface(source: dict) -> list[str]:
    if not IPC_SERVER.exists():
        return [f"{IPC_SERVER.relative_to(REPO_ROOT)} is missing"]
    server = IPC_SERVER.read_text(encoding="utf-8")
    manifest_methods = {item.get("method") for item in source.get("methods", []) if item.get("method")}
    errors: list[str] = []
    required_snippets = [
        "func replacedMethodHint(method string) string",
        '"requestedMethod":  req.Method',
        '"supportedMethods": SupportedMethods()',
        'data["replacement"] = replacement',
        "CodeMethodNotFound",
    ]
    for snippet in required_snippets:
        if snippet not in server:
            errors.append(f"IPC method-not-found surface missing {snippet!r} in {IPC_SERVER.relative_to(REPO_ROOT)}")
    legacy_hints = {
        "config.import": "config-import",
        "network.reset": "backend.reset",
        "node.test": "diagnostics.testNodes",
        "self.check": "self-check",
        "status": "backend.status",
        "start": "backend.start",
        "stop": "backend.stop",
        "reload": "backend.restart",
        "health": "diagnostics.health",
        "subscription-fetch": "subscription.refresh",
    }
    for legacy, canonical in sorted(legacy_hints.items()):
        if legacy in manifest_methods:
            errors.append(f"legacy IPC alias {legacy} must not be declared as a contract method")
        if canonical not in manifest_methods:
            errors.append(f"legacy IPC hint for {legacy} points at undeclared canonical method {canonical}")
        if f'case "{legacy}":' not in server:
            errors.append(f"IPC method-not-found hint missing legacy case {legacy}")
        if canonical not in server:
            errors.append(f"IPC method-not-found hint for {legacy} must mention canonical method {canonical}")
    return errors


def check_diagnostics_state_surface() -> list[str]:
    errors: list[str] = []
    required_files = [CONTROL_DIAGNOSTICS_HANDLERS, DAEMON_DIAGNOSTICS_CONTROL_WIRING]
    for path in required_files:
        if not path.exists():
            errors.append(f"{path.relative_to(REPO_ROOT)} is missing")
    if errors:
        return errors
    handlers = CONTROL_DIAGNOSTICS_HANDLERS.read_text(encoding="utf-8")
    wiring = DAEMON_DIAGNOSTICS_CONTROL_WIRING.read_text(encoding="utf-8")
    required_snippets = {
        CONTROL_DIAGNOSTICS_HANDLERS: [
            "CurrentConfig         CurrentConfigFunc",
            "RuntimeStatus         RuntimeStatusFunc",
            "RefreshRuntimeHealth  func() runtimev2.HealthSnapshot",
            "func (h DiagnosticsHandlers) DiagnosticsHealth(",
            "func (h DiagnosticsHandlers) DiagnosticsTestNodes(",
            "profiledoc.Path(h.ConfigPath)",
            "ConfigPath:  h.ConfigPath",
            "DataDir:     h.DataDir",
        ],
        DAEMON_DIAGNOSTICS_CONTROL_WIRING: [
            "ConfigPath:            d.cfgPath",
            "ProfilePath:           d.profilePath",
            "DataDir:               d.dataDir",
            "CurrentConfig:         d.currentConfig",
            "RefreshRuntimeHealth:  d.runtimeV2.RefreshHealth",
        ],
    }
    sources = {
        CONTROL_DIAGNOSTICS_HANDLERS: handlers,
        DAEMON_DIAGNOSTICS_CONTROL_WIRING: wiring,
    }
    for path, snippets in required_snippets.items():
        source = sources[path]
        for snippet in snippets:
            if snippet not in source:
                errors.append(f"diagnostics state surface missing {snippet!r} in {path.relative_to(REPO_ROOT)}")
    for path, source in sources.items():
        for snippet in ["CurrentState", "func() control.DiagnosticsState"]:
            if snippet in source:
                errors.append(f"{path.relative_to(REPO_ROOT)} must not use diagnostics CurrentState callback wrapper")
    registry = CONTROL_REGISTRY.read_text(encoding="utf-8") if CONTROL_REGISTRY.exists() else ""
    for snippets in [
        (
            '"diagnostics.health":        g.Diagnostics.DiagnosticsHealth',
            '"diagnostics.health":        ipc.WithoutContext(g.Diagnostics.DiagnosticsHealth)',
            '"diagnostics.health":         g.Diagnostics.DiagnosticsHealthContext',
        ),
        (
            '"diagnostics.testNodes":     g.Diagnostics.DiagnosticsTestNodes',
            '"diagnostics.testNodes":     ipc.WithoutContext(g.Diagnostics.DiagnosticsTestNodes)',
            '"diagnostics.testNodes":      g.Diagnostics.DiagnosticsTestNodesContext',
        ),
    ]:
        if not any(snippet in registry for snippet in snippets):
            errors.append(f"diagnostics IPC methods must be owned by DiagnosticsHandlers: missing one of {snippets!r}")
    runtime_handlers = CONTROL_RUNTIME_HANDLERS.read_text(encoding="utf-8") if CONTROL_RUNTIME_HANDLERS.exists() else ""
    for snippet in ["DiagnosticsHealth", "DiagnosticsTestNodes", "TestNodes             func("]:
        if snippet in runtime_handlers:
            errors.append(f"runtime handlers must not own diagnostics IPC endpoint {snippet!r}")
    audit_handlers = (REPO_ROOT / "daemon/internal/control/audit_handlers.go").read_text(encoding="utf-8")
    if "CurrentConfig CurrentConfigFunc" not in audit_handlers:
        errors.append("audit handlers must use CurrentConfigFunc for shared config callback contract")
    return errors


def _config_transaction_operation_types(source: dict) -> set[str]:
    operation_policies = source.get("operationPolicies", {})
    if not isinstance(operation_policies, dict):
        return set()
    return {
        operation_type
        for operation_type, policy in operation_policies.items()
        if isinstance(policy, dict)
        and CONFIG_TRANSACTION_REQUIRED_STAGES.issubset(set(policy.get("stages", [])))
    }


def render_contract(source: dict) -> str:
    version = int(source.get("version", 0))
    required = sorted(source.get("apkRequiredMethods", []))
    contracts = sorted(completed_method_contracts(source), key=lambda item: item.get("method", ""))
    error_codes = list(source.get("errorCodes", []))
    compatibility_policies = list(source.get("compatibilityPolicies", []))
    operation_policies = source.get("operationPolicies", {})
    methods = {item.get("method") for item in contracts}
    missing = [method for method in required if method not in methods]
    if missing:
        raise SystemExit(f"apkRequiredMethods contains undeclared method(s): {', '.join(missing)}")
    contract_error_codes_body = ", ".join(json.dumps(code) for code in error_codes)
    compatibility_policies_body = ", ".join(json.dumps(policy) for policy in compatibility_policies)
    operation_policies_body = "\n".join(
        "        "
        f'{json.dumps(operation_type)} to OperationPolicy(\n'
        f'            stages = listOf({", ".join(json.dumps(stage) for stage in policy.get("stages", []))}),\n'
        f'            errorCodes = listOf({", ".join(json.dumps(code) for code in policy.get("errorCodes", []))}),\n'
        "        ),"
        for operation_type, policy in sorted(operation_policies.items())
        if isinstance(policy, dict)
    )
    required_body = "\n".join(f'        "{method}",' for method in required)
    capabilities_body = "\n".join(
        f'        "{item.get("method", "")}" to "{item.get("capability", "")}",'
        for item in contracts
        if item.get("method")
    )
    mutating_body = "\n".join(
        f'        "{item.get("method", "")}",'
        for item in contracts
        if item.get("method") and item.get("mutating")
    )
    async_body = "\n".join(
        f'        "{item.get("method", "")}",'
        for item in contracts
        if item.get("method") and item.get("async")
    )
    request_body = "\n".join(
        f'        "{item.get("method", "")}" to "{item.get("request", "")}",'
        for item in contracts
        if item.get("method")
    )
    result_body = "\n".join(
        f'        "{item.get("method", "")}" to "{item.get("result", "")}",'
        for item in contracts
        if item.get("method")
    )
    method_error_codes_body = "\n".join(
        f'        "{item.get("method", "")}" to listOf({", ".join(json.dumps(code) for code in item.get("errorCodes", []))}),'
        for item in contracts
        if item.get("method")
    )
    method_compatibility_body = "\n".join(
        f'        "{item.get("method", "")}" to "{item.get("compatibility", "")}",'
        for item in contracts
        if item.get("method")
    )
    operation_type_body = "\n".join(
        f'        "{item.get("method", "")}" to "{item.get("operation", {}).get("type", "")}",'
        for item in contracts
        if item.get("method") and item.get("operation")
    )
    operation_async_result_body = "\n".join(
        f'        "{item.get("method", "")}" to "{item.get("operation", {}).get("asyncResultVia", "")}",'
        for item in contracts
        if item.get("method") and item.get("operation")
    )
    operation_stages_body = "\n".join(
        f'        "{item.get("method", "")}" to listOf({", ".join(json.dumps(stage) for stage in item.get("operation", {}).get("stages", []))}),'
        for item in contracts
        if item.get("method") and item.get("operation")
    )
    return (
        "package com.rknnovpn.panel.ipc\n"
        "\n"
        "// Generated from daemon/internal/ipc/contract_manifest.json.\n"
        "// Run .github/scripts/check_ipc_contract_codegen.py --write after editing the IPC contract.\n"
        "internal object GeneratedDaemonContract {\n"
        f"    const val CONTRACT_VERSION: Int = {version}\n"
        "    data class OperationPolicy(\n"
        "        val stages: List<String>,\n"
        "        val errorCodes: List<String>,\n"
        "    )\n"
        f"    val ERROR_CODES: List<String> = listOf({contract_error_codes_body})\n"
        f"    val COMPATIBILITY_POLICIES: Set<String> = setOf({compatibility_policies_body})\n"
        "    val OPERATION_POLICIES: Map<String, OperationPolicy> = mapOf(\n"
        f"{operation_policies_body}\n"
        "    )\n"
        "    val APK_REQUIRED_METHODS: Set<String> = setOf(\n"
        f"{required_body}\n"
        "    )\n"
        "    val METHOD_CAPABILITIES: Map<String, String> = mapOf(\n"
        f"{capabilities_body}\n"
        "    )\n"
        "    val MUTATING_METHODS: Set<String> = setOf(\n"
        f"{mutating_body}\n"
        "    )\n"
        "    val ASYNC_METHODS: Set<String> = setOf(\n"
        f"{async_body}\n"
        "    )\n"
        "    val METHOD_REQUESTS: Map<String, String> = mapOf(\n"
        f"{request_body}\n"
        "    )\n"
        "    val METHOD_RESULTS: Map<String, String> = mapOf(\n"
        f"{result_body}\n"
        "    )\n"
        "    val METHOD_ERROR_CODES: Map<String, List<String>> = mapOf(\n"
        f"{method_error_codes_body}\n"
        "    )\n"
        "    val METHOD_COMPATIBILITY: Map<String, String> = mapOf(\n"
        f"{method_compatibility_body}\n"
        "    )\n"
        "    val OPERATION_TYPES: Map<String, String> = mapOf(\n"
        f"{operation_type_body}\n"
        "    )\n"
        "    val OPERATION_ASYNC_RESULT_VIA: Map<String, String> = mapOf(\n"
        f"{operation_async_result_body}\n"
        "    )\n"
        "    val OPERATION_STAGES: Map<String, List<String>> = mapOf(\n"
        f"{operation_stages_body}\n"
        "    )\n"
        "}\n"
    )


def completed_method_contracts(source: dict) -> list[dict]:
    operation_policies = source.get("operationPolicies", {})
    contracts: list[dict] = []
    for item in source.get("methods", []):
        contract = dict(item)
        operation = contract.get("operation")
        if isinstance(operation, dict):
            operation = dict(operation)
            policy = operation_policies.get(operation.get("type", ""))
            if isinstance(policy, dict):
                contract["errorCodes"] = list(policy.get("errorCodes", []))
                operation["stages"] = list(policy.get("stages", []))
                contract["operation"] = operation
        contracts.append(contract)
    return contracts


def check_daemon_client_gates(source: dict) -> list[str]:
    if not DAEMON_CLIENT.exists():
        return [f"{DAEMON_CLIENT.relative_to(REPO_ROOT)} is missing"]
    manifest_methods = {item.get("method") for item in source.get("methods", []) if item.get("method")}
    required_methods = set(source.get("apkRequiredMethods", []))
    client = DAEMON_CLIENT.read_text(encoding="utf-8")
    errors: list[str] = []
    called_methods = set(_literal_method_calls(client))
    for method in sorted(called_methods):
        if method not in manifest_methods:
            errors.append(f"DaemonClient calls undeclared IPC method {method}")
        if method not in required_methods:
            errors.append(f"DaemonClient IPC method {method} is missing from apkRequiredMethods")
        if method not in BOOTSTRAP_METHODS and f'requireCompatible("{method}"' not in client:
            errors.append(f"DaemonClient IPC method {method} is missing requireCompatible gate")
    for method in sorted(required_methods - called_methods):
        errors.append(f"apkRequiredMethods contains method {method} that DaemonClient does not call")
    for method in sorted(_literal_require_compatible_methods(client)):
        if method not in manifest_methods:
            errors.append(f"DaemonClient requires undeclared IPC method {method}")
    errors.extend(_direct_executor_usage_errors())
    return errors


def check_daemon_control_wiring(source: dict) -> list[str]:
    missing_files = [
        path.relative_to(REPO_ROOT).as_posix()
        for path in [DAEMON_CONTROL_WIRING, CONTROL_REGISTRY]
        if not path.exists()
    ]
    if missing_files:
        return [f"{path} is missing" for path in missing_files]
    manifest_methods = {item.get("method") for item in source.get("methods", []) if item.get("method")}
    wiring = DAEMON_CONTROL_WIRING.read_text(encoding="utf-8")
    registry = CONTROL_REGISTRY.read_text(encoding="utf-8")
    registered_methods = set(_registered_daemon_control_methods(registry))
    errors: list[str] = []
    if "RegisterContractHandlers(" not in wiring or ".ContractHandlers()" not in wiring:
        errors.append(f"{DAEMON_CONTROL_WIRING.relative_to(REPO_ROOT)} must register HandlerGroups.ContractHandlers through control.RegisterContractHandlers")
    if "RegisterDaemonHandlers(" in registry:
        errors.append(f"{CONTROL_REGISTRY.relative_to(REPO_ROOT)} must not keep RegisterDaemonHandlers pass-through wrapper")
    if "map[string]ipc.Handler{" in wiring:
        errors.append(f"{DAEMON_CONTROL_WIRING.relative_to(REPO_ROOT)} must not own IPC method-to-handler mapping")
    for method in sorted(manifest_methods - registered_methods):
        errors.append(f"daemon control registry is missing IPC method {method}")
    for method in sorted(registered_methods - manifest_methods):
        errors.append(f"daemon control registry registers undeclared IPC method {method}")
    return errors


def _direct_executor_usage_errors() -> list[str]:
    errors: list[str] = []
    if not APP_KOTLIN_ROOT.exists():
        return errors
    allowed = {DAEMON_CLIENT, DAEMONCTL_EXECUTOR}
    for path in sorted(APP_KOTLIN_ROOT.rglob("*.kt")):
        if path in allowed:
            continue
        source = path.read_text(encoding="utf-8")
        if ".execute(" in source:
            errors.append(
                f"{path.relative_to(REPO_ROOT)} calls execute() directly; route daemon IPC through DaemonClient"
            )
    return errors


def _literal_method_calls(source: str) -> list[str]:
    import re

    methods = re.findall(r'\bcall(?:ConfigMutation)?\(\s*"([^"]+)"', source)
    methods.extend(re.findall(r'\bcall\(\s*method\s*=\s*"([^"]+)"', source))
    return methods


def _literal_require_compatible_methods(source: str) -> list[str]:
    import re

    methods: list[str] = []
    for args in re.findall(r'requireCompatible\(([^)]*)\)', source):
        methods.extend(re.findall(r'"([^"]+)"', args))
    return methods


def _registered_daemon_control_methods(source: str) -> list[str]:
    import re

    start = source.find("func (g HandlerGroups) ContractHandlers()")
    end = source.find("func RegisterContractHandlers", start)
    if start < 0 or end < 0:
        return []
    return re.findall(r'"([^"]+)"\s*:', source[start:end])


def _go_switch_cases(source: str, func_name: str) -> list[str]:
    import re

    pattern = r'func\s+{}\([^)]*\)\s+[^{{]+{{(?P<body>.*?)}}'.format(re.escape(func_name))
    match = re.search(pattern, source, re.S)
    if not match:
        return []
    cases: list[str] = []
    for clause in re.findall(r'case\s+([^:]+):', match.group("body")):
        cases.extend(re.findall(r'"([^"]+)"', clause))
    return cases


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--write", action="store_true", help="update the generated Kotlin source")
    args = parser.parse_args()

    source = json.loads(MANIFEST.read_text(encoding="utf-8"))
    gate_errors = validate_manifest(source)
    gate_errors.extend(check_daemon_control_wiring(source))
    gate_errors.extend(check_daemon_client_gates(source))
    if gate_errors:
        print("\n".join(gate_errors), file=sys.stderr)
        return 1
    rendered = render_contract(source)
    if args.write:
        OUTPUT.write_text(rendered, encoding="utf-8")
        return 0
    current = OUTPUT.read_text(encoding="utf-8") if OUTPUT.exists() else ""
    if current != rendered:
        print(f"{OUTPUT} is out of date; run {pathlib.Path(__file__).as_posix()} --write", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
