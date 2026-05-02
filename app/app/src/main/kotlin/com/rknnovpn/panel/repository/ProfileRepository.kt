package com.rknnovpn.panel.repository

import android.util.Log
import com.rknnovpn.panel.`import`.LinkParser
import com.rknnovpn.panel.i18n.UserMessageFormatter
import com.rknnovpn.panel.ipc.DaemonClient
import com.rknnovpn.panel.ipc.DaemonClientResult
import com.rknnovpn.panel.ipc.ConfigMutationInfo
import com.rknnovpn.panel.ipc.PollingStatusSource
import com.rknnovpn.panel.ipc.RejectedSubscriptionNode
import com.rknnovpn.panel.ipc.isNoRuntimeProfileReason
import com.rknnovpn.panel.model.Node
import com.rknnovpn.panel.model.ProfileConfig
import com.rknnovpn.panel.model.SubscriptionSource
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.coroutines.withContext
import kotlinx.serialization.json.booleanOrNull
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import javax.inject.Inject
import javax.inject.Singleton

data class SubscriptionImportPreview(
    val previewId: String,
    val url: String,
    val source: SubscriptionSource,
    val nodes: List<Node>,
    val parseFailures: Int,
    val rejectedNodes: List<RejectedSubscriptionNode>,
    val added: Int,
    val updated: Int,
    val stale: Int,
) {
    val addedCount: Int get() = added
    val updatedCount: Int get() = updated
    val removedCount: Int get() = stale
    val rejectedCount: Int get() = rejectedNodes.size
}

enum class ProfileFreshness {
    EMPTY,
    FRESH,
    STALE,
    ERROR,
}

/**
 * Cache-backed profile CRUD via [DaemonClient].
 *
 * **Single source of truth**: the daemon-owned `profile.json`.
 * This repository keeps an in-memory cache that is refreshed:
 * - On first access after construction
 * - On explicit [refresh] (typically called from Activity.onResume)
 * - After every mutating operation (add/remove/update node, change routing, etc.)
 *
 * All writes go through the daemon profile RPCs and then re-read to keep
 * the cache consistent. If a write fails the cache is NOT updated, so the UI
 * always reflects the actual daemon state.
 */
@Singleton
class ProfileRepository @Inject constructor(
    private val client: DaemonClient,
    private val poller: PollingStatusSource,
    private val messages: UserMessageFormatter,
) {
    companion object {
        private const val TAG = "ProfileRepository"
    }

    private val mutex = Mutex()

    private val _profile = MutableStateFlow<ProfileConfig?>(null)
    /** Current cached profile, or null if not yet loaded. Kept on transient load failures. */
    val profile: StateFlow<ProfileConfig?> = _profile.asStateFlow()

    private val _freshness = MutableStateFlow(ProfileFreshness.EMPTY)
    /** Freshness of [profile], so UI can keep stale data visible during transient IPC failures. */
    val freshness: StateFlow<ProfileFreshness> = _freshness.asStateFlow()

    private val _loading = MutableStateFlow(false)
    /** True while a network/IPC operation is in flight. */
    val loading: StateFlow<Boolean> = _loading.asStateFlow()

    private val _error = MutableStateFlow<String?>(null)
    /** Human-readable error from the last failed operation, or null. */
    val error: StateFlow<String?> = _error.asStateFlow()

    private val _notice = MutableStateFlow<String?>(null)
    /** Human-readable status from the last successful partial or informational operation. */
    val notice: StateFlow<String?> = _notice.asStateFlow()

    private fun emptyFirstRunProfile(): ProfileConfig =
        ProfileConfig(id = "default", name = "Default")

    // ---- Read ----

    /**
     * Refresh the cache from the daemon. Called from Activity.onResume
     * and after every mutation.
     *
     * @return The fresh profile or null on failure.
     */
    suspend fun refresh(): ProfileConfig? = mutex.withLock {
        _loading.value = true
        _error.value = null
        _notice.value = null
        try {
            when (val result = client.profileGet()) {
                is DaemonClientResult.Ok -> {
                    publishFreshProfile(result.data)
                    result.data
                }
                is DaemonClientResult.DaemonUnavailable -> {
                    if (result.reason.isNoRuntimeProfileReason()) {
                        val emptyProfile = emptyFirstRunProfile()
                        publishFreshProfile(emptyProfile)
                        _error.value = null
                        emptyProfile
                    } else {
                        val msg = describeFailure(result)
                        Log.w(TAG, "refresh failed: $msg")
                        markProfileLoadFailure(msg)
                        null
                    }
                }
                else -> {
                    val msg = describeFailure(result)
                    Log.w(TAG, "refresh failed: $msg")
                    markProfileLoadFailure(msg)
                    null
                }
            }
        } finally {
            _loading.value = false
        }
    }

    /**
     * Returns the cached profile, loading it from the daemon if the cache is empty.
     */
    suspend fun getOrLoad(): ProfileConfig? {
        _profile.value?.let { return it }
        return refresh()
    }

    // ---- Node CRUD ----

    /** Add a node to the profile. */
    suspend fun addNode(node: Node): Boolean = mutateDelta("addNode") {
        client.profileNodeUpsert(node)
    }

    /** Remove a node by ID. */
    suspend fun removeNode(nodeId: String): Boolean = mutateDelta("removeNode") {
        client.profileNodeRemove(nodeId)
    }

    /** Replace a node, matching by ID. */
    suspend fun updateNode(updated: Node): Boolean = mutateDelta("updateNode") {
        client.profileNodeUpsert(updated)
    }

    /** Set the active node for this profile. */
    suspend fun setActiveNode(nodeId: String): Boolean = mutex.withLock {
        _loading.value = true
        _error.value = null
        _notice.value = null
        try {
            val current = _profile.value ?: refreshUnlockedOrNull() ?: run {
                if (_error.value.isNullOrBlank()) {
                    _error.value = messages.get(com.rknnovpn.panel.R.string.error_no_profile_loaded)
                }
                return@withLock false
            }
            if (current.nodes.none { it.id == nodeId && !it.stale }) {
                _error.value = messages.get(com.rknnovpn.panel.R.string.node_not_found)
                return@withLock false
            }
            if (current.activeNodeId == nodeId) {
                return@withLock true
            }
            when (val result = client.profileSetActiveNode(nodeId)) {
                is DaemonClientResult.Ok -> {
                    applyMutationSuccess("setActiveNode", result.data)
                }
                else -> {
                    val msg = describeFailure(result)
                    Log.w(TAG, "setActiveNode profile update failed: $msg")
                    if (result.configWasSaved()) {
                        return@withLock refreshAfterSavedFailure("setActiveNode", msg)
                    }
                    _error.value = msg
                    false
                }
            }
        } finally {
            _loading.value = false
        }
    }

    /** Let the daemon selector use automatic node selection. */
    suspend fun clearActiveNode(): Boolean = mutex.withLock {
        _loading.value = true
        _error.value = null
        _notice.value = null
        try {
            val current = _profile.value ?: refreshUnlockedOrNull() ?: run {
                if (_error.value.isNullOrBlank()) {
                    _error.value = messages.get(com.rknnovpn.panel.R.string.error_no_profile_loaded)
                }
                return@withLock false
            }
            if (current.activeNodeId.isNullOrBlank()) {
                return@withLock true
            }
            when (val result = client.profileClearActiveNode()) {
                is DaemonClientResult.Ok -> {
                    applyMutationSuccess("clearActiveNode", result.data)
                }
                else -> {
                    val msg = describeFailure(result)
                    Log.w(TAG, "clearActiveNode profile update failed: $msg")
                    if (result.configWasSaved()) {
                        return@withLock refreshAfterSavedFailure("clearActiveNode", msg)
                    }
                    _error.value = msg
                    false
                }
            }
        } finally {
            _loading.value = false
        }
    }

    /** Import nodes from direct import content or refresh a subscription URL. */
    suspend fun importNodes(input: String): List<Node> = withContext(Dispatchers.IO) {
        mutex.withLock {
            _loading.value = true
            _error.value = null
            _notice.value = null
            try {
                if (LinkParser.isSubscriptionUrl(input.trim())) {
                    importSubscriptionUnlocked(input.trim())
                } else {
                    importDirectLinksUnlocked(input)
                }
            } finally {
                _loading.value = false
            }
        }
    }

    /** Import already parsed manual nodes, including nodes created from config JSON. */
    suspend fun importParsedNodes(nodes: List<Node>): List<Node> = withContext(Dispatchers.IO) {
        mutex.withLock {
            _loading.value = true
            _error.value = null
            _notice.value = null
            try {
                importParsedNodesUnlocked(nodes)
            } finally {
                _loading.value = false
            }
        }
    }

    suspend fun previewSubscription(url: String): SubscriptionImportPreview? = withContext(Dispatchers.IO) {
        mutex.withLock {
            _loading.value = true
            _error.value = null
            _notice.value = null
            try {
                val current = _profile.value ?: refreshUnlockedOrNull(allowModuleRepair = true) ?: run {
                    if (_error.value.isNullOrBlank()) {
                        _error.value = messages.get(com.rknnovpn.panel.R.string.error_no_profile_loaded)
                    }
                    return@withLock null
                }
                buildSubscriptionPreviewUnlocked(url.trim())
            } finally {
                _loading.value = false
            }
        }
    }

    suspend fun applySubscriptionPreview(preview: SubscriptionImportPreview): List<Node> =
        withContext(Dispatchers.IO) {
            mutex.withLock {
                _loading.value = true
                _error.value = null
                _notice.value = null
                try {
                    val current = _profile.value ?: refreshUnlockedOrNull(allowModuleRepair = true) ?: run {
                        if (_error.value.isNullOrBlank()) {
                            _error.value = messages.get(com.rknnovpn.panel.R.string.error_no_profile_loaded)
                        }
                        return@withLock emptyList()
                    }
                    applySubscriptionPreviewUnlocked(preview)
                } finally {
                    _loading.value = false
                }
            }
        }

    // ---- Profile-level mutations ----

    /** Replace the full profile config. */
    suspend fun setProfile(config: ProfileConfig): Boolean = mutex.withLock {
        _loading.value = true
        _error.value = null
        _notice.value = null
        try {
            when (val result = client.profileApply(config, reload = true)) {
                is DaemonClientResult.Ok -> {
                    applyMutationSuccess("setProfile", result.data)
                }
                else -> {
                    val msg = describeFailure(result)
                    Log.w(TAG, "setProfile failed: $msg")
                    if (result.configWasSaved()) {
                        return@withLock refreshAfterSavedFailure("setProfile", msg)
                    }
                    _error.value = msg
                    false
                }
            }
        } finally {
            _loading.value = false
        }
    }

    /** Update routing, DNS, reserved profile network fields, or inbound settings. */
    suspend fun updateConfig(transform: (ProfileConfig) -> ProfileConfig): Boolean =
        mutate("updateConfig", transform)

    // ---- Internal helpers ----

    /**
     * Generic read-modify-write: read cache -> apply transform -> send to daemon -> re-read.
     */
    private suspend fun mutate(
        tag: String,
        transform: (ProfileConfig) -> ProfileConfig
    ): Boolean = mutex.withLock {
        _loading.value = true
        _error.value = null
        _notice.value = null
        try {
            val current = _profile.value ?: refreshUnlockedOrNull() ?: run {
                if (_error.value.isNullOrBlank()) {
                    _error.value = messages.get(com.rknnovpn.panel.R.string.error_no_profile_loaded)
                }
                return@withLock false
            }
            val updated = transform(current)
            when (val result = applyDeltaOrFull(current, updated)) {
                is DaemonClientResult.Ok -> {
                    applyMutationSuccess(tag, result.data)
                }
                else -> {
                    val msg = describeFailure(result)
                    Log.w(TAG, "$tag failed: $msg")
                    if (result.configWasSaved()) {
                        return refreshAfterSavedFailure(tag, msg)
                    }
                    _error.value = msg
                    false
                }
            }
        } finally {
            _loading.value = false
        }
    }

    private suspend fun mutateDelta(
        tag: String,
        call: suspend (ProfileConfig) -> DaemonClientResult<ConfigMutationInfo>,
    ): Boolean = mutex.withLock {
        _loading.value = true
        _error.value = null
        _notice.value = null
        try {
            val current = _profile.value ?: refreshUnlockedOrNull() ?: run {
                if (_error.value.isNullOrBlank()) {
                    _error.value = messages.get(com.rknnovpn.panel.R.string.error_no_profile_loaded)
                }
                return@withLock false
            }
            when (val result = call(current)) {
                is DaemonClientResult.Ok -> {
                    applyMutationSuccess(tag, result.data)
                }
                else -> {
                    val msg = describeFailure(result)
                    Log.w(TAG, "$tag profile update failed: $msg")
                    if (result.configWasSaved()) {
                        return refreshAfterSavedFailure(tag, msg)
                    }
                    _error.value = msg
                    false
                }
            }
        } finally {
            _loading.value = false
        }
    }

    private suspend fun applyDeltaOrFull(
        current: ProfileConfig,
        updated: ProfileConfig,
    ): DaemonClientResult<ConfigMutationInfo> {
        val onlyRoutingChanged = updated.copy(routing = current.routing) == current
        if (onlyRoutingChanged && updated.routing != current.routing) {
            return client.profileRoutingPatch(updated.routing)
        }
        val onlyDnsChanged = updated.copy(dns = current.dns) == current
        if (onlyDnsChanged && updated.dns != current.dns) {
            return client.profileDNSPatch(updated.dns)
        }
        val onlyInboundsChanged = updated.copy(inbounds = current.inbounds) == current
        if (onlyInboundsChanged && updated.inbounds != current.inbounds) {
            return client.profileInboundPatch(updated.inbounds)
        }
        return client.profileApply(updated)
    }

    /** Refresh without acquiring the mutex (caller already holds it). */
    private suspend fun refreshUnlocked() {
        when (val result = client.profileGet()) {
            is DaemonClientResult.Ok -> {
                publishFreshProfile(result.data)
            }
            is DaemonClientResult.DaemonUnavailable -> {
                if (result.reason.isNoRuntimeProfileReason()) {
                    publishFreshProfile(emptyFirstRunProfile())
                    _error.value = null
                } else {
                    val msg = describeFailure(result)
                    Log.w(TAG, "refreshUnlocked failed: $msg")
                    markProfileLoadFailure(msg)
                }
            }
            else -> {
                val msg = describeFailure(result)
                Log.w(TAG, "refreshUnlocked failed: $msg")
                markProfileLoadFailure(msg)
            }
        }
    }

    private suspend fun refreshUnlockedWithStatus(tag: String): Boolean {
        return when (val result = client.profileGet()) {
            is DaemonClientResult.Ok -> {
                publishFreshProfile(result.data)
                true
            }
            is DaemonClientResult.DaemonUnavailable -> {
                if (result.reason.isNoRuntimeProfileReason()) {
                    publishFreshProfile(emptyFirstRunProfile())
                    _error.value = null
                    true
                } else {
                    val msg = describeFailure(result)
                    markProfileLoadFailure(msg)
                    Log.w(TAG, "$tag post-write refresh failed: $msg")
                    false
                }
            }
            else -> {
                val msg = describeFailure(result)
                markProfileLoadFailure(msg)
                Log.w(TAG, "$tag post-write refresh failed: $msg")
                false
            }
        }
    }

    private suspend fun refreshAfterSavedFailure(tag: String, message: String): Boolean {
        _error.value = message
        refreshUnlockedWithStatus("$tag saved-failure")
        return false
    }

    private suspend fun refreshUnlockedOrNull(allowModuleRepair: Boolean = false): ProfileConfig? {
        return when (val result = client.profileGet(allowModuleRepair = allowModuleRepair)) {
            is DaemonClientResult.Ok -> {
                publishFreshProfile(result.data)
                result.data
            }
            is DaemonClientResult.DaemonUnavailable -> {
                if (result.reason.isNoRuntimeProfileReason()) {
                    val emptyProfile = emptyFirstRunProfile()
                    publishFreshProfile(emptyProfile)
                    _error.value = null
                    emptyProfile
                } else {
                    val msg = describeFailure(result)
                    Log.w(TAG, "refreshUnlockedOrNull failed: $msg")
                    markProfileLoadFailure(msg)
                    null
                }
            }
            else -> {
                val msg = describeFailure(result)
                Log.w(TAG, "refreshUnlockedOrNull failed: $msg")
                markProfileLoadFailure(msg)
                null
            }
        }
    }

    private suspend fun importDirectLinksUnlocked(
        rawInput: String
    ): List<Node> {
        val parsedNodes = LinkParser.detectNodes(rawInput)
        if (parsedNodes.isEmpty()) {
            _error.value = messages.get(com.rknnovpn.panel.R.string.node_no_valid_proxy_links_detected)
            return emptyList()
        }

        return importParsedNodesUnlocked(parsedNodes)
    }

    private suspend fun importParsedNodesUnlocked(
        parsedNodes: List<Node>,
    ): List<Node> {
        if (parsedNodes.isEmpty()) {
            _error.value = messages.get(com.rknnovpn.panel.R.string.node_no_supported_proxy_links_parsed)
            return emptyList()
        }
        return when (val result = client.profileImportNodes(parsedNodes, reload = false)) {
            is DaemonClientResult.Ok -> {
                applyMutationSuccess("importNodes", result.data)
                parsedNodes
            }
            else -> {
                val msg = describeFailure(result)
                Log.w(TAG, "importNodes failed: $msg")
                if (result.configWasSaved()) {
                    refreshAfterSavedFailure("importNodes", msg)
                    return emptyList()
                }
                _error.value = msg
                emptyList()
            }
        }
    }

    private suspend fun importSubscriptionUnlocked(
        url: String
    ): List<Node> {
        val preview = buildSubscriptionPreviewUnlocked(url) ?: return emptyList()
        return applySubscriptionPreviewUnlocked(preview)
    }

    private suspend fun buildSubscriptionPreviewUnlocked(
        url: String,
    ): SubscriptionImportPreview? {
        val preview = when (val result = client.subscriptionPreview(url)) {
            is DaemonClientResult.Ok -> result.data
            else -> {
                val msg = messages.formatDaemonFailure(result)
                Log.w(TAG, "daemon subscription preview failed: $msg")
                _error.value = msg
                return null
            }
        }
        if (preview.nodes.isEmpty() && (preview.rejected > 0 || preview.rejectedNodes.isNotEmpty())) {
            _error.value = messages.formatRejectedSubscriptionOnly(preview.rejectedNodes, preview.rejected)
            return null
        }
        if (preview.nodes.isEmpty() && preview.parseFailures > 0) {
            _error.value = messages.get(com.rknnovpn.panel.R.string.subscription_no_supported_links)
            return null
        }
        if (preview.nodes.isEmpty() && preview.stale == 0) {
            _error.value = messages.get(com.rknnovpn.panel.R.string.subscription_empty)
            return null
        }

        return SubscriptionImportPreview(
            previewId = preview.previewId,
            url = preview.source.url.ifBlank { url },
            source = preview.source,
            nodes = preview.nodes,
            parseFailures = preview.parseFailures,
            rejectedNodes = preview.rejectedNodes,
            added = preview.added,
            updated = preview.updated,
            stale = preview.stale,
        )
    }

    private suspend fun applySubscriptionPreviewUnlocked(
        preview: SubscriptionImportPreview,
    ): List<Node> {
        if (preview.previewId.isBlank()) {
            _error.value = messages.get(com.rknnovpn.panel.R.string.subscription_no_supported_links)
            return emptyList()
        }
        return when (val result = client.subscriptionCommitPreview(preview.previewId)) {
            is DaemonClientResult.Ok -> {
                applyMutationSuccess("subscriptionRefresh", result.data)
                _notice.value = messages.formatSubscriptionRefresh(
                    importedNodes = result.data.imported ?: preview.nodes.size,
                    parseFailures = result.data.parseFailures ?: preview.parseFailures,
                    rejectedNodes = result.data.rejectedNodes.ifEmpty { preview.rejectedNodes },
                )
                preview.nodes
            }
            else -> {
                val msg = describeFailure(result)
                Log.w(TAG, "subscriptionRefresh failed: $msg")
                if (result.configWasSaved()) {
                    refreshAfterSavedFailure("subscriptionRefresh", msg)
                    return emptyList()
                }
                _error.value = msg
                emptyList()
            }
        }
    }

    private fun <T> describeFailure(result: DaemonClientResult<T>): String =
        messages.formatDaemonFailure(result)

    private suspend fun applyMutationSuccess(tag: String, info: ConfigMutationInfo): Boolean {
        publishRuntimeStatus(info)
        info.profile?.let { profile ->
            publishFreshProfile(profile)
            return true
        }
        return refreshUnlockedWithStatus(tag)
    }

    private fun publishFreshProfile(profile: ProfileConfig) {
        _profile.value = profile
        _freshness.value = if (profile.nodes.isEmpty()) {
            ProfileFreshness.EMPTY
        } else {
            ProfileFreshness.FRESH
        }
    }

    private fun markProfileLoadFailure(message: String) {
        _error.value = message
        _freshness.value = if (_profile.value == null) {
            ProfileFreshness.ERROR
        } else {
            ProfileFreshness.STALE
        }
    }

    private fun publishRuntimeStatus(info: ConfigMutationInfo) {
        info.runtimeStatus?.let(poller::publishBackendStatus)
        messages.formatConfigMutationNotice(info)?.let { _notice.value = it }
    }

    private fun <T> DaemonClientResult<T>.configWasSaved(): Boolean {
        val error = this as? DaemonClientResult.DaemonError ?: return false
        val details = runCatching {
            error.details?.jsonObject
                ?: error.envelope?.jsonObject
                    ?.get("error")?.jsonObject
                    ?.get("details")?.jsonObject
        }.getOrNull() ?: return false
        return details["config_saved"]?.jsonPrimitive?.booleanOrNull == true ||
            details["configSaved"]?.jsonPrimitive?.booleanOrNull == true
    }
}
