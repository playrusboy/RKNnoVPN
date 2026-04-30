#!/usr/bin/env python3
import argparse
import json
import pathlib
import sys


REPO_ROOT = pathlib.Path(__file__).resolve().parents[2]
MANIFEST = REPO_ROOT / "daemon/internal/ipc/contract_manifest.json"
OUTPUT = REPO_ROOT / "app/app/src/main/kotlin/com/rknnovpn/panel/ipc/GeneratedDaemonContract.kt"
DAEMON_CLIENT = REPO_ROOT / "app/app/src/main/kotlin/com/rknnovpn/panel/ipc/DaemonClient.kt"
DAEMONCTL_EXECUTOR = REPO_ROOT / "app/app/src/main/kotlin/com/rknnovpn/panel/ipc/DaemonctlExecutor.kt"
DAEMON_CONTROL_WIRING = REPO_ROOT / "daemon/cmd/daemon/control_wiring.go"
APPLY_TRANSACTION = REPO_ROOT / "daemon/internal/apply/transaction.go"
APP_KOTLIN_ROOT = REPO_ROOT / "app/app/src/main/kotlin"
BOOTSTRAP_METHODS = {"backend.status", "ipc.contract", "version"}
CONFIG_TRANSACTION_REQUIRED_STAGES = {
    "validate",
    "render",
    "persist-draft",
    "runtime-apply",
    "verify",
    "commit-generation",
    "cleanup",
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
    errors.extend(check_config_transaction_actions(source))
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
    if not DAEMON_CONTROL_WIRING.exists():
        return [f"{DAEMON_CONTROL_WIRING.relative_to(REPO_ROOT)} is missing"]
    manifest_methods = {item.get("method") for item in source.get("methods", []) if item.get("method")}
    wiring = DAEMON_CONTROL_WIRING.read_text(encoding="utf-8")
    registered_methods = set(_registered_daemon_control_methods(wiring))
    errors: list[str] = []
    for method in sorted(manifest_methods - registered_methods):
        errors.append(f"daemon control wiring is missing IPC method {method}")
    for method in sorted(registered_methods - manifest_methods):
        errors.append(f"daemon control wiring registers undeclared IPC method {method}")
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

    match = re.search(
        r"RegisterContractHandlers\([^,]+,\s*map\[string\]ipc\.Handler\{(?P<body>.*?)\n\s*\}\)",
        source,
        re.S,
    )
    if not match:
        return []
    return re.findall(r'"([^"]+)"\s*:', match.group("body"))


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
