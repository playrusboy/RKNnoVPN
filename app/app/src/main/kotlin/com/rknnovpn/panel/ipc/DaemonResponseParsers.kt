package com.rknnovpn.panel.ipc

import com.rknnovpn.panel.model.BackendStatusV2
import com.rknnovpn.panel.model.NodeProbeResultV2
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.booleanOrNull
import kotlinx.serialization.json.contentOrNull
import kotlinx.serialization.json.decodeFromJsonElement
import kotlinx.serialization.json.intOrNull
import kotlinx.serialization.json.jsonArray
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import kotlinx.serialization.json.longOrNull

internal fun parseProfileSummaries(element: JsonElement): List<ProfileSummary> =
    element.jsonObject.entries.map { (key, value) ->
        ProfileSummary(
            id = key,
            name = value.jsonPrimitive.content,
            isActive = false,
        )
    }

internal fun Json.parseNodeProbeResults(element: JsonElement): List<NodeProbeResultV2> {
    val obj = element.jsonObject
    return obj["results"]?.jsonArray?.map { item ->
        decodeFromJsonElement(NodeProbeResultV2.serializer(), item)
    }.orEmpty()
}

internal fun parseLogLines(element: JsonElement): List<String> {
    val arr = when {
        element is JsonObject && element.containsKey("lines") ->
            element["lines"]!!.jsonArray
        else -> element.jsonArray
    }
    return arr.map { it.jsonPrimitive.content }
}

internal fun parseRuntimeLogsInfo(element: JsonElement): RuntimeLogsInfo {
    val obj = element.jsonObject
    val files = obj["logs"]?.jsonArray?.map { item ->
        val log = item.jsonObject
        RuntimeLogFile(
            name = log["name"]?.jsonPrimitive?.content.orEmpty(),
            path = log["path"]?.jsonPrimitive?.content.orEmpty(),
            lines = log["lines"]?.jsonArray?.map { line -> line.jsonPrimitive.content }.orEmpty(),
            missing = log["missing"]?.jsonPrimitive?.booleanOrNull ?: false,
            error = log["error"]?.jsonPrimitive?.contentOrNull.orEmpty(),
        )
    }.orEmpty()
    return RuntimeLogsInfo(files = files)
}

internal fun parseUpdateCheckInfo(element: JsonElement): UpdateCheckInfo {
    val obj = element as JsonObject
    return UpdateCheckInfo(
        currentVersion = obj["current_version"]?.jsonPrimitive?.content ?: "unknown",
        latestVersion = obj["latest_version"]?.jsonPrimitive?.content ?: "unknown",
        hasUpdate = obj["has_update"]?.jsonPrimitive?.booleanOrNull ?: false,
        changelog = obj["changelog"]?.jsonPrimitive?.content ?: "",
        moduleSize = obj["module_size"]?.jsonPrimitive?.longOrNull ?: 0L,
        apkSize = obj["apk_size"]?.jsonPrimitive?.longOrNull ?: 0L,
    )
}

internal fun parseUpdateDownloadInfo(element: JsonElement): UpdateDownloadInfo {
    val obj = element as JsonObject
    return UpdateDownloadInfo(
        modulePath = obj["module_path"]?.jsonPrimitive?.content ?: "",
        apkPath = obj["apk_path"]?.jsonPrimitive?.content ?: "",
        manifestPath = obj["manifest_path"]?.jsonPrimitive?.content ?: "",
        checksums = obj["checksums"]?.jsonPrimitive?.booleanOrNull ?: false,
        operation = obj["operation"],
    )
}

internal fun Json.parseUpdateInstallInfo(element: JsonElement): UpdateInstallInfo {
    val status = decodeFromJsonElement(BackendStatusV2.serializer(), element)
    return UpdateInstallInfo(
        accepted = status.activeOperation?.kind == "update-install",
        operationGeneration = status.activeOperation?.generation ?: 0L,
        runtimeStatus = status,
    )
}

internal fun parseVersionInfo(element: JsonElement): VersionInfo {
    val obj = element as JsonObject
    return VersionInfo(
        daemonVersion = obj["daemon"]?.jsonPrimitive?.content ?: "unknown",
        coreVersion = obj["core"]?.jsonPrimitive?.content ?: "unknown",
        daemonctlVersion = obj["daemonctl"]?.jsonPrimitive?.content ?: "unknown",
        moduleVersion = obj["module"]?.jsonObject?.get("version")?.jsonPrimitive?.content ?: "unknown",
        moduleVersionCode = obj["module"]?.jsonObject?.get("versionCode")?.jsonPrimitive?.content ?: "",
        modulePath = obj["module"]?.jsonObject?.get("path")?.jsonPrimitive?.content ?: "",
        currentReleaseVersion = obj["current_release"]?.jsonObject?.get("version")?.jsonPrimitive?.content ?: "",
        currentReleaseOK = obj["current_release"]?.jsonObject?.get("ok")?.jsonPrimitive?.booleanOrNull,
        currentReleaseError = obj["current_release"]?.jsonObject?.get("error")?.jsonPrimitive?.contentOrNull ?: "",
        runtimePreflightOK = obj["runtime_preflight"]?.jsonObject?.get("ok")?.jsonPrimitive?.booleanOrNull ?: true,
        runtimePreflightIssues = obj["runtime_preflight"]?.jsonObject?.get("issues")?.jsonArray?.mapNotNull {
            it.jsonPrimitive.contentOrNull
        }.orEmpty(),
        runtimePreflightWarnings = obj["runtime_preflight"]?.jsonObject?.get("warnings")?.jsonArray?.mapNotNull {
            it.jsonPrimitive.contentOrNull
        }.orEmpty(),
        singBoxAvailable = obj["sing_box"]?.jsonObject?.get("error")?.jsonPrimitive?.contentOrNull.isNullOrBlank(),
        singBoxError = obj["sing_box"]?.jsonObject?.get("error")?.jsonPrimitive?.contentOrNull ?: "",
        controlProtocolVersion = obj["control_protocol_version"]?.jsonPrimitive?.intOrNull
            ?: obj["control_protocol"]?.jsonPrimitive?.intOrNull
            ?: 0,
        schemaVersion = obj["schema_version"]?.jsonPrimitive?.intOrNull ?: 0,
        ipcContractVersion = obj["ipc_contract_version"]?.jsonPrimitive?.intOrNull ?: 0,
        contractHash = obj.stringValue("contract_hash", "contractHash"),
        compatibilityFingerprint = obj.stringValue("compatibility_fingerprint", "compatibilityFingerprint"),
        daemonPid = obj["daemon_pid"]?.jsonPrimitive?.intOrNull
            ?: obj["daemonPid"]?.jsonPrimitive?.intOrNull
            ?: 0,
        socketInode = obj.stringValue("socket_inode", "socketInode"),
        panelMinVersion = obj["panel_min_version"]?.jsonPrimitive?.contentOrNull ?: "",
        capabilities = obj["capabilities"]?.jsonArray?.mapNotNull {
            it.jsonPrimitive.contentOrNull
        }.orEmpty(),
        supportedMethods = obj["supported_methods"]?.jsonArray?.mapNotNull {
            it.jsonPrimitive.contentOrNull
        }.orEmpty(),
        apkRequiredMethods = obj["apk_required_methods"]?.jsonArray?.mapNotNull {
            it.jsonPrimitive.contentOrNull
        }.orEmpty(),
        errorCodes = obj.stringList("error_codes", "errorCodes"),
        compatibilityPolicies = obj.stringList("compatibility_policies", "compatibilityPolicies"),
        operationPolicies = obj.operationPolicies("operation_policies", "operationPolicies"),
        methods = obj.methodContracts("methods"),
    )
}

private fun JsonObject.stringValue(vararg keys: String): String =
    keys.firstNotNullOfOrNull { key -> this[key]?.jsonPrimitive?.contentOrNull }.orEmpty()

private fun JsonObject.stringList(vararg keys: String): List<String> {
    val element = keys.firstNotNullOfOrNull { key -> this[key] } ?: return emptyList()
    return element.jsonArray.mapNotNull { it.jsonPrimitive.contentOrNull }
}

private fun JsonObject.operationPolicies(vararg keys: String): Map<String, IpcOperationPolicyInfo> {
    val obj = keys.firstNotNullOfOrNull { key -> this[key]?.jsonObject } ?: return emptyMap()
    return obj.mapValues { (_, value) ->
        val policy = value.jsonObject
        IpcOperationPolicyInfo(
            stages = policy.stringList("stages"),
            errorCodes = policy.stringList("errorCodes", "error_codes"),
        )
    }
}

private fun JsonObject.methodContracts(vararg keys: String): List<IpcMethodContractInfo> {
    val arr = keys.firstNotNullOfOrNull { key -> this[key]?.jsonArray } ?: return emptyList()
    return arr.map { item ->
        val obj = item.jsonObject
        val operation = obj["operation"]?.jsonObject?.let { operation ->
            IpcOperationContractInfo(
                type = operation["type"]?.jsonPrimitive?.contentOrNull.orEmpty(),
                asyncResultVia = operation["asyncResultVia"]?.jsonPrimitive?.contentOrNull
                    ?: operation["async_result_via"]?.jsonPrimitive?.contentOrNull
                    ?: "",
                stages = operation.stringList("stages"),
            )
        }
        IpcMethodContractInfo(
            method = obj["method"]?.jsonPrimitive?.contentOrNull.orEmpty(),
            capability = obj["capability"]?.jsonPrimitive?.contentOrNull.orEmpty(),
            mutating = obj["mutating"]?.jsonPrimitive?.booleanOrNull ?: false,
            async = obj["async"]?.jsonPrimitive?.booleanOrNull ?: false,
            request = obj["request"]?.jsonPrimitive?.contentOrNull.orEmpty(),
            result = obj["result"]?.jsonPrimitive?.contentOrNull.orEmpty(),
            errorCodes = obj.stringList("errorCodes", "error_codes"),
            operation = operation,
            compatibility = obj["compatibility"]?.jsonPrimitive?.contentOrNull.orEmpty(),
        )
    }
}
