package com.rknnovpn.panel.ipc

import com.rknnovpn.panel.BuildConfig
import com.rknnovpn.panel.model.AppInfo
import com.rknnovpn.panel.model.AuditReport
import com.rknnovpn.panel.model.BackendHealthSnapshot
import com.rknnovpn.panel.model.BackendStatusV2
import com.rknnovpn.panel.model.DaemonStatus
import com.rknnovpn.panel.model.DesiredStateV2
import com.rknnovpn.panel.model.Node
import com.rknnovpn.panel.model.NodeProbeResultV2
import com.rknnovpn.panel.model.ProfileConfig
import kotlinx.serialization.builtins.ListSerializer
import kotlinx.serialization.json.add
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.decodeFromJsonElement
import kotlinx.serialization.json.encodeToJsonElement
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.put
import kotlinx.serialization.json.putJsonArray
import javax.inject.Inject
import javax.inject.Singleton

/**
 * Typed, high-level client for daemon IPC methods.
 *
 * The daemon persists a runtime-oriented config (`node`, `transport`, etc.),
 * while the APK works with a richer UI profile (`nodes[]`, `activeNodeId`).
 * This client bridges those two shapes so the rest of the app can stay on the
 * UI-facing model without losing information.
 */
@Singleton
class DaemonClient @Inject constructor(
    private val executor: DaemonctlExecutor
) {
    companion object {
        const val MIN_CONTROL_PROTOCOL_VERSION = 5
        const val MIN_SCHEMA_VERSION = 5
        val REQUIRED_METHODS: Set<String> = GeneratedDaemonContract.APK_REQUIRED_METHODS
    }

    private val json = Json {
        ignoreUnknownKeys = true
        isLenient = true
        coerceInputValues = true
        encodeDefaults = true
    }
    private val prettyJson = Json {
        prettyPrint = true
        ignoreUnknownKeys = true
        isLenient = true
        coerceInputValues = true
    }

    fun toDaemonStatus(status: BackendStatusV2): DaemonStatus = status.toDaemonStatus()

    // ---- Connection lifecycle ----

    /** Get daemon runtime status (connection state, active node, health). */
    suspend fun status(): DaemonClientResult<DaemonStatus> {
        return when (val result = backendStatus()) {
            is DaemonClientResult.Ok -> DaemonClientResult.Ok(result.data.toDaemonStatus())
            is DaemonClientResult.DaemonError -> result
            else -> result.asFailure()
        }
    }

    /** Start the proxy connection using the active profile. */
    suspend fun start(): DaemonClientResult<BackendStatusV2> {
        return when (val result = backendStart()) {
            is DaemonClientResult.Ok -> result
            is DaemonClientResult.DaemonError -> result.asFailure()
            else -> result.asFailure()
        }
    }

    /** Stop the proxy connection (keeps daemon alive). */
    suspend fun stop(): DaemonClientResult<BackendStatusV2> {
        return when (val result = backendStop()) {
            is DaemonClientResult.Ok -> result
            is DaemonClientResult.DaemonError -> result.asFailure()
            else -> result.asFailure()
        }
    }

    /** Reload the active configuration without full restart. */
    suspend fun reload(): DaemonClientResult<BackendStatusV2> {
        return when (val result = backendRestart()) {
            is DaemonClientResult.Ok -> result
            is DaemonClientResult.DaemonError -> result.asFailure()
            else -> result.asFailure()
        }
    }

    /** Force-remove RKNnoVPN network rules and stop the proxy core. */
    suspend fun networkReset(): DaemonClientResult<BackendStatusV2> {
        return when (val result = backendReset()) {
            is DaemonClientResult.Ok -> result
            is DaemonClientResult.DaemonError -> result.asFailure()
            else -> result.asFailure()
        }
    }

    /** Run a full health check and return the result in the dashboard shape. */
    suspend fun health(): DaemonClientResult<DaemonStatus> {
        return when (val statusResult = backendStatus()) {
            is DaemonClientResult.Ok -> when (val healthResult = diagnosticsHealth()) {
                is DaemonClientResult.Ok -> {
                    DaemonClientResult.Ok(statusResult.data.toDaemonStatus(healthOverride = healthResult.data))
                }
                is DaemonClientResult.DaemonError -> healthResult.asFailure()
                else -> healthResult.asFailure()
            }
            is DaemonClientResult.DaemonError -> statusResult.asFailure()
            else -> statusResult.asFailure()
        }
    }

    /** Run a privacy/security audit. */
    suspend fun audit(): DaemonClientResult<AuditReport> {
        requireCompatible("audit")?.let { return it.asFailure() }
        return call("audit") { json.decodeFromJsonElement(AuditReport.serializer(), it) }
    }

    // ---- Profile ----

    /** Get the daemon-owned profile document. */
    suspend fun profileGet(): DaemonClientResult<ProfileConfig> {
        requireCompatible("profile.get")?.let { return it.asFailure() }
        return call("profile.get") {
            json.decodeFromJsonElement(ProfileConfig.serializer(), it)
        }
    }

    /** Replace the full daemon-owned profile document. */
    suspend fun profileApply(
        config: ProfileConfig,
        reload: Boolean = true,
    ): DaemonClientResult<ConfigMutationInfo> {
        requireCompatible("profile.apply")?.let { return it.asFailure() }
        return callConfigMutation(
            "profile.apply",
            buildJsonObject {
                put("profile", json.encodeToJsonElement(ProfileConfig.serializer(), config))
                put("reload", reload)
            },
        )
    }

    suspend fun profileImportNodes(
        nodes: List<Node>,
        reload: Boolean = true,
    ): DaemonClientResult<ConfigMutationInfo> {
        requireCompatible("profile.importNodes")?.let { return it.asFailure() }
        return callConfigMutation(
            "profile.importNodes",
            buildJsonObject {
                put("nodes", json.encodeToJsonElement(ListSerializer(Node.serializer()), nodes))
                put("reload", reload)
            },
        )
    }

    suspend fun profileSetActiveNode(
        nodeId: String,
        reload: Boolean = true,
    ): DaemonClientResult<ConfigMutationInfo> {
        requireCompatible("profile.setActiveNode")?.let { return it.asFailure() }
        return callConfigMutation(
            "profile.setActiveNode",
            buildJsonObject {
                put("nodeId", nodeId)
                put("reload", reload)
            },
        )
    }

    /** List all stored config sections the daemon currently understands. */
    suspend fun configList(): DaemonClientResult<List<ProfileSummary>> {
        requireCompatible("config-list")?.let { return it.asFailure() }
        return call("config-list", transform = ::parseProfileSummaries)
    }

    suspend fun subscriptionPreview(url: String): DaemonClientResult<SubscriptionPreviewInfo> {
        requireCompatible("subscription.preview")?.let { return it.asFailure() }
        val params = buildJsonObject { put("url", url) }
        return call("subscription.preview", params, timeoutMs = 60_000L) { element ->
            json.decodeFromJsonElement(SubscriptionPreviewInfo.serializer(), element)
        }
    }

    suspend fun subscriptionRefresh(url: String): DaemonClientResult<ConfigMutationInfo> {
        requireCompatible("subscription.refresh")?.let { return it.asFailure() }
        val params = buildJsonObject { put("url", url) }
        return callConfigMutation("subscription.refresh", params)
    }

    suspend fun nodeTest(
        nodeIds: List<String> = emptyList(),
        url: String = "",
        timeoutMs: Int = 5_000,
    ): DaemonClientResult<NodeTestInfo> {
        when (val result = diagnosticsTestNodes(nodeIds, url, timeoutMs)) {
            is DaemonClientResult.Ok -> {
                return DaemonClientResult.Ok(result.data.toNodeTestInfo(url))
            }
            is DaemonClientResult.DaemonError -> return result.asFailure()
            is DaemonClientResult.RootDenied -> return result.asFailure()
            is DaemonClientResult.Timeout -> return result.asFailure()
            is DaemonClientResult.DaemonNotFound -> return result.asFailure()
            is DaemonClientResult.ParseError -> return result.asFailure()
            is DaemonClientResult.Failure -> return result.asFailure()
        }
    }

    suspend fun backendStatus(): DaemonClientResult<BackendStatusV2> =
        call("backend.status") { json.decodeFromJsonElement(BackendStatusV2.serializer(), it) }

    suspend fun backendStart(): DaemonClientResult<BackendStatusV2> {
        requireCompatible("backend.start", "backend.status")?.let { return it.asFailure() }
        return call("backend.start", timeoutMs = ACCEPT_TIMEOUT_MS) {
            json.decodeFromJsonElement(BackendStatusV2.serializer(), it)
        }
    }

    suspend fun backendStop(): DaemonClientResult<BackendStatusV2> {
        requireCompatible("backend.stop", "backend.status")?.let { return it.asFailure() }
        return call("backend.stop", timeoutMs = ACCEPT_TIMEOUT_MS) {
            json.decodeFromJsonElement(BackendStatusV2.serializer(), it)
        }
    }

    suspend fun backendRestart(): DaemonClientResult<BackendStatusV2> {
        requireCompatible("backend.restart", "backend.status")?.let { return it.asFailure() }
        return call("backend.restart", timeoutMs = ACCEPT_TIMEOUT_MS) {
            json.decodeFromJsonElement(BackendStatusV2.serializer(), it)
        }
    }

    suspend fun backendReset(): DaemonClientResult<BackendStatusV2> {
        requireCompatible("backend.reset", "backend.status")?.let { return it.asFailure() }
        return call("backend.reset", timeoutMs = ACCEPT_TIMEOUT_MS) {
            json.decodeFromJsonElement(BackendStatusV2.serializer(), it)
        }
    }

    suspend fun backendApplyDesiredState(desiredState: DesiredStateV2): DaemonClientResult<BackendStatusV2> {
        requireCompatible("backend.applyDesiredState", "backend.status")?.let { return it.asFailure() }
        return call(
            method = "backend.applyDesiredState",
            params = json.encodeToJsonElement(DesiredStateV2.serializer(), desiredState).jsonObject,
            timeoutMs = 15_000L,
        ) {
            json.decodeFromJsonElement(BackendStatusV2.serializer(), it)
        }
    }

    suspend fun diagnosticsHealth(): DaemonClientResult<BackendHealthSnapshot> {
        requireCompatible("diagnostics.health", "backend.status")?.let { return it.asFailure() }
        return call("diagnostics.health", timeoutMs = 30_000L) {
            json.decodeFromJsonElement(BackendHealthSnapshot.serializer(), it)
        }
    }

    suspend fun diagnosticsTestNodes(
        nodeIds: List<String> = emptyList(),
        url: String = "",
        timeoutMs: Int = 5_000,
    ): DaemonClientResult<List<NodeProbeResultV2>> {
        requireCompatible("diagnostics.testNodes")?.let { return it.asFailure() }
        val params = buildJsonObject {
            putJsonArray("node_ids") {
                nodeIds.forEach { add(it) }
            }
            if (url.isNotBlank()) {
                put("url", url)
            }
            put("timeout_ms", timeoutMs)
        }
        return call("diagnostics.testNodes", params, timeoutMs = timeoutMs.toLong() + 5_000L) {
            json.parseNodeProbeResults(it)
        }
    }

    // ---- Logs ----

    suspend fun logs(
        lines: Int = 100,
        level: String = "info"
    ): DaemonClientResult<List<String>> {
        requireCompatible("logs")?.let { return it.asFailure() }
        val params = buildJsonObject {
            put("lines", lines)
            put("level", level)
        }
        return call("logs", params, transform = ::parseLogLines)
    }

    suspend fun runtimeLogs(lines: Int = 160): DaemonClientResult<RuntimeLogsInfo> {
        requireCompatible("logs")?.let { return it.asFailure() }
        val params = buildJsonObject {
            put("lines", lines)
            putJsonArray("files") {
                add("daemon")
                add("sing-box")
            }
        }
        return call("logs", params, timeoutMs = 15_000L, transform = ::parseRuntimeLogsInfo)
    }

    suspend fun diagnosticBundle(lines: Int = 220): DaemonClientResult<String> {
        requireCompatible("diagnostics.report")?.let { return it.asFailure() }
        val params = buildJsonObject {
            put("lines", lines)
        }
        return call("diagnostics.report", params, timeoutMs = 30_000L) { element ->
            prettyJson.encodeToString(JsonElement.serializer(), element)
        }
    }

    suspend fun selfCheck(): DaemonClientResult<SelfCheckSummary> {
        requireCompatible("self-check")?.let { return it.asFailure() }
        return call("self-check", timeoutMs = 15_000L) { element ->
            json.decodeFromJsonElement(SelfCheckSummary.serializer(), element)
        }
    }

    // ---- App management ----

    suspend fun resolveUid(uid: Int): DaemonClientResult<AppInfo> {
        requireCompatible("app.resolveUid")?.let { return it.asFailure() }
        val params = buildJsonObject { put("uid", uid) }
        return call("app.resolveUid", params) {
            json.decodeFromJsonElement(AppInfo.serializer(), it)
        }
    }

    suspend fun appList(): DaemonClientResult<List<AppInfo>> {
        requireCompatible("app.list")?.let { return it.asFailure() }
        return call("app.list", timeoutMs = 15_000L) {
            json.decodeFromJsonElement(ListSerializer(AppInfo.serializer()), it)
        }
    }

    // ---- Updates ----

    suspend fun updateCheck(): DaemonClientResult<UpdateCheckInfo> {
        requireCompatible("update-check")?.let { return it.asFailure() }
        return call("update-check", timeoutMs = 30_000L, transform = ::parseUpdateCheckInfo)
    }

    suspend fun updateDownload(): DaemonClientResult<UpdateDownloadInfo> {
        requireCompatible("update-download")?.let { return it.asFailure() }
        return call("update-download", timeoutMs = 600_000L, transform = ::parseUpdateDownloadInfo)
    }

    suspend fun updateInstall(
        modulePath: String = "",
        apkPath: String = "",
    ): DaemonClientResult<UpdateInstallInfo> {
        requireCompatible("update-install")?.let { return it.asFailure() }
        val params = buildJsonObject {
            if (modulePath.isNotBlank()) {
                put("module_path", modulePath)
            }
            if (apkPath.isNotBlank()) {
                put("apk_path", apkPath)
            }
        }
        return call("update-install", params, timeoutMs = ACCEPT_TIMEOUT_MS) {
            json.parseUpdateInstallInfo(it)
        }
    }

    // ---- Meta ----

    suspend fun ipcContract(): DaemonClientResult<IpcContractInfo> =
        call("ipc.contract") { element ->
            json.decodeFromJsonElement(IpcContractInfo.serializer(), element)
        }

    suspend fun version(): DaemonClientResult<VersionInfo> =
        call("version", transform = ::parseVersionInfo)

    // ---- Internal helpers ----

    private suspend fun <T> call(
        method: String,
        params: JsonObject = emptyJsonObject(),
        timeoutMs: Long = 5_000L,
        transform: (JsonElement) -> T
    ): DaemonClientResult<T> {
        return executor.execute(method, params, timeoutMs).toDaemonClientResult(transform)
    }

    private suspend fun callConfigMutation(
        method: String,
        params: JsonObject,
    ): DaemonClientResult<ConfigMutationInfo> {
        return executor.execute(method, params, timeoutMs = 60_000L)
            .toDaemonClientResultEnvelope { element ->
                val info = json.parseConfigMutationInfo(element)
                if (!info.ok) {
                    DaemonClientResult.DaemonError(
                        DaemonClientErrorCodes.CONFIG_ERROR,
                        info.message.ifBlank { "configuration was not applied" },
                        element,
                    )
                } else {
                    DaemonClientResult.Ok(info)
                }
            }
    }

    private suspend fun requireCompatible(vararg requiredMethods: String): DaemonClientResult<Unit>? {
        return when (val result = version()) {
            is DaemonClientResult.Ok -> {
                val info = result.data
                if ("ipc.contract" !in info.supportedMethods) {
                    return DaemonClientResult.DaemonError(
                        DaemonClientErrorCodes.COMPATIBILITY,
                        "APK и модуль несовместимы: daemon не рекламирует IPC contract",
                    )
                }
                val contract = when (val contractResult = ipcContract()) {
                    is DaemonClientResult.Ok -> contractResult.data
                    is DaemonClientResult.DaemonError -> return DaemonClientResult.DaemonError(
                        DaemonClientErrorCodes.COMPATIBILITY,
                        "APK и модуль несовместимы: daemon не отдал IPC contract (${contractResult.message})",
                        contractResult.details,
                    )
                    is DaemonClientResult.ParseError -> return DaemonClientResult.DaemonError(
                        DaemonClientErrorCodes.COMPATIBILITY,
                        "APK и модуль несовместимы: некорректный IPC contract",
                    )
                    is DaemonClientResult.RootDenied -> return contractResult
                    is DaemonClientResult.Timeout -> return contractResult
                    is DaemonClientResult.DaemonNotFound -> return contractResult
                    is DaemonClientResult.Failure -> return contractResult
                }
                val issue = ipcCompatibilityIssue(
                    info = info,
                    contract = contract,
                    apkVersion = BuildConfig.VERSION_NAME,
                    requiredMethods = requiredMethods.toList(),
                    minControlProtocolVersion = MIN_CONTROL_PROTOCOL_VERSION,
                    minSchemaVersion = MIN_SCHEMA_VERSION,
                )
                if (issue == null) {
                    null
                } else {
                    DaemonClientResult.DaemonError(DaemonClientErrorCodes.COMPATIBILITY, issue)
                }
            }
            is DaemonClientResult.DaemonError ->
                DaemonClientResult.DaemonError(
                    DaemonClientErrorCodes.COMPATIBILITY,
                    "APK и модуль несовместимы: daemon не сообщает capabilities (${result.message})",
                    result.details,
                )
            is DaemonClientResult.RootDenied -> result
            is DaemonClientResult.Timeout -> result
            is DaemonClientResult.DaemonNotFound -> result
            is DaemonClientResult.ParseError -> DaemonClientResult.DaemonError(
                DaemonClientErrorCodes.COMPATIBILITY,
                "APK и модуль несовместимы: некорректный ответ version",
            )
            is DaemonClientResult.Failure -> result
        }
    }

}

private const val ACCEPT_TIMEOUT_MS = 10_000L
