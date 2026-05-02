package com.rknnovpn.panel.ipc

import com.rknnovpn.panel.BuildConfig
import com.rknnovpn.panel.model.AppInfo
import com.rknnovpn.panel.model.AuditReport
import com.rknnovpn.panel.model.BackendHealthSnapshot
import com.rknnovpn.panel.model.BackendStatusV2
import com.rknnovpn.panel.model.DaemonStatus
import com.rknnovpn.panel.model.DesiredStateV2
import com.rknnovpn.panel.model.DnsConfig
import com.rknnovpn.panel.model.InboundsConfig
import com.rknnovpn.panel.model.Node
import com.rknnovpn.panel.model.NodeProbeResultV2
import com.rknnovpn.panel.model.ProfileConfig
import com.rknnovpn.panel.model.RoutingConfig
import kotlinx.serialization.builtins.ListSerializer
import kotlinx.serialization.json.add
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.decodeFromJsonElement
import kotlinx.serialization.json.encodeToJsonElement
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.put
import kotlinx.serialization.json.putJsonArray
import java.util.UUID
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

    @Volatile
    private var compatibilityCache: CompatibilityCache? = null

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
        return when (val statusResult = backendStatus(includeHealthRefresh = true)) {
            is DaemonClientResult.Ok -> DaemonClientResult.Ok(statusResult.data.toDaemonStatus())
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
    suspend fun profileGet(allowModuleRepair: Boolean = false): DaemonClientResult<ProfileConfig> {
        requireCompatible("profile.get", allowModuleRepair = allowModuleRepair)?.let { return it.asFailure() }
        return call("profile.get", allowModuleRepair = allowModuleRepair) {
            json.decodeFromJsonElement(ProfileConfig.serializer(), it)
        }
    }

    /** Replace the full daemon-owned profile document. */
    suspend fun profileApply(
        config: ProfileConfig,
        reload: Boolean = true,
    ): DaemonClientResult<ConfigMutationInfo> {
        requireCompatible("profile.apply", allowModuleRepair = true)?.let { return it.asFailure() }
        val params = buildJsonObject {
            put("profile", json.encodeToJsonElement(ProfileConfig.serializer(), config))
            put("reload", reload)
        }
        if (jsonRpcRequestFrameByteSize("profile.apply", params) > IPC_MAX_FRAME_BYTES) {
            return payloadTooLargeError()
        }
        return callConfigMutation(
            "profile.apply",
            params,
        )
    }

    suspend fun profileImportNodes(
        nodes: List<Node>,
        reload: Boolean = true,
    ): DaemonClientResult<ConfigMutationInfo> {
        requireCompatible("profile.importNodes", allowModuleRepair = true)?.let { return it.asFailure() }
        val params = importNodesParams(nodes, reload)
        if (jsonRpcRequestFrameByteSize("profile.importNodes", params) > IPC_MAX_FRAME_BYTES) {
            return profileImportNodesBatched(nodes, reload)
        }
        return callConfigMutation(
            "profile.importNodes",
            params,
        )
    }

    suspend fun profileSetActiveNode(
        nodeId: String,
        reload: Boolean = true,
    ): DaemonClientResult<ConfigMutationInfo> {
        requireCompatible("profile.setActiveNode", allowModuleRepair = true)?.let { return it.asFailure() }
        return callConfigMutation(
            "profile.setActiveNode",
            buildJsonObject {
                put("nodeId", nodeId)
                put("reload", reload)
            },
        )
    }

    suspend fun profileClearActiveNode(
        reload: Boolean = true,
    ): DaemonClientResult<ConfigMutationInfo> {
        requireCompatible("profile.patch", allowModuleRepair = true)?.let { return it.asFailure() }
        return callConfigMutation(
            "profile.patch",
            buildJsonObject {
                put("activeNodeId", JsonNull)
                put("reload", reload)
            },
        )
    }

    suspend fun profileNodeUpsert(
        node: Node,
        reload: Boolean = true,
    ): DaemonClientResult<ConfigMutationInfo> {
        requireCompatible("profile.node.upsert", allowModuleRepair = true)?.let { return it.asFailure() }
        return callConfigMutation(
            "profile.node.upsert",
            buildJsonObject {
                put("node", json.encodeToJsonElement(Node.serializer(), node))
                put("reload", reload)
            },
        )
    }

    suspend fun profileNodeRemove(
        nodeId: String,
        reload: Boolean = true,
    ): DaemonClientResult<ConfigMutationInfo> {
        requireCompatible("profile.node.remove", allowModuleRepair = true)?.let { return it.asFailure() }
        return callConfigMutation(
            "profile.node.remove",
            buildJsonObject {
                put("nodeId", nodeId)
                put("reload", reload)
            },
        )
    }

    suspend fun profileRoutingPatch(
        routing: RoutingConfig,
        reload: Boolean = true,
    ): DaemonClientResult<ConfigMutationInfo> {
        requireCompatible("profile.routing.patch", allowModuleRepair = true)?.let { return it.asFailure() }
        return callConfigMutation(
            "profile.routing.patch",
            buildJsonObject {
                put("routing", json.encodeToJsonElement(RoutingConfig.serializer(), routing))
                put("reload", reload)
            },
        )
    }

    suspend fun profileDNSPatch(
        dns: DnsConfig,
        reload: Boolean = true,
    ): DaemonClientResult<ConfigMutationInfo> {
        requireCompatible("profile.dns.patch", allowModuleRepair = true)?.let { return it.asFailure() }
        return callConfigMutation(
            "profile.dns.patch",
            buildJsonObject {
                put("dns", json.encodeToJsonElement(DnsConfig.serializer(), dns))
                put("reload", reload)
            },
        )
    }

    suspend fun profileInboundPatch(
        inbounds: InboundsConfig,
        reload: Boolean = true,
    ): DaemonClientResult<ConfigMutationInfo> {
        requireCompatible("profile.inbound.patch", allowModuleRepair = true)?.let { return it.asFailure() }
        return callConfigMutation(
            "profile.inbound.patch",
            buildJsonObject {
                put("inbounds", json.encodeToJsonElement(InboundsConfig.serializer(), inbounds))
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
        requireCompatible("subscription.preview", allowModuleRepair = true)?.let { return it.asFailure() }
        val params = buildJsonObject { put("url", url) }
        return call("subscription.preview", params, timeoutMs = 60_000L, allowModuleRepair = true) { element ->
            json.decodeFromJsonElement(SubscriptionPreviewInfo.serializer(), element)
        }
    }

    suspend fun subscriptionRefresh(url: String): DaemonClientResult<ConfigMutationInfo> {
        requireCompatible("subscription.refresh", allowModuleRepair = true)?.let { return it.asFailure() }
        val params = buildJsonObject { put("url", url) }
        return callConfigMutation("subscription.refresh", params)
    }

    suspend fun subscriptionCommitPreview(previewId: String): DaemonClientResult<ConfigMutationInfo> {
        requireCompatible("subscription.commitPreview", allowModuleRepair = true)?.let { return it.asFailure() }
        val params = buildJsonObject { put("previewId", previewId) }
        return callConfigMutation("subscription.commitPreview", params)
    }

    suspend fun nodeTest(
        nodeIds: List<String> = emptyList(),
        url: String = "",
        timeoutMs: Int = 5_000,
        mode: String = "fast",
    ): DaemonClientResult<NodeTestInfo> {
        when (val result = diagnosticsTestNodes(nodeIds, url, timeoutMs, mode)) {
            is DaemonClientResult.Ok -> {
                return DaemonClientResult.Ok(result.data.toNodeTestInfo(url))
            }
            is DaemonClientResult.DaemonError -> return result.asFailure()
            is DaemonClientResult.RootDenied -> return result.asFailure()
            is DaemonClientResult.Timeout -> return result.asFailure()
            is DaemonClientResult.DaemonNotFound -> return result.asFailure()
            is DaemonClientResult.DaemonUnavailable -> return result.asFailure()
            is DaemonClientResult.ParseError -> return result.asFailure()
            is DaemonClientResult.Failure -> return result.asFailure()
        }
    }

    suspend fun backendStatus(
        includeHealthRefresh: Boolean = false,
        includeCompatibility: Boolean = false,
    ): DaemonClientResult<BackendStatusV2> {
        val params = buildJsonObject {
            if (includeHealthRefresh) {
                put("includeHealthRefresh", true)
            }
            if (!includeCompatibility) {
                put("includeCompatibility", false)
            }
        }
        val timeoutMs = if (includeHealthRefresh) 30_000L else 5_000L
        return call(
            "backend.status",
            params = params,
            timeoutMs = timeoutMs,
        ) {
            json.decodeFromJsonElement(BackendStatusV2.serializer(), it)
        }
    }

    suspend fun backendStart(): DaemonClientResult<BackendStatusV2> {
        requireCompatible("backend.start", "backend.status", allowModuleRepair = true)?.let { return it.asFailure() }
        return call("backend.start", timeoutMs = ACCEPT_TIMEOUT_MS, allowModuleRepair = true) {
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
        requireCompatible("backend.restart", "backend.status", allowModuleRepair = true)?.let { return it.asFailure() }
        return call("backend.restart", timeoutMs = ACCEPT_TIMEOUT_MS, allowModuleRepair = true) {
            json.decodeFromJsonElement(BackendStatusV2.serializer(), it)
        }
    }

    suspend fun backendReset(): DaemonClientResult<BackendStatusV2> {
        requireCompatible("backend.reset", "backend.status", allowModuleRepair = true)?.let { return it.asFailure() }
        return call("backend.reset", timeoutMs = ACCEPT_TIMEOUT_MS, allowModuleRepair = true) {
            json.decodeFromJsonElement(BackendStatusV2.serializer(), it)
        }
    }

    suspend fun backendApplyDesiredState(desiredState: DesiredStateV2): DaemonClientResult<BackendStatusV2> {
        requireCompatible("backend.applyDesiredState", "backend.status", allowModuleRepair = true)?.let { return it.asFailure() }
        return call(
            method = "backend.applyDesiredState",
            params = json.encodeToJsonElement(DesiredStateV2.serializer(), desiredState).jsonObject,
            timeoutMs = 15_000L,
            allowModuleRepair = true,
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
        mode: String = "fast",
    ): DaemonClientResult<List<NodeProbeResultV2>> {
        requireCompatible("diagnostics.testNodes")?.let { return it.asFailure() }
        val normalizedMode = if (mode == "full" || mode == "throughput") "full" else "fast"
        val params = buildJsonObject {
            putJsonArray("node_ids") {
                nodeIds.forEach { add(it) }
            }
            if (url.isNotBlank()) {
                put("url", url)
            }
            put("timeout_ms", timeoutMs)
            put("mode", normalizedMode)
        }
        return call("diagnostics.testNodes", params, timeoutMs = nodeProbeCallTimeoutMs(nodeIds.size, timeoutMs, normalizedMode)) {
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
        return call(
            "update-check",
            timeoutMs = 30_000L,
            transform = ::parseUpdateCheckInfo,
        )
    }

    suspend fun updateDownload(): DaemonClientResult<UpdateDownloadInfo> {
        requireCompatible("update-download")?.let { return it.asFailure() }
        return call(
            "update-download",
            timeoutMs = 600_000L,
            transform = ::parseUpdateDownloadInfo,
        )
    }

    suspend fun updateInstall(
        modulePath: String = "",
        apkPath: String = "",
    ): DaemonClientResult<UpdateInstallInfo> {
        requireCompatible("update-install")?.let { return it.asFailure() }
        compatibilityCache = null
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

    suspend fun ipcContract(allowModuleRepair: Boolean = false): DaemonClientResult<IpcContractInfo> =
        call("ipc.contract", allowModuleRepair = allowModuleRepair) { element ->
            json.decodeFromJsonElement(IpcContractInfo.serializer(), element)
        }

    suspend fun compatibilityCheck(
        requiredMethods: Collection<String> = emptyList(),
        allowModuleRepair: Boolean = false,
    ): DaemonClientResult<CompatibilityCheckInfo> {
        val params = buildJsonObject {
            putJsonArray("requiredMethods") {
                requiredMethods.distinct().forEach { add(it) }
            }
        }
        return call("compat.check", params = params, allowModuleRepair = allowModuleRepair) { element ->
            json.parseCompatibilityCheckInfo(element)
        }
    }

    suspend fun version(allowModuleRepair: Boolean = false): DaemonClientResult<VersionInfo> =
        call("version", allowModuleRepair = allowModuleRepair, transform = ::parseVersionInfo)

    suspend fun moduleState(): DaemonctlResult =
        executor.execute(
            method = "module.state",
            params = emptyJsonObject(),
            timeoutMs = 5_000L,
            allowModuleRepair = false,
        )

    suspend fun moduleRepair(): DaemonctlResult {
        compatibilityCache = null
        return executor.execute(
            method = "module.repair",
            params = emptyJsonObject(),
            timeoutMs = 5_000L,
            allowModuleRepair = false,
        )
    }

    // ---- Internal helpers ----

    private suspend fun <T> call(
        method: String,
        params: JsonObject = emptyJsonObject(),
        timeoutMs: Long = 5_000L,
        allowModuleRepair: Boolean = false,
        transform: (JsonElement) -> T
    ): DaemonClientResult<T> {
        val result = executor.execute(method, params, timeoutMs, allowModuleRepair)
        invalidateCompatibilityCacheOnTransportChange(result)
        return result.toDaemonClientResult(transform)
    }

    private fun nodeProbeCallTimeoutMs(selectedNodeCount: Int, perProbeTimeoutMs: Int, mode: String): Long {
        val workers = 6
        val batches = if (selectedNodeCount > 0) {
            ((selectedNodeCount + workers - 1) / workers).coerceAtLeast(1)
        } else {
            // Empty node_ids means "all nodes"; leave room for several bounded batches.
            6
        }
        val stages = if (mode == "full") 4L else 3L
        val perNodeWorstCase = perProbeTimeoutMs.toLong().coerceAtLeast(1_000L) * stages
        return (perNodeWorstCase * batches + 5_000L).coerceIn(15_000L, 60_000L)
    }

    private suspend fun callConfigMutation(
        method: String,
        params: JsonObject,
    ): DaemonClientResult<ConfigMutationInfo> {
        val result = executor.execute(method, params, timeoutMs = 60_000L, allowModuleRepair = true)
        invalidateCompatibilityCacheOnTransportChange(result)
        return result.toDaemonClientResultEnvelope { element ->
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

    private fun invalidateCompatibilityCacheOnTransportChange(result: DaemonctlResult) {
        if (result is DaemonctlResult.DaemonUnavailable ||
            result is DaemonctlResult.DaemonNotFound ||
            result is DaemonctlResult.Timeout ||
            result is DaemonctlResult.Error && result.code == DaemonClientErrorCodes.METHOD_NOT_FOUND
        ) {
            compatibilityCache = null
        }
    }

    private suspend fun profileImportNodesBatched(
        nodes: List<Node>,
        reload: Boolean,
    ): DaemonClientResult<ConfigMutationInfo> {
        requireCompatible("profile.importNodesBatch", allowModuleRepair = true)?.let {
            return it.asFailure()
        }
        requireCompatible("profile.commitImportBatch", allowModuleRepair = true)?.let {
            return it.asFailure()
        }
        val batches = splitImportNodeBatches(nodes)
        if (batches.isEmpty()) {
            return payloadTooLargeError()
        }
        val batchId = "apk-${UUID.randomUUID()}"
        val totalBatches = batches.size
        for (batch in batches) {
            val result = call(
                "profile.importNodesBatch",
                importNodesBatchParams(batchId, totalBatches, batch),
                timeoutMs = 60_000L,
                allowModuleRepair = true,
            ) {
                Unit
            }
            if (result !is DaemonClientResult.Ok) {
                return result.asFailure()
            }
        }
        return callConfigMutation(
            "profile.commitImportBatch",
            buildJsonObject {
                put("batchId", batchId)
                put("reload", reload)
            },
        )
    }

    private fun splitImportNodeBatches(nodes: List<Node>): List<List<Node>> {
        val result = mutableListOf<List<Node>>()
        val current = mutableListOf<Node>()
        val maxTotalBatchesDigits = nodes.size.coerceAtLeast(1)
        for (node in nodes) {
            val candidate = current + node
            val candidateParams = importNodesBatchParams(
                batchId = "apk-00000000-0000-0000-0000-000000000000",
                totalBatches = maxTotalBatchesDigits,
                nodes = candidate,
            )
            if (
                current.isNotEmpty() &&
                jsonRpcRequestFrameByteSize("profile.importNodesBatch", candidateParams) > IPC_IMPORT_BATCH_TARGET_BYTES
            ) {
                result += current.toList()
                current.clear()
            }
            current += node
            val singleParams = importNodesBatchParams(
                batchId = "apk-00000000-0000-0000-0000-000000000000",
                totalBatches = maxTotalBatchesDigits,
                nodes = current,
            )
            if (jsonRpcRequestFrameByteSize("profile.importNodesBatch", singleParams) > IPC_IMPORT_BATCH_TARGET_BYTES) {
                return emptyList()
            }
        }
        if (current.isNotEmpty()) {
            result += current.toList()
        }
        return result
    }

    private fun importNodesParams(nodes: List<Node>, reload: Boolean): JsonObject =
        buildJsonObject {
            put("nodes", json.encodeToJsonElement(ListSerializer(Node.serializer()), nodes))
            put("reload", reload)
        }

    private fun importNodesBatchParams(
        batchId: String,
        totalBatches: Int,
        nodes: List<Node>,
    ): JsonObject = buildJsonObject {
        put("batchId", batchId)
        put("totalBatches", totalBatches)
        put("nodes", json.encodeToJsonElement(ListSerializer(Node.serializer()), nodes))
    }

    private fun payloadTooLargeError(): DaemonClientResult.DaemonError =
        DaemonClientResult.DaemonError(
            code = -32602,
            message = IPC_PAYLOAD_TOO_LARGE_MESSAGE,
        )

    private suspend fun requireCompatible(
        vararg requiredMethods: String,
        allowModuleRepair: Boolean = false,
    ): DaemonClientResult<Unit>? {
        val required = requiredMethods.toList()
        val bootstrapMethods = listOf("compat.check", "ipc.contract", "version")
        val compatibilityRequired = required + listOf("compat.check")
        compatibilityCache
            ?.takeIf { cache -> cache.supports(required + bootstrapMethods) }
            ?.let { cache ->
                val cachedIssue = ipcCompatibilityIssue(
                    info = cache.info,
                    contract = cache.contract,
                    apkVersion = BuildConfig.VERSION_NAME,
                    requiredMethods = compatibilityRequired,
                    minControlProtocolVersion = MIN_CONTROL_PROTOCOL_VERSION,
                    minSchemaVersion = MIN_SCHEMA_VERSION,
                )
                return if (cachedIssue == null) {
                    null
                } else {
                    DaemonClientResult.DaemonError(DaemonClientErrorCodes.COMPATIBILITY, cachedIssue)
                }
            }
        return when (val result = compatibilityCheck(required + bootstrapMethods, allowModuleRepair)) {
            is DaemonClientResult.Ok -> {
                val info = result.data.version
                if (bootstrapMethods.any { it !in info.supportedMethods }) {
                    return DaemonClientResult.DaemonError(
                        DaemonClientErrorCodes.COMPATIBILITY,
                        "APK и модуль несовместимы: daemon не рекламирует compatibility check",
                    )
                }
                val contract = result.data.contract
                val fingerprint = info.compatibilityFingerprint()
                compatibilityCache
                    ?.takeIf { it.fingerprint == fingerprint }
                    ?.takeIf { cache -> cache.supports(required + bootstrapMethods) }
                    ?.let { cache ->
                        val cachedIssue = ipcCompatibilityIssue(
                            info = info,
                            contract = cache.contract,
                            apkVersion = BuildConfig.VERSION_NAME,
                            requiredMethods = compatibilityRequired,
                            minControlProtocolVersion = MIN_CONTROL_PROTOCOL_VERSION,
                            minSchemaVersion = MIN_SCHEMA_VERSION,
                        )
                        return if (cachedIssue == null) {
                            compatibilityCache = cache.copy(info = info)
                            null
                        } else {
                            DaemonClientResult.DaemonError(DaemonClientErrorCodes.COMPATIBILITY, cachedIssue)
                        }
                    }
                val issue = ipcCompatibilityIssue(
                    info = info,
                    contract = contract,
                    apkVersion = BuildConfig.VERSION_NAME,
                    requiredMethods = compatibilityRequired,
                    minControlProtocolVersion = MIN_CONTROL_PROTOCOL_VERSION,
                    minSchemaVersion = MIN_SCHEMA_VERSION,
                )
                if (issue == null) {
                    compatibilityCache = CompatibilityCache(
                        info = info,
                        fingerprint = fingerprint,
                        contract = contract,
                        supportedMethods = contract.methods.mapTo(mutableSetOf()) { it.method },
                    )
                    null
                } else {
                    DaemonClientResult.DaemonError(DaemonClientErrorCodes.COMPATIBILITY, issue)
                }
            }
            is DaemonClientResult.DaemonError ->
                DaemonClientResult.DaemonError(
                    DaemonClientErrorCodes.COMPATIBILITY,
                    "APK и модуль несовместимы: daemon не выполнил compatibility check (${result.message})",
                    result.details,
                )
            is DaemonClientResult.RootDenied -> result
            is DaemonClientResult.Timeout -> result
            is DaemonClientResult.DaemonNotFound -> result
            is DaemonClientResult.DaemonUnavailable -> result
            is DaemonClientResult.ParseError -> DaemonClientResult.DaemonError(
                DaemonClientErrorCodes.COMPATIBILITY,
                "APK и модуль несовместимы: некорректный ответ compat.check",
            )
            is DaemonClientResult.Failure -> result
        }
    }

}

private data class CompatibilityCache(
    val info: VersionInfo,
    val fingerprint: String,
    val contract: IpcContractInfo,
    val supportedMethods: Set<String>,
) {
    fun supports(methods: Collection<String>): Boolean =
        supportedMethods.isNotEmpty() && methods.all { it in supportedMethods }
}

private fun VersionInfo.compatibilityFingerprint(): String {
    compatibilityFingerprint.takeIf { it.isNotBlank() }?.let { return it }
    return listOf(
        daemonVersion,
        moduleVersion,
        controlProtocolVersion.toString(),
        schemaVersion.toString(),
        ipcContractVersion.toString(),
        contractHash,
        daemonPid.toString(),
        socketInode,
    ).joinToString(":")
}

private const val ACCEPT_TIMEOUT_MS = 10_000L
