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
import com.rknnovpn.panel.model.RuntimeOperationResult
import com.rknnovpn.panel.model.RuntimeOperationStatus
import kotlinx.coroutines.delay
import kotlinx.serialization.builtins.ListSerializer
import kotlinx.serialization.json.add
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.booleanOrNull
import kotlinx.serialization.json.contentOrNull
import kotlinx.serialization.json.decodeFromJsonElement
import kotlinx.serialization.json.encodeToJsonElement
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
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
        private val INVALID_RESPONSE_RECOVERY_METHODS = setOf(
            "backend.status",
            "profile.get",
            "version",
            "compat.check",
            "ipc.contract",
        )
        private val BRIDGE_PARSE_RETRY_METHODS = setOf(
            "backend.status",
            "compat.check",
            "ipc.contract",
            "profile.get",
            "version",
        )
        private const val INVALID_RESPONSE_REPAIR_COOLDOWN_MS = 30_000L
        private const val INVALID_RESPONSE_STABLE_FAILURE_THRESHOLD = 3
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
    @Volatile
    private var lastInvalidResponseRepairAtMs: Long = 0L
    @Volatile
    private var lastInvalidResponseFingerprint: String = ""
    @Volatile
    private var lastInvalidResponseFingerprintCount: Int = 0

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

    suspend fun subscriptionRefresh(
        url: String,
        reload: Boolean? = null,
    ): DaemonClientResult<ConfigMutationInfo> {
        requireCompatible("subscription.refresh", allowModuleRepair = true)?.let { return it.asFailure() }
        val params = buildJsonObject {
            put("url", url)
            reload?.let { put("reload", it) }
        }
        return callConfigMutation("subscription.refresh", params)
    }

    suspend fun subscriptionCommitPreview(
        previewId: String,
        reload: Boolean? = null,
    ): DaemonClientResult<ConfigMutationInfo> {
        requireCompatible("subscription.commitPreview", allowModuleRepair = true)?.let { return it.asFailure() }
        val params = buildJsonObject {
            put("previewId", previewId)
            reload?.let { put("reload", it) }
        }
        return callConfigMutation("subscription.commitPreview", params)
    }

    suspend fun nodeTest(
        nodeIds: List<String> = emptyList(),
        url: String = "",
        timeoutMs: Int = 3_000,
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
        val status = call(
            "backend.status",
            params = params,
            timeoutMs = timeoutMs,
        ) {
            json.decodeFromJsonElement(BackendStatusV2.serializer(), it)
        }

        if (status is DaemonClientResult.ParseError) {
            compatibilityCache = null
            val compat = compatibilityCheck(
                requiredMethods = REQUIRED_METHODS,
                allowModuleRepair = true,
            )
            if (compat is DaemonClientResult.Ok) {
                val issue = ipcCompatibilityIssue(
                    info = compat.data.version,
                    contract = compat.data.contract,
                    apkVersion = BuildConfig.VERSION_NAME,
                    requiredMethods = REQUIRED_METHODS + "compat.check",
                    minControlProtocolVersion = MIN_CONTROL_PROTOCOL_VERSION,
                    minSchemaVersion = MIN_SCHEMA_VERSION,
                )
                return DaemonClientResult.DaemonError(
                    DaemonClientErrorCodes.COMPATIBILITY,
                    issue ?: "Daemon status schema is incompatible with this APK",
                )
            }
        }

        return status
    }

    suspend fun backendStart(): DaemonClientResult<BackendStatusV2> {
        requireCompatible("backend.start", "backend.status", allowModuleRepair = true)?.let { return it.asFailure() }
        return callBackendStatusMutation("backend.start", timeoutMs = ACCEPT_TIMEOUT_MS, allowModuleRepair = true)
    }

    suspend fun backendStop(): DaemonClientResult<BackendStatusV2> {
        requireCompatible("backend.stop", "backend.status")?.let { return it.asFailure() }
        return callBackendStatusMutation("backend.stop", timeoutMs = ACCEPT_TIMEOUT_MS)
    }

    suspend fun backendRestart(): DaemonClientResult<BackendStatusV2> {
        requireCompatible("backend.restart", "backend.status", allowModuleRepair = true)?.let { return it.asFailure() }
        return callBackendStatusMutation("backend.restart", timeoutMs = ACCEPT_TIMEOUT_MS, allowModuleRepair = true)
    }

    suspend fun backendReset(): DaemonClientResult<BackendStatusV2> {
        requireCompatible("backend.reset", "backend.status", allowModuleRepair = true)?.let { return it.asFailure() }
        return callBackendStatusMutation("backend.reset", timeoutMs = ACCEPT_TIMEOUT_MS, allowModuleRepair = true)
    }

    suspend fun backendApplyDesiredState(desiredState: DesiredStateV2): DaemonClientResult<BackendStatusV2> {
        requireCompatible("backend.applyDesiredState", "backend.status", allowModuleRepair = true)?.let { return it.asFailure() }
        return callBackendStatusMutation(
            method = "backend.applyDesiredState",
            params = json.encodeToJsonElement(DesiredStateV2.serializer(), desiredState).jsonObject,
            timeoutMs = 15_000L,
            allowModuleRepair = true,
        )
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
        timeoutMs: Int = 3_000,
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
        val result = call(
            "update-download",
            timeoutMs = 600_000L,
            allowBridge = false,
            transform = ::parseUpdateDownloadInfo,
        )
        return if (result is DaemonClientResult.ParseError) {
            reconcileMutatingParseError("update-download", result)
        } else {
            result
        }
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
        val result = call("update-install", params, timeoutMs = ACCEPT_TIMEOUT_MS, allowBridge = false) {
            json.parseUpdateInstallInfo(it)
        }
        return if (result is DaemonClientResult.ParseError) {
            reconcileMutatingParseError("update-install", result)
        } else {
            result
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
        return call(
            "compat.check",
            params = params,
            allowModuleRepair = allowModuleRepair,
            allowBridge = compatibilityCache != null,
        ) { element ->
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
        allowBridge: Boolean = true,
        transform: (JsonElement) -> T
    ): DaemonClientResult<T> {
        val result = executor.execute(method, params, timeoutMs, allowModuleRepair, allowBridge)
        invalidateCompatibilityCacheOnTransportChange(result)
        val converted = result.toDaemonClientResult(transform)
        if (
            converted is DaemonClientResult.ParseError &&
            result is DaemonctlResult.Success &&
            result.transport == DaemonctlTransport.BRIDGE &&
            method in BRIDGE_PARSE_RETRY_METHODS
        ) {
            executor.disableBridge("parse failure for $method")
            val retry = executor.execute(
                method = method,
                params = params,
                timeoutMs = timeoutMs,
                allowModuleRepair = allowModuleRepair,
                allowBridge = false,
            )
            invalidateCompatibilityCacheOnTransportChange(retry)
            val retryConverted = retry.toDaemonClientResult(transform)
            if (retryConverted !is DaemonClientResult.ParseError) {
                return retryConverted
            }
            return recoverFromInvalidResponse(
                method = method,
                params = params,
                timeoutMs = timeoutMs,
                allowModuleRepair = allowModuleRepair,
                transform = transform,
                previous = retryConverted,
            )
        }

        if (converted is DaemonClientResult.ParseError) {
            return recoverFromInvalidResponse(
                method = method,
                params = params,
                timeoutMs = timeoutMs,
                allowModuleRepair = allowModuleRepair,
                transform = transform,
                previous = converted,
            )
        }
        return converted
    }

    private suspend fun <T> recoverFromInvalidResponse(
        method: String,
        params: JsonObject,
        timeoutMs: Long,
        allowModuleRepair: Boolean,
        transform: (JsonElement) -> T,
        previous: DaemonClientResult.ParseError,
    ): DaemonClientResult<T> {
        if (method !in INVALID_RESPONSE_RECOVERY_METHODS) {
            return previous
        }

        executor.disableBridge("invalid response for $method")

        val oneShot = executor.execute(
            method = method,
            params = params,
            timeoutMs = timeoutMs.coerceAtLeast(5_000L),
            allowModuleRepair = allowModuleRepair,
            allowBridge = false,
        )
        invalidateCompatibilityCacheOnTransportChange(oneShot)

        val oneShotConverted = oneShot.toDaemonClientResult(transform)
        if (oneShotConverted !is DaemonClientResult.ParseError) {
            clearInvalidResponseFingerprint(method)
            return oneShotConverted
        }

        val oneShotFingerprintCount = recordInvalidResponseFingerprint(method, oneShotConverted.raw)
        if (oneShotFingerprintCount >= INVALID_RESPONSE_STABLE_FAILURE_THRESHOLD) {
            return stableInvalidResponseError(method, oneShotConverted.raw)
        }

        val now = System.currentTimeMillis()
        if (now - lastInvalidResponseRepairAtMs < INVALID_RESPONSE_REPAIR_COOLDOWN_MS) {
            return DaemonClientResult.DaemonError(
                DaemonClientErrorCodes.COMPATIBILITY,
                "Daemon returned an incompatible response for $method. " +
                    "Repair cooldown is active; raw=${oneShotConverted.raw.take(300)}",
            )
        }
        lastInvalidResponseRepairAtMs = now

        val state = moduleState()
        if (state !is DaemonctlResult.Success) {
            return DaemonClientResult.DaemonError(
                DaemonClientErrorCodes.COMPATIBILITY,
                "Daemon response is invalid and module state could not be checked before repair: $state",
            )
        }
        moduleStateBlockingError(state)?.let { return it }

        val repair = moduleRepair()
        if (repair !is DaemonctlResult.Success) {
            return DaemonClientResult.DaemonError(
                DaemonClientErrorCodes.COMPATIBILITY,
                "Daemon response is invalid and module repair did not start: $repair",
            )
        }

        delay(1_500L)

        compatibilityCache = null
        val afterRepair = executor.execute(
            method = method,
            params = params,
            timeoutMs = timeoutMs.coerceAtLeast(5_000L),
            allowModuleRepair = true,
            allowBridge = false,
        )
        invalidateCompatibilityCacheOnTransportChange(afterRepair)

        val afterRepairConverted = afterRepair.toDaemonClientResult(transform)
        if (afterRepairConverted !is DaemonClientResult.ParseError) {
            clearInvalidResponseFingerprint(method)
            return afterRepairConverted
        }

        val afterRepairFingerprintCount = recordInvalidResponseFingerprint(method, afterRepairConverted.raw)
        if (afterRepairFingerprintCount >= INVALID_RESPONSE_STABLE_FAILURE_THRESHOLD) {
            return stableInvalidResponseError(method, afterRepairConverted.raw)
        }

        return DaemonClientResult.DaemonError(
            DaemonClientErrorCodes.COMPATIBILITY,
            "APK и модуль/daemon несовместимы: daemon вернул JSON, но APK не смог разобрать схему. " +
                "Нужна переустановка модуля/APK одной версии или reboot после staged update.",
            JsonPrimitive(afterRepairConverted.raw.take(1000)),
        )
    }

    private fun moduleStateBlockingError(
        state: DaemonctlResult.Success,
    ): DaemonClientResult.DaemonError? {
        val obj = runCatching { state.data.jsonObject }.getOrNull()
            ?: return DaemonClientResult.DaemonError(
                DaemonClientErrorCodes.COMPATIBILITY,
                "Daemon response is invalid and module.state did not return a JSON object before repair.",
                state.data,
            )
        val status = obj["status"]?.jsonPrimitive?.contentOrNull.orEmpty()
        val message = obj["message"]?.jsonPrimitive?.contentOrNull.orEmpty()
        val repairStatus = obj["repairStatus"]?.jsonPrimitive?.contentOrNull.orEmpty()
        val socketPresent = obj["socketPresent"]?.jsonPrimitive?.booleanOrNull ?: false
        val socketAccepting = obj["socketAccepting"]?.jsonPrimitive?.booleanOrNull ?: false
        return when {
            status == "pending_update" || status == "pending_remove" ->
                DaemonClientResult.DaemonError(
                    DaemonClientErrorCodes.COMPATIBILITY,
                    message.ifBlank { "Module state is $status; reboot is required before IPC recovery." },
                    state.data,
                )
            status == "disabled" || status == "missing" || status == "service_missing" ||
                status == "daemonctl_missing" || status == "daemon_missing" ->
                DaemonClientResult.DaemonError(
                    DaemonClientErrorCodes.COMPATIBILITY,
                    message.ifBlank { "Module state is $status; reinstall or enable the module before IPC recovery." },
                    state.data,
                )
            repairStatus == "repair_running" ->
                DaemonClientResult.DaemonError(
                    DaemonClientErrorCodes.COMPATIBILITY,
                    "Module repair is already running; waiting before retrying invalid-response recovery.",
                    state.data,
                )
            status == "active" && socketPresent && !socketAccepting ->
                null
            else -> null
        }
    }

    private fun invalidResponseFingerprint(method: String, raw: String): String =
        method + ":" + raw.take(512).hashCode().toString()

    private fun recordInvalidResponseFingerprint(method: String, raw: String): Int {
        val fingerprint = invalidResponseFingerprint(method, raw)
        return if (fingerprint == lastInvalidResponseFingerprint) {
            val count = lastInvalidResponseFingerprintCount + 1
            lastInvalidResponseFingerprintCount = count
            count
        } else {
            lastInvalidResponseFingerprint = fingerprint
            lastInvalidResponseFingerprintCount = 1
            1
        }
    }

    private fun clearInvalidResponseFingerprint(method: String) {
        if (lastInvalidResponseFingerprint.startsWith("$method:")) {
            lastInvalidResponseFingerprint = ""
            lastInvalidResponseFingerprintCount = 0
        }
    }

    private fun stableInvalidResponseError(
        method: String,
        raw: String,
    ): DaemonClientResult.DaemonError =
        DaemonClientResult.DaemonError(
            DaemonClientErrorCodes.COMPATIBILITY,
            "APK и модуль/daemon стабильно несовместимы для $method: daemon возвращает JSON, " +
                "который APK не может разобрать даже после recovery. Нужна переустановка APK/модуля одной версии " +
                "или reboot после staged update.",
            JsonPrimitive(raw.take(1000)),
        )

    private fun nodeProbeCallTimeoutMs(selectedNodeCount: Int, perProbeTimeoutMs: Int, mode: String): Long {
        val workers = 3
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

    private suspend fun callBackendStatusMutation(
        method: String,
        params: JsonObject = emptyJsonObject(),
        timeoutMs: Long,
        allowModuleRepair: Boolean = false,
    ): DaemonClientResult<BackendStatusV2> {
        val result = call(
            method = method,
            params = params,
            timeoutMs = timeoutMs,
            allowModuleRepair = allowModuleRepair,
            allowBridge = false,
        ) {
            json.decodeFromJsonElement(BackendStatusV2.serializer(), it)
        }
        return if (result is DaemonClientResult.ParseError) {
            reconcileBackendStatusMutation(method, result)
        } else {
            result
        }
    }

    private suspend fun reconcileBackendStatusMutation(
        method: String,
        parseError: DaemonClientResult.ParseError,
    ): DaemonClientResult<BackendStatusV2> {
        executor.disableBridge("invalid mutation response for $method")
        val status = backendStatus(includeCompatibility = true)
        return when (status) {
            is DaemonClientResult.Ok -> {
                val expectedKind = GeneratedDaemonContract.OPERATION_TYPES[method]
                val activeOperation = status.data.activeOperation
                if (expectedKind != null && activeOperation?.kind == expectedKind) {
                    return status
                }
                val lastOperation = status.data.lastOperation
                if (expectedKind != null && lastOperation?.kind == expectedKind) {
                    return if (lastOperation.succeeded) {
                        status
                    } else {
                        DaemonClientResult.DaemonError(
                            DaemonClientErrorCodes.RUNTIME_BUSY,
                            lastOperation.errorMessage.ifBlank {
                                "Mutation response was invalid; backend.status reports the operation failed."
                            },
                            lastOperation.toJsonElement(),
                        )
                    }
                }
                DaemonClientResult.DaemonError(
                    DaemonClientErrorCodes.COMPATIBILITY,
                    "Mutation response for $method was invalid. backend.status did not confirm an active or completed " +
                        "operation for ${expectedKind ?: "this method"}; the original mutation was not retried.",
                    JsonPrimitive(parseError.raw.take(1000)),
                )
            }
            is DaemonClientResult.DaemonError -> status.asFailure()
            is DaemonClientResult.RootDenied -> status.asFailure()
            is DaemonClientResult.Timeout -> status.asFailure()
            is DaemonClientResult.DaemonNotFound -> status.asFailure()
            is DaemonClientResult.DaemonUnavailable -> status.asFailure()
            is DaemonClientResult.ParseError -> DaemonClientResult.DaemonError(
                DaemonClientErrorCodes.COMPATIBILITY,
                "Mutation response for $method was invalid, and backend.status was also invalid after recovery. " +
                    "The original mutation was not retried.",
                JsonPrimitive(status.raw.take(1000)),
            )
            is DaemonClientResult.Failure -> status.asFailure()
        }
    }

    private suspend fun callConfigMutation(
        method: String,
        params: JsonObject,
    ): DaemonClientResult<ConfigMutationInfo> {
        val result = executor.execute(
            method,
            params,
            timeoutMs = 60_000L,
            allowModuleRepair = true,
            allowBridge = false,
        )
        invalidateCompatibilityCacheOnTransportChange(result)
        val converted = result.toDaemonClientResultEnvelope { element ->
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
        if (converted is DaemonClientResult.ParseError) {
            return reconcileMutationInvalidResponse(method, converted)
        }
        return converted
    }

    private suspend fun reconcileMutationInvalidResponse(
        method: String,
        parseError: DaemonClientResult.ParseError,
    ): DaemonClientResult<ConfigMutationInfo> {
        executor.disableBridge("invalid mutation response for $method")
        val status = backendStatus(includeCompatibility = true)
        return when (status) {
            is DaemonClientResult.Ok -> reconcileMutationStatus(method, status.data, parseError)
            is DaemonClientResult.DaemonError -> status.asFailure()
            is DaemonClientResult.RootDenied -> status.asFailure()
            is DaemonClientResult.Timeout -> status.asFailure()
            is DaemonClientResult.DaemonNotFound -> status.asFailure()
            is DaemonClientResult.DaemonUnavailable -> status.asFailure()
            is DaemonClientResult.ParseError -> DaemonClientResult.DaemonError(
                DaemonClientErrorCodes.COMPATIBILITY,
                "Mutation response for $method was invalid, and backend.status was also invalid after recovery. " +
                    "The original mutation was not retried.",
                JsonPrimitive(status.raw.take(1000)),
            )
            is DaemonClientResult.Failure -> status.asFailure()
        }
    }

    private fun reconcileMutationStatus(
        method: String,
        status: BackendStatusV2,
        parseError: DaemonClientResult.ParseError,
    ): DaemonClientResult<ConfigMutationInfo> {
        val expectedKind = GeneratedDaemonContract.OPERATION_TYPES[method]
        val activeOperation = status.activeOperation
        if (expectedKind != null && activeOperation?.kind == expectedKind) {
            return DaemonClientResult.Ok(
                ConfigMutationInfo(
                    ok = true,
                    status = "accepted",
                    runtimeApplied = false,
                    message = "Mutation response was invalid; backend.status shows the operation is active.",
                    operation = activeOperation.toJsonElement(),
                    runtimeStatus = status,
                )
            )
        }

        val lastOperation = status.lastOperation
        if (expectedKind != null && lastOperation?.kind == expectedKind) {
            return if (lastOperation.succeeded) {
                DaemonClientResult.Ok(
                    ConfigMutationInfo(
                        ok = true,
                        status = "applied",
                        runtimeApplied = true,
                        message = "Mutation response was invalid; backend.status confirms the operation completed.",
                        operation = lastOperation.toJsonElement(),
                        runtimeStatus = status,
                    )
                )
            } else {
                DaemonClientResult.DaemonError(
                    DaemonClientErrorCodes.CONFIG_ERROR,
                    lastOperation.errorMessage.ifBlank {
                        "Mutation response was invalid; backend.status reports the operation failed."
                    },
                    lastOperation.toJsonElement(),
                )
            }
        }

        return DaemonClientResult.DaemonError(
            DaemonClientErrorCodes.CONFIG_ERROR,
            "Mutation response for $method was invalid. backend.status did not confirm an active or completed " +
                "operation for ${expectedKind ?: "this method"}; the original mutation was not retried.",
            JsonPrimitive(parseError.raw.take(1000)),
        )
    }

    private suspend fun reconcileMutatingParseError(
        method: String,
        parseError: DaemonClientResult.ParseError,
    ): DaemonClientResult<Nothing> {
        executor.disableBridge("invalid mutation response for $method")
        val status = backendStatus(includeCompatibility = true)
        return when (status) {
            is DaemonClientResult.Ok -> {
                val expectedKind = GeneratedDaemonContract.OPERATION_TYPES[method]
                val activeOperation = status.data.activeOperation
                if (expectedKind != null && activeOperation?.kind == expectedKind) {
                    return DaemonClientResult.DaemonError(
                        DaemonClientErrorCodes.RUNTIME_BUSY,
                        "Mutation response for $method was invalid; backend.status shows operation $expectedKind is active. " +
                            "The original mutation was not retried.",
                        activeOperation.toJsonElement(),
                    )
                }
                val lastOperation = status.data.lastOperation
                if (expectedKind != null && lastOperation?.kind == expectedKind) {
                    return if (lastOperation.succeeded) {
                        DaemonClientResult.DaemonError(
                            DaemonClientErrorCodes.COMPATIBILITY,
                            "Mutation response for $method was invalid; backend.status says operation $expectedKind completed. " +
                                "Refresh status before retrying; the original mutation was not retried.",
                            lastOperation.toJsonElement(),
                        )
                    } else {
                        DaemonClientResult.DaemonError(
                            DaemonClientErrorCodes.RUNTIME_BUSY,
                            lastOperation.errorMessage.ifBlank {
                                "Mutation response was invalid; backend.status reports the operation failed."
                            },
                            lastOperation.toJsonElement(),
                        )
                    }
                }
                DaemonClientResult.DaemonError(
                    DaemonClientErrorCodes.COMPATIBILITY,
                    "Mutation response for $method was invalid. backend.status did not confirm an active or completed " +
                        "operation for ${expectedKind ?: "this method"}; the original mutation was not retried.",
                    JsonPrimitive(parseError.raw.take(1000)),
                )
            }
            is DaemonClientResult.DaemonError -> status.asFailure()
            is DaemonClientResult.RootDenied -> status.asFailure()
            is DaemonClientResult.Timeout -> status.asFailure()
            is DaemonClientResult.DaemonNotFound -> status.asFailure()
            is DaemonClientResult.DaemonUnavailable -> status.asFailure()
            is DaemonClientResult.ParseError -> DaemonClientResult.DaemonError(
                DaemonClientErrorCodes.COMPATIBILITY,
                "Mutation response for $method was invalid, and backend.status was also invalid after recovery. " +
                    "The original mutation was not retried.",
                JsonPrimitive(status.raw.take(1000)),
            )
            is DaemonClientResult.Failure -> status.asFailure()
        }
    }

    private fun RuntimeOperationStatus.toJsonElement(): JsonElement =
        buildJsonObject {
            put("operationId", operationId)
            put("kind", kind)
            put("generation", generation)
            put("phase", phase.name)
            put("step", step)
            put("stepStatus", stepStatus)
            put("stepCode", stepCode)
            put("stepDetail", stepDetail)
            put("runtimeMs", runtimeMs)
            put("watchdogAfterMs", watchdogAfterMs)
            put("stuck", stuck)
        }

    private fun RuntimeOperationResult.toJsonElement(): JsonElement =
        buildJsonObject {
            put("operationId", operationId)
            put("kind", kind)
            put("generation", generation)
            put("phase", phase.name)
            put("succeeded", succeeded)
            put("errorCode", errorCode)
            put("errorMessage", errorMessage)
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
                allowBridge = false,
            ) {
                Unit
            }
            if (result !is DaemonClientResult.Ok) {
                if (result is DaemonClientResult.ParseError) {
                    return reconcileMutatingParseError("profile.importNodesBatch", result)
                }
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
            is DaemonClientResult.ParseError -> result
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
