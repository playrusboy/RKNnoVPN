package com.rknnovpn.panel.ipc

import com.rknnovpn.panel.model.RuntimeCompatibilityStatus
import com.rknnovpn.panel.model.RuntimeMethodCapability
import com.rknnovpn.panel.model.RuntimeOperationPolicy
import kotlinx.serialization.Serializable

@Serializable
data class IpcContractInfo(
    val version: Int = 0,
    val controlProtocolVersion: Int = 0,
    val schemaVersion: Int = 0,
    val errorCodes: List<String> = emptyList(),
    val compatibilityPolicies: List<String> = emptyList(),
    val operationPolicies: Map<String, IpcOperationPolicyInfo> = emptyMap(),
    val capabilities: List<String> = emptyList(),
    val apkRequiredMethods: List<String> = emptyList(),
    val methods: List<IpcMethodContractInfo> = emptyList(),
) {
    fun missingRequiredMethods(required: Collection<String>): List<String> {
        val methodNames = methods.mapTo(mutableSetOf()) { it.method }
        if (methodNames.isEmpty()) return required.toList()
        return required.filterNot { it in methodNames }
    }

    fun missingCapabilities(requiredMethods: Collection<String>): List<String> {
        if (capabilities.isEmpty()) return listOf("capabilities")
        return requiredMethods
            .mapNotNull { method -> GeneratedDaemonContract.METHOD_CAPABILITIES[method]?.takeIf { it.isNotBlank() } }
            .distinct()
            .filterNot { it in capabilities }
    }

    fun contractSurfaceMismatches(requiredMethods: Collection<String>): List<String> {
        val byMethod = methods.associateBy { it.method }
        val methodMismatches = requiredMethods
            .distinct()
            .sorted()
            .mapNotNull { method ->
                val contract = byMethod[method] ?: return@mapNotNull "$method: method contract missing"
                contract.surfaceMismatch(method)
            }
        return operationPolicyMismatches(
            errorCodes,
            compatibilityPolicies,
            operationPolicies.mapValues { (_, policy) -> policy.contractPolicySurface() },
        ) + methodMismatches
    }
}

@Serializable
data class IpcOperationPolicyInfo(
    val stages: List<String> = emptyList(),
    val errorCodes: List<String> = emptyList(),
)

@Serializable
data class IpcMethodContractInfo(
    val method: String = "",
    val capability: String = "",
    val mutating: Boolean = false,
    val async: Boolean = false,
    val request: String = "",
    val result: String = "",
    val errorCodes: List<String> = emptyList(),
    val operation: IpcOperationContractInfo? = null,
    val compatibility: String = "",
)

@Serializable
data class IpcOperationContractInfo(
    val type: String = "",
    val asyncResultVia: String = "",
    val stages: List<String> = emptyList(),
)

internal data class IpcCompatibilityRequirement(
    val enforceReleaseMatch: Boolean,
    val requiresSingBox: Boolean,
    val requiresSchemaVersion: Boolean,
)

private const val COMPAT_RELEASE_MISMATCH_ALLOWED = "release-mismatch-allowed"
private const val COMPAT_REQUIRES_SCHEMA = "requires-schema"
private const val COMPAT_REQUIRES_RUNTIME_SCHEMA = "requires-runtime-schema"

fun VersionInfo.missingRequiredMethods(required: Collection<String>): List<String> {
    if (supportedMethods.isEmpty()) return required.toList()
    return required.filterNot { it in supportedMethods }
}

fun VersionInfo.apkRequiredMethodMismatches(required: Collection<String>): List<String> {
    val requiredSet = required.toSet()
    val daemonRequiredSet = apkRequiredMethods.toSet()
    if (daemonRequiredSet.isEmpty()) {
        return listOf("daemon version не объявляет apk_required_methods")
    }
    return requiredMethodMismatches(requiredSet, daemonRequiredSet)
}

fun VersionInfo.contractSurfaceMismatches(requiredMethods: Collection<String>): List<String> {
    val byMethod = methods.associateBy { it.method }
    val methodMismatches = requiredMethods
        .distinct()
        .sorted()
        .mapNotNull { method ->
            val contract = byMethod[method] ?: return@mapNotNull "$method: method contract missing"
            contract.surfaceMismatch(method)
        }
    return operationPolicyMismatches(
        errorCodes,
        compatibilityPolicies,
        operationPolicies.mapValues { (_, policy) -> policy.contractPolicySurface() },
    ) + methodMismatches
}

fun VersionInfo.releaseMismatch(apkVersion: String): String? {
    val apk = stableReleaseVersion(apkVersion) ?: return null
    val daemon = stableReleaseVersion(daemonVersion)
    if (daemon != null && daemon != apk) {
        return "APK и daemon несовместимы: APK $apkVersion, daemon $daemonVersion. Установите matching release."
    }
    val module = stableReleaseVersion(moduleVersion)
    if (module != null && module != apk) {
        return "APK и модуль несовместимы: APK $apkVersion, module $moduleVersion. Установите matching release."
    }
    val currentRelease = stableReleaseVersion(currentReleaseVersion)
    if (currentRelease != null && currentRelease != apk) {
        return "APK и current release несовместимы: APK $apkVersion, current $currentReleaseVersion. Установите matching release."
    }
    return null
}

fun VersionInfo.currentReleaseWarning(): String? {
    if (currentReleaseOK != false) return null
    val detail = currentReleaseError.takeIf { it.isNotBlank() }?.let { ": $it" }.orEmpty()
    return "Каталог текущего релиза повреждён$detail. Запуск не заблокирован; используйте сброс root-части или переустановку модуля, если runtime ведёт себя нестабильно."
}

internal fun ipcCompatibilityRequirement(requiredMethods: Set<String>): IpcCompatibilityRequirement {
    val policies = requiredMethods.map { method -> GeneratedDaemonContract.METHOD_COMPATIBILITY[method].orEmpty() }
    val enforceReleaseMatch = policies.none { it == COMPAT_RELEASE_MISMATCH_ALLOWED }
    return IpcCompatibilityRequirement(
        enforceReleaseMatch = enforceReleaseMatch,
        requiresSingBox = policies.any { it == COMPAT_REQUIRES_RUNTIME_SCHEMA },
        requiresSchemaVersion = enforceReleaseMatch &&
            policies.any { it == COMPAT_REQUIRES_SCHEMA || it == COMPAT_REQUIRES_RUNTIME_SCHEMA },
    )
}

internal fun ipcCompatibilityIssue(
    info: VersionInfo,
    contract: IpcContractInfo,
    apkVersion: String,
    requiredMethods: Collection<String>,
    minControlProtocolVersion: Int,
    minSchemaVersion: Int,
): String? {
    val required = requiredMethods.toSet()
    val contractRequiredMethods = requiredMethods + listOf("ipc.contract", "version")
    val requirement = ipcCompatibilityRequirement(required)
    val missingCapabilities = contract.missingCapabilities(contractRequiredMethods)
    val missingMethods = contract.missingRequiredMethods(contractRequiredMethods)
    val contractMismatches = contract.contractSurfaceMismatches(contractRequiredMethods)
    val releaseMismatch = info.releaseMismatch(apkVersion)
    contract.headerMismatch(info, requirement, minControlProtocolVersion, minSchemaVersion)
        ?.let { return it }
    if (contractMismatches.isNotEmpty()) {
        return "APK и модуль несовместимы: IPC contract не совпадает (${contractMismatches.joinToString("; ")})"
    }
    return when {
        requirement.enforceReleaseMatch && releaseMismatch != null ->
            releaseMismatch
        info.controlProtocolVersion < minControlProtocolVersion ->
            "APK и модуль несовместимы: APK $apkVersion, daemon ${info.daemonVersion}, module ${info.moduleVersion}; control protocol ${info.controlProtocolVersion}, нужен $minControlProtocolVersion"
        requirement.requiresSchemaVersion && info.schemaVersion < minSchemaVersion ->
            "APK и модуль несовместимы: daemon config schema ${info.schemaVersion}, нужна $minSchemaVersion"
        requirement.enforceReleaseMatch && missingCapabilities.isNotEmpty() ->
            "APK и модуль несовместимы: daemon ${info.daemonVersion}, module ${info.moduleVersion}; нет capabilities ${missingCapabilities.joinToString(", ")}"
        requirement.requiresSingBox && !info.singBoxAvailable ->
            "APK и модуль несовместимы: sing-box недоступен (${info.singBoxError.ifBlank { "unknown" }})"
        missingMethods.isNotEmpty() ->
            "APK и модуль несовместимы: APK $apkVersion, daemon ${info.daemonVersion}, module ${info.moduleVersion}; нет методов ${missingMethods.joinToString(", ")}"
        else -> null
    }
}

private fun IpcContractInfo.headerMismatch(
    info: VersionInfo,
    requirement: IpcCompatibilityRequirement,
    minControlProtocolVersion: Int,
    minSchemaVersion: Int,
): String? {
    return when {
        version != GeneratedDaemonContract.CONTRACT_VERSION ->
            "APK и модуль несовместимы: IPC contract version $version, нужна ${GeneratedDaemonContract.CONTRACT_VERSION}"
        controlProtocolVersion < minControlProtocolVersion ->
            "APK и модуль несовместимы: IPC contract control protocol $controlProtocolVersion, нужен $minControlProtocolVersion"
        info.controlProtocolVersion != controlProtocolVersion ->
            "APK и модуль несовместимы: version/control protocol ${info.controlProtocolVersion} != contract $controlProtocolVersion"
        requirement.requiresSchemaVersion && schemaVersion < minSchemaVersion ->
            "APK и модуль несовместимы: IPC contract schema $schemaVersion, нужна $minSchemaVersion"
        requirement.requiresSchemaVersion && info.schemaVersion != schemaVersion ->
            "APK и модуль несовместимы: version/schema ${info.schemaVersion} != contract $schemaVersion"
        else -> null
    }
}

internal fun RuntimeCompatibilityStatus.blockingCompatibilityIssue(
    apkVersion: String,
    requiredMethods: Collection<String>,
): String? {
    releaseMismatch(apkVersion)?.let { return it }
    if (ipcContractVersion != 0 && ipcContractVersion != GeneratedDaemonContract.CONTRACT_VERSION) {
        return "APK и модуль несовместимы: IPC contract version $ipcContractVersion, нужна ${GeneratedDaemonContract.CONTRACT_VERSION}"
    }
    if (controlProtocolVersion < DaemonClient.MIN_CONTROL_PROTOCOL_VERSION) {
        return "APK и модуль несовместимы: APK $apkVersion, daemon $daemonVersion, module $moduleVersion; control protocol $controlProtocolVersion, нужен ${DaemonClient.MIN_CONTROL_PROTOCOL_VERSION}"
    }
    if (schemaVersion < DaemonClient.MIN_SCHEMA_VERSION) {
        return "APK и модуль несовместимы: daemon config schema $schemaVersion, нужна ${DaemonClient.MIN_SCHEMA_VERSION}"
    }
    val missingMethods = missingRequiredMethods(requiredMethods)
    if (missingMethods.isNotEmpty()) {
        return "APK и модуль несовместимы: APK $apkVersion, daemon $daemonVersion, module $moduleVersion; нет методов ${missingMethods.joinToString(", ")}"
    }
    val apkRequiredMethodMismatches = apkRequiredMethodMismatches(requiredMethods)
    if (apkRequiredMethodMismatches.isNotEmpty()) {
        return "APK и модуль несовместимы: daemon APK required methods не совпадают (${apkRequiredMethodMismatches.joinToString(", ")})"
    }
    val contractMismatches = contractSurfaceMismatches(requiredMethods)
    if (contractMismatches.isNotEmpty()) {
        return "APK и модуль несовместимы: IPC contract не совпадает (${contractMismatches.joinToString("; ")})"
    }
    val missingCapabilities = missingCapabilities(requiredMethods)
    if (missingCapabilities.isNotEmpty()) {
        return "APK и модуль несовместимы: daemon $daemonVersion, module $moduleVersion; нет capabilities ${missingCapabilities.joinToString(", ")}"
    }
    return null
}

private fun RuntimeCompatibilityStatus.apkRequiredMethodMismatches(required: Collection<String>): List<String> {
    if (apkRequiredMethods.isEmpty()) return emptyList()
    return requiredMethodMismatches(required.toSet(), apkRequiredMethods.toSet())
}

private fun requiredMethodMismatches(
    requiredSet: Set<String>,
    daemonRequiredSet: Set<String>,
): List<String> {
    return buildList {
        addAll(requiredSet.filterNot { it in daemonRequiredSet }.map { "missing $it" })
        addAll(daemonRequiredSet.filterNot { it in requiredSet }.map { "extra $it" })
    }.sorted()
}

private fun RuntimeCompatibilityStatus.missingRequiredMethods(required: Collection<String>): List<String> {
    if (supportedMethods.isEmpty()) return required.toList()
    return required.filterNot { it in supportedMethods }
}

private fun RuntimeCompatibilityStatus.missingCapabilities(requiredMethods: Collection<String>): List<String> {
    if (capabilities.isEmpty()) return listOf("capabilities")
    return requiredMethods
        .mapNotNull { method -> GeneratedDaemonContract.METHOD_CAPABILITIES[method]?.takeIf { it.isNotBlank() } }
        .distinct()
        .filterNot { it in capabilities }
}

private fun RuntimeCompatibilityStatus.contractSurfaceMismatches(requiredMethods: Collection<String>): List<String> {
    val byMethod = methods.associateBy { it.method }
    val methodMismatches = requiredMethods
        .distinct()
        .sorted()
        .mapNotNull { method ->
            val contract = byMethod[method] ?: return@mapNotNull "$method: method contract missing"
            contract.surfaceMismatch(method)
        }
    return operationPolicyMismatches(
        errorCodes,
        compatibilityPolicies,
        operationPolicies.mapValues { (_, policy) -> policy.contractPolicySurface() },
    ) + methodMismatches
}

private data class ContractPolicySurface(
    val stages: List<String>,
    val errorCodes: List<String>,
)

private fun IpcOperationPolicyInfo.contractPolicySurface(): ContractPolicySurface =
    ContractPolicySurface(stages = stages, errorCodes = errorCodes)

private fun RuntimeOperationPolicy.contractPolicySurface(): ContractPolicySurface =
    ContractPolicySurface(stages = stages, errorCodes = errorCodes)

private fun GeneratedDaemonContract.OperationPolicy.contractPolicySurface(): ContractPolicySurface =
    ContractPolicySurface(stages = stages, errorCodes = errorCodes)

private fun operationPolicyMismatches(
    errorCodes: List<String>,
    compatibilityPolicies: List<String>,
    operationPolicies: Map<String, ContractPolicySurface>,
): List<String> = buildList {
    if (errorCodes != GeneratedDaemonContract.ERROR_CODES) {
        add("errorCodes ${errorCodes.joinToString(",")} != ${GeneratedDaemonContract.ERROR_CODES.joinToString(",")}")
    }
    val expectedCompatibilityPolicies = GeneratedDaemonContract.COMPATIBILITY_POLICIES
    if (compatibilityPolicies.toSet() != expectedCompatibilityPolicies) {
        add("compatibilityPolicies ${compatibilityPolicies.joinToString(",")} != ${expectedCompatibilityPolicies.sorted().joinToString(",")}")
    }
    val expectedPolicies = GeneratedDaemonContract.OPERATION_POLICIES
        .mapValues { (_, policy) -> policy.contractPolicySurface() }
    if (operationPolicies.isEmpty() && expectedPolicies.isNotEmpty()) {
        add("operationPolicies missing")
        return@buildList
    }
    for (entry in expectedPolicies.entries.sortedBy { it.key }) {
        val type = entry.key
        val expected = entry.value
        val actual = operationPolicies[type]
        when {
            actual == null ->
                add("operation policy $type missing")
            actual.stages != expected.stages ->
                add("operation policy $type stages ${actual.stages.joinToString(",")} != ${expected.stages.joinToString(",")}")
            actual.errorCodes != expected.errorCodes ->
                add("operation policy $type errorCodes ${actual.errorCodes.joinToString(",")} != ${expected.errorCodes.joinToString(",")}")
        }
    }
    for (type in (operationPolicies.keys - expectedPolicies.keys).sorted()) {
        add("operation policy $type unexpected")
    }
}

private fun IpcMethodContractInfo.surfaceMismatch(method: String): String? {
    val expectedCapability = GeneratedDaemonContract.METHOD_CAPABILITIES[method].orEmpty()
    val expectedCompatibility = GeneratedDaemonContract.METHOD_COMPATIBILITY[method].orEmpty()
    val expectedMutating = method in GeneratedDaemonContract.MUTATING_METHODS
    val expectedAsync = method in GeneratedDaemonContract.ASYNC_METHODS
    surfaceMismatchMessage(
        method,
        capability,
        compatibility,
        mutating,
        async,
        expectedCapability,
        expectedCompatibility,
        expectedMutating,
        expectedAsync,
    )
        ?.let { return it }

    val expectedRequest = GeneratedDaemonContract.METHOD_REQUESTS[method].orEmpty()
    val expectedResult = GeneratedDaemonContract.METHOD_RESULTS[method].orEmpty()
    val expectedErrorCodes = GeneratedDaemonContract.METHOD_ERROR_CODES[method].orEmpty()
    val expectedOperationType = GeneratedDaemonContract.OPERATION_TYPES[method].orEmpty()
    val expectedAsyncResultVia = GeneratedDaemonContract.OPERATION_ASYNC_RESULT_VIA[method].orEmpty()
    val expectedStages = GeneratedDaemonContract.OPERATION_STAGES[method].orEmpty()
    val actualOperation = operation
    return when {
        request != expectedRequest ->
            "$method: request ${request.ifBlank { "missing" }} != $expectedRequest"
        result != expectedResult ->
            "$method: result ${result.ifBlank { "missing" }} != $expectedResult"
        errorCodes != expectedErrorCodes ->
            "$method: errorCodes ${errorCodes.joinToString(",")} != ${expectedErrorCodes.joinToString(",")}"
        expectedOperationType.isBlank() && actualOperation != null ->
            "$method: operation present, expected none"
        expectedOperationType.isNotBlank() && actualOperation == null ->
            "$method: operation missing"
        expectedOperationType.isNotBlank() && actualOperation != null && actualOperation.type != expectedOperationType ->
            "$method: operation.type ${actualOperation.type.ifBlank { "missing" }} != $expectedOperationType"
        expectedOperationType.isNotBlank() && actualOperation != null && actualOperation.asyncResultVia != expectedAsyncResultVia ->
            "$method: operation.asyncResultVia ${actualOperation.asyncResultVia.ifBlank { "missing" }} != $expectedAsyncResultVia"
        expectedOperationType.isNotBlank() && actualOperation != null && actualOperation.stages != expectedStages ->
            "$method: operation.stages ${actualOperation.stages.joinToString(",")} != ${expectedStages.joinToString(",")}"
        else -> null
    }
}

private fun RuntimeMethodCapability.surfaceMismatch(method: String): String? {
    val expectedCapability = GeneratedDaemonContract.METHOD_CAPABILITIES[method].orEmpty()
    val expectedCompatibility = GeneratedDaemonContract.METHOD_COMPATIBILITY[method].orEmpty()
    val expectedMutating = method in GeneratedDaemonContract.MUTATING_METHODS
    val expectedAsync = method in GeneratedDaemonContract.ASYNC_METHODS
    return surfaceMismatchMessage(
        method,
        capability,
        compatibility,
        mutating,
        async,
        expectedCapability,
        expectedCompatibility,
        expectedMutating,
        expectedAsync,
    )
}

private fun surfaceMismatchMessage(
    method: String,
    actualCapability: String,
    actualCompatibility: String,
    actualMutating: Boolean,
    actualAsync: Boolean,
    expectedCapability: String,
    expectedCompatibility: String,
    expectedMutating: Boolean,
    expectedAsync: Boolean,
): String? {
    return when {
        method !in GeneratedDaemonContract.METHOD_CAPABILITIES ->
            "$method: APK has no generated method contract"
        actualCapability != expectedCapability ->
            "$method: capability ${actualCapability.ifBlank { "missing" }} != $expectedCapability"
        actualCompatibility != expectedCompatibility ->
            "$method: compatibility ${actualCompatibility.ifBlank { "missing" }} != ${expectedCompatibility.ifBlank { "empty" }}"
        actualMutating != expectedMutating ->
            "$method: mutating=$actualMutating, expected $expectedMutating"
        actualAsync != expectedAsync ->
            "$method: async=$actualAsync, expected $expectedAsync"
        else -> null
    }
}

private fun RuntimeCompatibilityStatus.releaseMismatch(apkVersion: String): String? {
    val apk = stableReleaseVersion(apkVersion) ?: return null
    val daemon = stableReleaseVersion(daemonVersion)
    if (daemon != null && daemon != apk) {
        return "APK и daemon несовместимы: APK $apkVersion, daemon $daemonVersion. Установите matching release."
    }
    val module = stableReleaseVersion(moduleVersion)
    if (module != null && module != apk) {
        return "APK и модуль несовместимы: APK $apkVersion, module $moduleVersion. Установите matching release."
    }
    val currentRelease = stableReleaseVersion(currentReleaseVersion)
    if (currentRelease != null && currentRelease != apk) {
        return "APK и current release несовместимы: APK $apkVersion, current $currentReleaseVersion. Установите matching release."
    }
    return null
}

internal fun stableReleaseVersion(raw: String): String? {
    val normalized = raw.trim().removePrefix("v")
    if (!Regex("""\d+\.\d+\.\d+""").matches(normalized)) return null
    return normalized
}
