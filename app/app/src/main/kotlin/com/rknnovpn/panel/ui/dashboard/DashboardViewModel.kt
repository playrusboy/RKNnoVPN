package com.rknnovpn.panel.ui.dashboard

import android.util.Log
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.rknnovpn.panel.i18n.UserMessageFormatter
import com.rknnovpn.panel.model.BackendPhase
import com.rknnovpn.panel.model.ConnectionState
import com.rknnovpn.panel.model.DaemonConnectionState
import com.rknnovpn.panel.model.DaemonStatus
import com.rknnovpn.panel.model.Node
import com.rknnovpn.panel.model.ProfileConfig
import com.rknnovpn.panel.model.TrafficStats
import com.rknnovpn.panel.repository.CommandOutcome
import com.rknnovpn.panel.repository.ProfileRepository
import com.rknnovpn.panel.repository.StatusRepository
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import javax.inject.Inject

/**
 * UI state exposed to the Dashboard screen.
 */
data class DashboardUiState(
    val connectionState: ConnectionState = ConnectionState.DISCONNECTED,
    val runtimePhase: BackendPhase = BackendPhase.STOPPED,
    val activeNodeName: String? = null,
    val activeNodeProtocol: String? = null,
    val hasAvailableNode: Boolean = false,
    val traffic: TrafficStats = TrafficStats(),
    /** Ring buffer of normalized RX rate samples for the sparkline (0..1). */
    val trafficHistory: List<Float> = emptyList(),
    val egressIp: String? = null,
    val countryFlag: String? = null,
    val latencyMs: Int? = null,
    val dnsChecked: Boolean = false,
    val dnsOperational: Boolean = false,
    val operationalDegraded: Boolean = false,
    val operationalIssueMessage: String? = null,
    val uptimeSeconds: Long = 0L,
    val isRefreshing: Boolean = false,
    val runtimeActionActive: Boolean = false,
    /** Error message from the last daemon operation, or null. */
    val errorMessage: String? = null,
    /** Informational message from the last daemon operation, or null. */
    val statusMessage: String? = null,
    /** True when the daemon process is not reachable at all. */
    val daemonUnreachable: Boolean = false,
)

private const val TRAFFIC_HISTORY_SIZE = 120
private const val TAG = "DashboardViewModel"

/** Peak rate used to normalize sparkline samples. */
private const val PEAK_RATE_FOR_NORMALIZATION = 10_000_000f // 10 MB/s

@HiltViewModel
class DashboardViewModel @Inject constructor(
    private val statusRepository: StatusRepository,
    private val profileRepository: ProfileRepository,
    private val messages: UserMessageFormatter,
) : ViewModel() {

    private val _uiState = MutableStateFlow(DashboardUiState())
    val uiState: StateFlow<DashboardUiState> = _uiState.asStateFlow()

    /** Mutable ring buffer backing the sparkline. */
    private val _trafficRing = ArrayDeque<Float>(TRAFFIC_HISTORY_SIZE)
    private var latestStatus: DaemonStatus? = null

    init {
        observeDaemonStatus()
        observeDaemonConnectionState()
        observePollErrors()
        observeProfile()
        viewModelScope.launch {
            profileRepository.getOrLoad()
        }
    }

    // ---- Public actions ----

    fun startStatusPolling() {
        statusRepository.startPolling()
    }

    fun stopStatusPolling() {
        statusRepository.stopPolling()
    }

    fun toggleConnection() {
        if (_uiState.value.runtimeActionActive) return
        val current = _uiState.value.connectionState
        when (current) {
            ConnectionState.DISCONNECTED,
            ConnectionState.ERROR,
            ConnectionState.UNKNOWN -> connect()
            ConnectionState.CONNECTED -> disconnect()
            ConnectionState.CONNECTING -> { /* ignore while connecting */ }
        }
    }

    fun refresh() {
        viewModelScope.launch {
            _uiState.update { it.copy(isRefreshing = true, errorMessage = null, statusMessage = null) }
            statusRepository.pollNow()
            // Give the poller a moment to complete, then clear refresh flag.
            // The actual data update comes via the status flow observer.
            delay(500)
            _uiState.update { it.copy(isRefreshing = false) }
        }
    }

    fun restartBackend() {
        if (_uiState.value.runtimeActionActive) return
        if (!_uiState.value.hasAvailableNode) {
            _uiState.update {
                it.copy(
                    connectionState = ConnectionState.DISCONNECTED,
                    errorMessage = messages.get(com.rknnovpn.panel.R.string.no_nodes),
                    statusMessage = null,
                )
            }
            return
        }
        _uiState.update {
            it.copy(
                runtimeActionActive = true,
                errorMessage = null,
                statusMessage = messages.get(com.rknnovpn.panel.R.string.daemon_status_restarting),
            )
        }
        viewModelScope.launch {
            when (val outcome = statusRepository.reload()) {
                is CommandOutcome.Success -> {
                    Log.d(TAG, "Backend restart succeeded from dashboard")
                    _uiState.update {
                        it.copy(
                            statusMessage = messages.get(com.rknnovpn.panel.R.string.daemon_status_restarted),
                            errorMessage = null,
                        )
                    }
                }
                is CommandOutcome.Failed -> {
                    Log.w(TAG, "Backend restart failed from dashboard: ${outcome.message}")
                    _uiState.update {
                        it.copy(
                            statusMessage = null,
                            errorMessage = outcome.message,
                            runtimeActionActive = false,
                        )
                    }
                }
            }
        }
    }

    // ---- Internal ----

    private fun connect() {
        viewModelScope.launch {
            val profile = profileRepository.getOrLoad()
            if (profile == null) {
                _uiState.update {
                    it.copy(
                        connectionState = ConnectionState.ERROR,
                        runtimeActionActive = false,
                        errorMessage = profileRepository.error.value
                            ?: messages.get(com.rknnovpn.panel.R.string.error_no_profile_loaded),
                        statusMessage = null,
                    )
                }
                return@launch
            }
            if (availableNodes(profile).isEmpty()) {
                _uiState.update {
                    it.copy(
                        connectionState = ConnectionState.DISCONNECTED,
                        runtimeActionActive = false,
                        errorMessage = messages.get(com.rknnovpn.panel.R.string.no_nodes),
                        statusMessage = null,
                    )
                }
                return@launch
            }
            _uiState.update {
                it.copy(
                    connectionState = ConnectionState.CONNECTING,
                    runtimeActionActive = true,
                    errorMessage = null,
                    statusMessage = null,
                )
            }
            when (val outcome = statusRepository.start()) {
                is CommandOutcome.Success -> {
                    Log.d(TAG, "Start command succeeded; waiting for status poll")
                    // Status will be updated via the observer when the poll fires
                }
                is CommandOutcome.Failed -> {
                    Log.w(TAG, "Start failed: ${outcome.message}")
                    _uiState.update {
                        it.copy(
                            connectionState = ConnectionState.ERROR,
                            runtimeActionActive = false,
                            errorMessage = outcome.message,
                            statusMessage = null,
                        )
                    }
                }
            }
        }
    }

    private fun disconnect() {
        viewModelScope.launch {
            _uiState.update {
                it.copy(
                    connectionState = ConnectionState.CONNECTING,
                    runtimePhase = BackendPhase.STOPPING,
                    runtimeActionActive = true,
                    errorMessage = null,
                    statusMessage = null,
                )
            }
            when (val outcome = statusRepository.stop()) {
                is CommandOutcome.Success -> {
                    Log.d(TAG, "Stop command succeeded; waiting for status poll")
                    // Clear traffic history immediately for snappy feel
                    _trafficRing.clear()
                    _uiState.update {
                        it.copy(
                            trafficHistory = emptyList(),
                        )
                    }
                }
                is CommandOutcome.Failed -> {
                    Log.w(TAG, "Stop failed: ${outcome.message}")
                    _uiState.update {
                        it.copy(
                            connectionState = ConnectionState.ERROR,
                            runtimeActionActive = false,
                            errorMessage = outcome.message,
                            statusMessage = null,
                        )
                    }
                }
            }
        }
    }

    /**
     * Observe daemon status snapshots from [StatusRepository] and project
     * them into [DashboardUiState].
     */
    private fun observeDaemonStatus() {
        viewModelScope.launch {
            statusRepository.status.collect { status ->
                if (status != null) {
                    applyDaemonStatus(status)
                }
            }
        }
    }

    /**
     * Observe the Panel-to-daemon connectivity state so we can show
     * "daemon unreachable" in the UI.
     */
    private fun observeDaemonConnectionState() {
        viewModelScope.launch {
            statusRepository.connectionState.collect { connState ->
                val unreachable = connState == DaemonConnectionState.UNREACHABLE
                _uiState.update { it.copy(daemonUnreachable = unreachable) }

                // If daemon becomes unreachable while we are waiting for status
                // or already connected, stop showing a stale spinner/state.
                if (unreachable &&
                    (_uiState.value.connectionState == ConnectionState.CONNECTED ||
                        _uiState.value.connectionState == ConnectionState.CONNECTING)
                ) {
                    _uiState.update { it.copy(connectionState = ConnectionState.UNKNOWN) }
                }
            }
        }
    }

    private fun observePollErrors() {
        viewModelScope.launch {
            statusRepository.lastPollError.collect { pollError ->
                if (pollError != null) {
                    _uiState.update { it.copy(errorMessage = pollError) }
                }
            }
        }
    }

    private fun observeProfile() {
        viewModelScope.launch {
            profileRepository.profile.collect { profile ->
                val status = latestStatus ?: return@collect
                _uiState.update {
                    it.copy(
                        activeNodeName = formatActiveNodeName(status, profile),
                        activeNodeProtocol = formatActiveNodeSubtitle(status, profile),
                        hasAvailableNode = availableNodes(profile).isNotEmpty(),
                    )
                }
            }
        }
    }

    fun applyDaemonStatus(status: DaemonStatus) {
        // Push a normalized RX rate sample into the sparkline ring buffer
        val rxRate = status.traffic.rxRate
        if (status.state == ConnectionState.CONNECTED && rxRate > 0) {
            val normalized = (rxRate / PEAK_RATE_FOR_NORMALIZATION).coerceIn(0f, 1f)
            if (_trafficRing.size >= TRAFFIC_HISTORY_SIZE) {
                _trafficRing.removeFirst()
            }
            _trafficRing.addLast(normalized)
        } else if (status.state == ConnectionState.DISCONNECTED) {
            _trafficRing.clear()
        }
        latestStatus = status

        _uiState.update {
            val showRuntimeHealth = status.shouldShowRuntimeHealth()
            val operationalDegraded = showRuntimeHealth &&
                status.health.healthy &&
                !status.health.operationalHealthy &&
                status.health.checkedAt > 0L
            val healthIssueMessage = messages.formatHealthIssue(
                status.health.lastCode,
                status.health.lastError,
                status.health.lastUserMessage,
                status.health.stageReport,
            )
            val lastOperationFailure = status.lastOperation
                ?.takeIf { operation -> !operation.succeeded && status.activeOperation == null }
                ?.let { operation ->
                    messages.formatOperationFailure(
                        operation.kind.operationNameRes(),
                        operation.errorMessage.ifBlank { operation.errorCode },
                    )
                }
            val activeOperationStuckMessage = status.activeOperation
                ?.takeIf { operation -> operation.stuck }
                ?.let { operation ->
                    formatStuckOperation(operation.stepDetail, operation.step)
                }
            it.copy(
                connectionState = status.state,
                runtimePhase = status.health.phase,
                activeNodeName = formatActiveNodeName(status, profileRepository.profile.value),
                activeNodeProtocol = formatActiveNodeSubtitle(status, profileRepository.profile.value),
                hasAvailableNode = availableNodes(profileRepository.profile.value).isNotEmpty(),
                egressIp = status.egressIp,
                countryFlag = status.countryFlag,
                latencyMs = status.latencyMs,
                traffic = status.traffic,
                trafficHistory = _trafficRing.toList(),
                dnsChecked = showRuntimeHealth && status.health.checkedAt > 0L,
                dnsOperational = showRuntimeHealth && status.health.dnsOperational,
                operationalDegraded = operationalDegraded,
                operationalIssueMessage = if (operationalDegraded) {
                    healthIssueMessage
                } else {
                    null
                },
                uptimeSeconds = status.uptime,
                runtimeActionActive = status.activeOperation != null,
                // Clear error when we get a successful status with a healthy state
                errorMessage = when {
                    activeOperationStuckMessage != null -> activeOperationStuckMessage
                    operationalDegraded -> null
                    lastOperationFailure != null -> lastOperationFailure
                    status.state == ConnectionState.ERROR -> healthIssueMessage
                    else -> null
                },
                statusMessage = if (
                    activeOperationStuckMessage == null &&
                    !operationalDegraded &&
                    lastOperationFailure == null &&
                    status.state != ConnectionState.ERROR &&
                    status.activeOperation == null
                ) {
                    it.statusMessage
                } else {
                    null
                },
            )
        }
    }

    private fun formatStuckOperation(stepDetail: String, step: String): String {
        val currentStep = stepDetail.ifBlank { step }.trim()
        return if (currentStep.isBlank()) {
            messages.get(com.rknnovpn.panel.R.string.daemon_status_operation_stuck)
        } else {
            messages.get(com.rknnovpn.panel.R.string.daemon_status_operation_stuck_with_step, currentStep)
        }
    }

    private fun formatActiveNodeName(status: DaemonStatus, profile: ProfileConfig?): String? {
        val mode = effectiveActiveNodeMode(status, profile)
        return when (mode) {
            "auto_selector" -> messages.get(com.rknnovpn.panel.R.string.active_node_mode_auto)
            "manual_missing" -> null
            "manual" -> configuredActiveNode(profile)?.name
            else -> status.activeNodeName?.trim()?.ifBlank { null }
                ?: effectiveActiveNode(status, profile)?.name
        }
    }

    private fun formatActiveNodeSubtitle(status: DaemonStatus, profile: ProfileConfig?): String? {
        val mode = effectiveActiveNodeMode(status, profile)
        return when (mode) {
            "auto_selector" -> null
            "manual" -> configuredActiveNode(profile)?.protocol?.name?.let {
                messages.get(com.rknnovpn.panel.R.string.active_node_mode_manual, it)
            }
            "manual_missing" -> messages.get(com.rknnovpn.panel.R.string.active_node_mode_missing)
            "single_node" -> status.activeNodeProtocol ?: effectiveActiveNode(status, profile)?.protocol?.name
            else -> status.activeNodeProtocol ?: effectiveActiveNode(status, profile)?.protocol?.name
        }
    }

    private fun effectiveActiveNodeMode(status: DaemonStatus, profile: ProfileConfig?): String? {
        status.activeNodeMode?.trim()?.ifBlank { null }?.let { return it }
        val nodes = availableNodes(profile)
        if (nodes.isEmpty()) return null
        val configuredId = profile?.activeNodeId?.trim()?.ifBlank { null }
        return when {
            configuredId != null && nodes.any { it.id == configuredId } -> "manual"
            configuredId != null -> "manual_missing"
            nodes.size == 1 -> "single_node"
            else -> "auto_selector"
        }
    }

    private fun effectiveActiveNode(status: DaemonStatus, profile: ProfileConfig?): Node? {
        val nodes = availableNodes(profile)
        if (nodes.isEmpty()) return null
        val activeId = if (effectiveActiveNodeMode(status, profile) == "manual") {
            profile?.activeNodeId?.trim()?.ifBlank { null }
        } else {
            status.activeNodeId?.trim()?.ifBlank { null }
                ?: profile?.activeNodeId?.trim()?.ifBlank { null }
        }
        return activeId?.let { id -> nodes.firstOrNull { it.id == id } } ?: nodes.firstOrNull()
    }

    private fun configuredActiveNode(profile: ProfileConfig?): Node? {
        val activeId = profile?.activeNodeId?.trim()?.ifBlank { null } ?: return null
        return availableNodes(profile).firstOrNull { it.id == activeId }
    }

    private fun availableNodes(profile: ProfileConfig?): List<Node> =
        profile?.nodes?.filterNot { it.stale }.orEmpty()

    private fun DaemonStatus.shouldShowRuntimeHealth(): Boolean {
        val runtimeSettled = health.phase !in TRANSIENT_OR_STOPPED_PHASES
        return runtimeSettled &&
            (state == ConnectionState.CONNECTED || state == ConnectionState.ERROR)
    }

    override fun onCleared() {
        super.onCleared()
        statusRepository.stopPolling()
    }
}

private fun String.operationNameRes(): Int = when (this) {
    "start" -> com.rknnovpn.panel.R.string.operation_start
    "stop" -> com.rknnovpn.panel.R.string.operation_stop
    "restart", "reload" -> com.rknnovpn.panel.R.string.operation_reload
    "applyDesiredState" -> com.rknnovpn.panel.R.string.operation_apply_desired_state
    "profile-apply" -> com.rknnovpn.panel.R.string.operation_profile_apply
    "config-mutation" -> com.rknnovpn.panel.R.string.operation_config_import
    "reset" -> com.rknnovpn.panel.R.string.reset_network_rules
    else -> com.rknnovpn.panel.R.string.operation_runtime
}

private val TRANSIENT_OR_STOPPED_PHASES = setOf(
    BackendPhase.STOPPED,
    BackendPhase.APPLYING,
    BackendPhase.STARTING,
    BackendPhase.CONFIG_CHECKED,
    BackendPhase.CORE_SPAWNED,
    BackendPhase.CORE_LISTENING,
    BackendPhase.STOPPING,
    BackendPhase.RESETTING,
)
