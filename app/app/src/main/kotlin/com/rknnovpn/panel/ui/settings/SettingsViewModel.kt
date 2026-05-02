package com.rknnovpn.panel.ui.settings

import android.util.Log
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.rknnovpn.panel.BuildConfig
import com.rknnovpn.panel.i18n.UserMessageFormatter
import com.rknnovpn.panel.ipc.DaemonClient
import com.rknnovpn.panel.ipc.DaemonClientResult
import com.rknnovpn.panel.model.CachedUpdateCheckState
import com.rknnovpn.panel.model.ConnectionState
import com.rknnovpn.panel.model.DaemonStatus
import com.rknnovpn.panel.model.DnsIpv6Mode
import com.rknnovpn.panel.model.FallbackPolicy
import com.rknnovpn.panel.model.ProfileConfig
import com.rknnovpn.panel.model.RuntimeOperationResult
import com.rknnovpn.panel.model.RuntimeOperationStatus
import com.rknnovpn.panel.model.UpdateInstallState
import com.rknnovpn.panel.repository.CommandOutcome
import com.rknnovpn.panel.repository.ProfileRepository
import com.rknnovpn.panel.repository.StatusRepository
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.collect
import kotlinx.coroutines.flow.filterNotNull
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import javax.inject.Inject

private const val TAG = "SettingsViewModel"

enum class RoutingMode { GLOBAL, WHITELIST, BYPASS, RULES, DIRECT }

enum class DnsPreset(
    val remoteUrl: String,
    val directUrl: String,
    val bootstrapIp: String,
) {
    CLOUDFLARE("https://1.1.1.1/dns-query", "https://1.1.1.1/dns-query", "1.1.1.1"),
    GOOGLE("https://dns.google/dns-query", "https://dns.google/dns-query", "8.8.8.8"),
    MULLVAD("https://adblock.dns.mullvad.net/dns-query", "https://adblock.dns.mullvad.net/dns-query", "194.242.2.3"),
    ADGUARD("https://dns.adguard-dns.com/dns-query", "https://dns.adguard-dns.com/dns-query", "94.140.14.14"),
    CUSTOM("", "", ""),
}

enum class ThemeMode { LIGHT, DARK, SYSTEM }

enum class LogLevel { DEBUG, INFO, WARNING, ERROR, NONE }

enum class UpdateStatus {
    IDLE, CHECKING, UP_TO_DATE, AVAILABLE,
    DOWNLOADING, DOWNLOADED, INSTALLING, INSTALLED,
    MODULE_TOO_OLD, ERROR
}

data class UpdateUiState(
    val currentVersion: String = BuildConfig.VERSION_NAME,
    val latestVersion: String = "",
    val status: UpdateStatus = UpdateStatus.IDLE,
    val changelog: String = "",
    val downloadProgress: Float = 0f,
    val errorMessage: String = "",
    val modulePath: String = "",
    val apkPath: String = "",
)

data class SettingsUiState(
    val fallbackPolicy: FallbackPolicy = FallbackPolicy.OFFER_RESET,
    // Routing
    val routingMode: RoutingMode = RoutingMode.GLOBAL,
    val bypassRussia: Boolean = true,
    val directDomainsText: String = "",
    val proxyDomainsText: String = "",
    val blockDomainsText: String = "",
    val directIpsText: String = "",
    val proxyIpsText: String = "",
    val blockIpsText: String = "",
    // DNS
    val dnsPreset: DnsPreset = DnsPreset.CLOUDFLARE,
    val remoteDnsUrl: String = DnsPreset.CLOUDFLARE.remoteUrl,
    val directDnsUrl: String = DnsPreset.CLOUDFLARE.directUrl,
    val bootstrapDnsIp: String = DnsPreset.CLOUDFLARE.bootstrapIp,
    val dnsIpv6Mode: DnsIpv6Mode = DnsIpv6Mode.MIRROR,
    val fakeDns: Boolean = false,
    val urlTestUrl: String = "https://www.gstatic.com/generate_204",
    val alwaysDirectPackagesText: String = "",
    val alwaysDirectExcludedPackagesText: String = "",
    val alwaysDirectSystemApps: Boolean = true,
    val sharingEnabled: Boolean = false,
    val sharingInterfacesText: String = "",
    val logLevel: LogLevel = LogLevel.WARNING,
    // Module
    val moduleVersion: String = BuildConfig.VERSION_NAME,
    val daemonStatusText: String = "",
    // Theme
    val themeMode: ThemeMode = ThemeMode.SYSTEM,
    // About
    val appVersion: String = BuildConfig.VERSION_NAME,
    val githubUrl: String = "https://github.com/youtubediscord/RKNnoVPN",
    // Error
    val errorMessage: String? = null,
    val lastResetSummary: String? = null,
    val isResetting: Boolean = false,
    val runtimeActionActive: Boolean = false,
    val logsText: String = "",
    val isLoadingLogs: Boolean = false,
    val shareLogsText: String? = null,
    val shareLogsEventId: Long = 0L,
)

@HiltViewModel
class SettingsViewModel @Inject constructor(
    private val daemonClient: DaemonClient,
    private val profileRepository: ProfileRepository,
    private val statusRepository: StatusRepository,
    private val messages: UserMessageFormatter,
) : ViewModel() {

    private val _uiState = MutableStateFlow(SettingsUiState())
    val uiState: StateFlow<SettingsUiState> = _uiState.asStateFlow()

    private val _updateState = MutableStateFlow(UpdateUiState())
    val updateState: StateFlow<UpdateUiState> = _updateState.asStateFlow()

    private var lastCheckedHasUpdate: Boolean = false
    private var pendingDaemonOperationKinds: Set<String> = emptySet()

    init {
        observeProfile()
        observeRuntimeStatus()
    }

    // ---- Public actions ----

    fun setFallbackPolicy(policy: FallbackPolicy) {
        val previousPolicy = _uiState.value.fallbackPolicy
        _uiState.update { it.copy(fallbackPolicy = policy, errorMessage = null) }
        viewModelScope.launch {
            val ok = profileRepository.updateConfig { config ->
                config.copy(runtime = config.runtime.copy(fallbackPolicy = policy))
            }
            if (!ok) {
                val err = profileRepository.error.value
                Log.w(TAG, "Failed to set fallback policy: $err")
                profileRepository.refresh()
                _uiState.update { it.copy(fallbackPolicy = previousPolicy, errorMessage = err) }
            }
        }
    }

    fun setRoutingMode(mode: RoutingMode) {
        val previousMode = _uiState.value.routingMode
        _uiState.update { it.copy(routingMode = mode, errorMessage = null) }
        viewModelScope.launch {
            val profileMode = when (mode) {
                RoutingMode.GLOBAL -> com.rknnovpn.panel.model.RoutingMode.PROXY_ALL
                RoutingMode.WHITELIST -> com.rknnovpn.panel.model.RoutingMode.PER_APP
                RoutingMode.BYPASS -> com.rknnovpn.panel.model.RoutingMode.PER_APP_BYPASS
                RoutingMode.RULES -> com.rknnovpn.panel.model.RoutingMode.RULES
                RoutingMode.DIRECT -> com.rknnovpn.panel.model.RoutingMode.DIRECT
            }
            val ok = profileRepository.updateConfig { config ->
                config.copy(routing = config.routing.copy(mode = profileMode))
            }
            if (!ok) {
                val err = profileRepository.error.value
                Log.w(TAG, "Failed to set routing mode: $err")
                profileRepository.refresh()
                _uiState.update { it.copy(routingMode = previousMode, errorMessage = err) }
            }
        }
    }

    fun setDnsPreset(preset: DnsPreset) {
        val previousPreset = _uiState.value.dnsPreset
        if (preset == DnsPreset.CUSTOM) {
            _uiState.update { it.copy(dnsPreset = preset, errorMessage = null) }
            return
        }
        _uiState.update {
            it.copy(
                dnsPreset = preset,
                remoteDnsUrl = preset.remoteUrl,
                directDnsUrl = preset.directUrl,
                bootstrapDnsIp = preset.bootstrapIp,
                errorMessage = null,
            )
        }
        saveDnsToProfile(previousPreset)
    }

    fun setRemoteDnsUrl(url: String) {
        _uiState.update {
            it.copy(
                dnsPreset = DnsPreset.CUSTOM,
                remoteDnsUrl = url,
                errorMessage = null,
            )
        }
    }

    fun setDirectDnsUrl(url: String) {
        _uiState.update {
            it.copy(
                dnsPreset = DnsPreset.CUSTOM,
                directDnsUrl = url,
                errorMessage = null,
            )
        }
    }

    fun setBootstrapDnsIp(value: String) {
        _uiState.update {
            it.copy(
                dnsPreset = DnsPreset.CUSTOM,
                bootstrapDnsIp = value,
                errorMessage = null,
            )
        }
    }

    fun setDnsIpv6Mode(mode: DnsIpv6Mode) {
        val previous = _uiState.value.dnsIpv6Mode
        _uiState.update { it.copy(dnsIpv6Mode = mode, errorMessage = null) }
        viewModelScope.launch {
            val ok = profileRepository.updateConfig { config ->
                config.copy(dns = config.dns.copy(ipv6Mode = mode))
            }
            if (!ok) {
                val err = profileRepository.error.value
                Log.w(TAG, "Failed to save DNS IPv6 mode: $err")
                profileRepository.refresh()
                _uiState.update { it.copy(dnsIpv6Mode = previous, errorMessage = err) }
            }
        }
    }

    fun setFakeDns(enabled: Boolean) {
        val previous = _uiState.value.fakeDns
        _uiState.update { it.copy(fakeDns = enabled, errorMessage = null) }
        viewModelScope.launch {
            val ok = profileRepository.updateConfig { config ->
                config.copy(dns = config.dns.copy(fakeDns = enabled))
            }
            if (!ok) {
                val err = profileRepository.error.value
                Log.w(TAG, "Failed to save FakeDNS policy: $err")
                profileRepository.refresh()
                _uiState.update { it.copy(fakeDns = previous, errorMessage = err) }
            }
        }
    }

    fun applyDnsSettings() {
        val state = _uiState.value
        if (state.remoteDnsUrl.isBlank() || state.directDnsUrl.isBlank() || state.bootstrapDnsIp.isBlank()) {
            _uiState.update {
                it.copy(errorMessage = messages.get(com.rknnovpn.panel.R.string.dns_settings_required))
            }
            return
        }
        saveDnsToProfile(state.dnsPreset)
    }

    fun setUrlTestUrl(url: String) {
        _uiState.update { it.copy(urlTestUrl = url, errorMessage = null) }
    }

    fun setAlwaysDirectPackagesText(value: String) {
        _uiState.update { it.copy(alwaysDirectPackagesText = value, errorMessage = null) }
    }

    fun setAlwaysDirectSystemApps(enabled: Boolean) {
        val previous = _uiState.value.alwaysDirectSystemApps
        _uiState.update { it.copy(alwaysDirectSystemApps = enabled, errorMessage = null) }
        viewModelScope.launch {
            val ok = profileRepository.updateConfig { config ->
                config.copy(
                    routing = config.routing.copy(alwaysDirectSystemApps = enabled),
                )
            }
            if (!ok) {
                val err = profileRepository.error.value
                Log.w(TAG, "Failed to save system always-direct setting: $err")
                profileRepository.refresh()
                _uiState.update { it.copy(alwaysDirectSystemApps = previous, errorMessage = err) }
            }
        }
    }

    fun setBypassRussia(enabled: Boolean) {
        val previous = _uiState.value.bypassRussia
        _uiState.update { it.copy(bypassRussia = enabled, errorMessage = null) }
        viewModelScope.launch {
            val ok = profileRepository.updateConfig { config ->
                config.copy(
                    routing = config.routing.copy(bypassRussia = enabled),
                )
            }
            if (!ok) {
                val err = profileRepository.error.value
                Log.w(TAG, "Failed to save Russian direct routing setting: $err")
                profileRepository.refresh()
                _uiState.update { it.copy(bypassRussia = previous, errorMessage = err) }
            }
        }
    }

    fun setDirectDomainsText(value: String) {
        _uiState.update { it.copy(directDomainsText = value, errorMessage = null) }
    }

    fun setProxyDomainsText(value: String) {
        _uiState.update { it.copy(proxyDomainsText = value, errorMessage = null) }
    }

    fun setBlockDomainsText(value: String) {
        _uiState.update { it.copy(blockDomainsText = value, errorMessage = null) }
    }

    fun setDirectIpsText(value: String) {
        _uiState.update { it.copy(directIpsText = value, errorMessage = null) }
    }

    fun setProxyIpsText(value: String) {
        _uiState.update { it.copy(proxyIpsText = value, errorMessage = null) }
    }

    fun setBlockIpsText(value: String) {
        _uiState.update { it.copy(blockIpsText = value, errorMessage = null) }
    }

    fun applyRoutingRules() {
        val state = _uiState.value
        viewModelScope.launch {
            val ok = profileRepository.updateConfig { config ->
                config.copy(
                    routing = config.routing.copy(
                        directDomains = parseRuleList(state.directDomainsText),
                        proxyDomains = parseRuleList(state.proxyDomainsText),
                        blockDomains = parseRuleList(state.blockDomainsText),
                        directIps = parseRuleList(state.directIpsText),
                        proxyIps = parseRuleList(state.proxyIpsText),
                        blockIps = parseRuleList(state.blockIpsText),
                    ),
                )
            }
            if (!ok) {
                val err = profileRepository.error.value
                Log.w(TAG, "Failed to save routing rules: $err")
                profileRepository.refresh()
                _uiState.update { it.copy(errorMessage = err) }
            }
        }
    }

    fun addAlwaysDirectPackage(packageName: String) {
        val cleanPackage = packageName.trim()
        if (cleanPackage.isBlank()) return
        _uiState.update { state ->
            val packages = parsePackageList(state.alwaysDirectPackagesText)
                .toMutableList()
            if (cleanPackage !in packages) {
                packages += cleanPackage
            }
            val excluded = parsePackageList(state.alwaysDirectExcludedPackagesText)
                .filterNot { it == cleanPackage }
            state.copy(
                alwaysDirectPackagesText = packages.joinToString("\n"),
                alwaysDirectExcludedPackagesText = excluded.joinToString("\n"),
                errorMessage = null,
            )
        }
    }

    fun removeAlwaysDirectPackage(packageName: String) {
        val cleanPackage = packageName.trim()
        if (cleanPackage.isBlank()) return
        _uiState.update { state ->
            val packages = parsePackageList(state.alwaysDirectPackagesText)
                .filterNot { it == cleanPackage }
            state.copy(
                alwaysDirectPackagesText = packages.joinToString("\n"),
                errorMessage = null,
            )
        }
    }

    fun addAlwaysDirectExcludedPackage(packageName: String) {
        val cleanPackage = packageName.trim()
        if (cleanPackage.isBlank()) return
        _uiState.update { state ->
            val excluded = parsePackageList(state.alwaysDirectExcludedPackagesText)
                .toMutableList()
            if (cleanPackage !in excluded) {
                excluded += cleanPackage
            }
            val direct = parsePackageList(state.alwaysDirectPackagesText)
                .filterNot { it == cleanPackage }
            state.copy(
                alwaysDirectPackagesText = direct.joinToString("\n"),
                alwaysDirectExcludedPackagesText = excluded.joinToString("\n"),
                errorMessage = null,
            )
        }
    }

    fun removeAlwaysDirectExcludedPackage(packageName: String) {
        val cleanPackage = packageName.trim()
        if (cleanPackage.isBlank()) return
        _uiState.update { state ->
            val excluded = parsePackageList(state.alwaysDirectExcludedPackagesText)
                .filterNot { it == cleanPackage }
            state.copy(
                alwaysDirectExcludedPackagesText = excluded.joinToString("\n"),
                errorMessage = null,
            )
        }
    }

    fun applyUrlTestUrl() {
        val url = _uiState.value.urlTestUrl.trim()
        if (url.isBlank()) {
            _uiState.update {
                it.copy(errorMessage = messages.get(com.rknnovpn.panel.R.string.url_test_endpoint_required))
            }
            return
        }
        viewModelScope.launch {
            val ok = profileRepository.updateConfig { config ->
                config.copy(health = config.health.copy(checkUrl = url))
            }
            if (!ok) {
                val err = profileRepository.error.value
                Log.w(TAG, "Failed to save URL test setting: $err")
                profileRepository.refresh()
                _uiState.update { it.copy(errorMessage = err) }
            }
        }
    }

    fun applyAlwaysDirectPackages() {
        val excluded = parsePackageList(_uiState.value.alwaysDirectExcludedPackagesText)
        val packages = parsePackageList(_uiState.value.alwaysDirectPackagesText)
            .filterNot { it in excluded }
        viewModelScope.launch {
            val ok = profileRepository.updateConfig { config ->
                val restoredDirectPackages = config.routing.alwaysDirectExcludedAppList
                    .filterNot { it in excluded }
                    .toSet()
                config.copy(
                    routing = config.routing.copy(
                        alwaysDirectAppList = packages,
                        alwaysDirectExcludedAppList = excluded,
                        appProxyList = config.routing.appProxyList
                            .filterNot { it in packages || it in restoredDirectPackages },
                        appGroupRoutes = config.routing.appGroupRoutes
                            .filterKeys { it !in packages && it !in restoredDirectPackages },
                    ),
                )
            }
            if (!ok) {
                val err = profileRepository.error.value
                Log.w(TAG, "Failed to save always-direct apps: $err")
                profileRepository.refresh()
                _uiState.update { it.copy(errorMessage = err) }
            }
        }
    }

    fun setSharingEnabled(enabled: Boolean) {
        val previous = _uiState.value.sharingEnabled
        _uiState.update { it.copy(sharingEnabled = enabled, errorMessage = null) }
        viewModelScope.launch {
            val ok = profileRepository.updateConfig { config ->
                config.copy(sharing = config.sharing.copy(enabled = enabled))
            }
            if (!ok) {
                val err = profileRepository.error.value
                Log.w(TAG, "Failed to save sharing mode: $err")
                profileRepository.refresh()
                _uiState.update { it.copy(sharingEnabled = previous, errorMessage = err) }
            }
        }
    }

    fun setSharingInterfacesText(value: String) {
        _uiState.update { it.copy(sharingInterfacesText = value, errorMessage = null) }
    }

    fun applySharingInterfaces() {
        val interfaces = parsePackageList(_uiState.value.sharingInterfacesText)
        viewModelScope.launch {
            val ok = profileRepository.updateConfig { config ->
                config.copy(sharing = config.sharing.copy(interfaces = interfaces))
            }
            if (!ok) {
                val err = profileRepository.error.value
                Log.w(TAG, "Failed to save sharing interfaces: $err")
                profileRepository.refresh()
                _uiState.update { it.copy(errorMessage = err) }
            }
        }
    }

    fun setLogLevel(level: LogLevel) {
        _uiState.update { it.copy(logLevel = level, errorMessage = null) }
        // Log level is a local preference affecting which daemon logs we request.
    }

    fun restartDaemon() {
        if (_uiState.value.runtimeActionActive) return
        pendingDaemonOperationKinds = setOf("restart", "reload")
        _uiState.update {
            it.copy(
                daemonStatusText = messages.get(com.rknnovpn.panel.R.string.daemon_status_restarting),
                errorMessage = null,
                lastResetSummary = null,
            )
        }
        viewModelScope.launch {
            when (val outcome = statusRepository.reload()) {
                is CommandOutcome.Accepted -> {
                    Log.d(TAG, "Backend restart accepted")
                    _uiState.update {
                        it.copy(daemonStatusText = outcome.message)
                    }
                }
                is CommandOutcome.Success -> {
                    Log.d(TAG, "Backend restart succeeded")
                    pendingDaemonOperationKinds = emptySet()
                    _uiState.update {
                        it.copy(daemonStatusText = messages.get(com.rknnovpn.panel.R.string.daemon_status_restarted))
                    }
                }
                is CommandOutcome.Failed -> {
                    Log.w(TAG, "Backend restart failed: ${outcome.message}")
                    pendingDaemonOperationKinds = emptySet()
                    _uiState.update {
                        it.copy(
                            daemonStatusText = messages.get(com.rknnovpn.panel.R.string.state_error),
                            errorMessage = outcome.message,
                        )
                    }
                }
            }
        }
    }

    fun resetNetworkRules() {
        if (_uiState.value.runtimeActionActive || _uiState.value.isResetting) return
        _uiState.update {
            it.copy(
                daemonStatusText = messages.get(com.rknnovpn.panel.R.string.daemon_status_resetting),
                errorMessage = null,
                lastResetSummary = null,
                isResetting = true,
            )
        }
        viewModelScope.launch {
            when (val result = statusRepository.networkReset()) {
                is DaemonClientResult.Ok -> {
                    Log.d(TAG, "Backend reset accepted; waiting for status result")
                    _uiState.update {
                        it.copy(
                            daemonStatusText = messages.get(com.rknnovpn.panel.R.string.daemon_status_resetting),
                            errorMessage = null,
                            lastResetSummary = messages.get(com.rknnovpn.panel.R.string.operation_accepted),
                            isResetting = true,
                        )
                    }
                }
                else -> {
                    val message = formatUpdateError(result)
                    Log.w(TAG, "Backend reset failed: $message")
                    _uiState.update {
                        it.copy(
                            daemonStatusText = messages.get(com.rknnovpn.panel.R.string.state_error),
                            errorMessage = message,
                            isResetting = false,
                        )
                    }
                }
            }
        }
    }

    fun setThemeMode(mode: ThemeMode) {
        _uiState.update { it.copy(themeMode = mode) }
        // Theme is a pure UI preference, stored locally (SharedPreferences/DataStore).
        // Not sent to the daemon.
    }

    fun refreshRuntimeLogs() {
        _uiState.update { it.copy(isLoadingLogs = true, errorMessage = null) }
        viewModelScope.launch {
            when (val result = daemonClient.runtimeLogs(lines = 160)) {
                is DaemonClientResult.Ok -> {
                    _uiState.update {
                        it.copy(
                            logsText = result.data.text,
                            isLoadingLogs = false,
                        )
                    }
                }
                else -> {
                    val message = formatUpdateError(result)
                    Log.w(TAG, "Failed to load runtime logs: $message")
                    _uiState.update {
                        it.copy(
                            isLoadingLogs = false,
                            errorMessage = message,
                        )
                    }
                }
            }
        }
    }

    fun shareRuntimeLogs() {
        _uiState.update { it.copy(isLoadingLogs = true, errorMessage = null) }
        viewModelScope.launch {
            when (val result = daemonClient.diagnosticBundle(lines = 220)) {
                is DaemonClientResult.Ok -> {
                    val logs = result.data
                    _uiState.update {
                        it.copy(
                            logsText = logs,
                            shareLogsText = logs,
                            shareLogsEventId = it.shareLogsEventId + 1,
                            isLoadingLogs = false,
                        )
                    }
                }
                else -> {
                    when (val fallback = daemonClient.runtimeLogs(lines = 220)) {
                        is DaemonClientResult.Ok -> {
                            val logs = fallback.data.text
                            _uiState.update {
                                it.copy(
                                    logsText = logs,
                                    shareLogsText = logs,
                                    shareLogsEventId = it.shareLogsEventId + 1,
                                    isLoadingLogs = false,
                                )
                            }
                        }
                        else -> {
                            val message = formatUpdateError(result)
                            Log.w(TAG, "Failed to prepare diagnostic bundle: $message")
                            _uiState.update {
                                it.copy(
                                    isLoadingLogs = false,
                                    errorMessage = message,
                                )
                            }
                        }
                    }
                }
            }
        }
    }

    fun clearSharedLogs() {
        _uiState.update { it.copy(shareLogsText = null) }
    }

    fun refreshProfile() {
        viewModelScope.launch {
            profileRepository.refresh()
        }
    }

    // ---- Update actions ----

    fun checkForUpdates() {
        _updateState.update { it.copy(status = UpdateStatus.CHECKING, errorMessage = "") }
        viewModelScope.launch {
            when (val result = daemonClient.updateCheck()) {
                is DaemonClientResult.Ok -> {
                    val info = result.data
                    lastCheckedHasUpdate = info.hasUpdate
                    _updateState.update {
                        it.copy(
                            currentVersion = info.currentVersion,
                            latestVersion = info.latestVersion,
                            changelog = info.changelog,
                            status = if (info.hasUpdate) UpdateStatus.AVAILABLE else UpdateStatus.UP_TO_DATE,
                            modulePath = "",
                            apkPath = "",
                        )
                    }
                }
                else -> {
                    _updateState.update {
                        it.copy(
                            status = UpdateStatus.ERROR,
                            errorMessage = formatUpdateError(result),
                        )
                    }
                }
            }
        }
    }

    fun downloadUpdate() {
        _updateState.update { it.copy(status = UpdateStatus.DOWNLOADING, downloadProgress = 0f) }
        viewModelScope.launch {
            if (!lastCheckedHasUpdate) {
                _updateState.update {
                    it.copy(
                        status = UpdateStatus.ERROR,
                        errorMessage = messages.get(com.rknnovpn.panel.R.string.update_error_no_checked_update),
                    )
                }
                return@launch
            }

            when (val result = daemonClient.updateDownload()) {
                is DaemonClientResult.Ok -> {
                    val downloaded = result.data
                    val hasArtifacts = downloaded.modulePath.isNotBlank() && downloaded.apkPath.isNotBlank()
                    _updateState.update {
                        it.copy(
                            status = if (hasArtifacts) UpdateStatus.DOWNLOADED else UpdateStatus.ERROR,
                            downloadProgress = if (hasArtifacts) 1f else 0f,
                            errorMessage = if (hasArtifacts) {
                                ""
                            } else {
                                messages.get(com.rknnovpn.panel.R.string.update_error_pair_required)
                            },
                            modulePath = downloaded.modulePath,
                            apkPath = downloaded.apkPath,
                        )
                    }
                }
                else -> {
                    _updateState.update {
                        it.copy(
                            status = UpdateStatus.ERROR,
                            errorMessage = formatUpdateError(result),
                        )
                    }
                }
            }
        }
    }

    fun installUpdate() {
        if (_uiState.value.runtimeActionActive) return
        _updateState.update { it.copy(status = UpdateStatus.INSTALLING) }
        viewModelScope.launch {
            val current = _updateState.value
            if (current.modulePath.isBlank() || current.apkPath.isBlank()) {
                _updateState.update {
                    it.copy(
                        status = UpdateStatus.ERROR,
                        errorMessage = messages.get(com.rknnovpn.panel.R.string.update_error_no_downloaded_update),
                    )
                }
                return@launch
            }

            when (val result = daemonClient.updateInstall(
                modulePath = current.modulePath,
                apkPath = current.apkPath,
            )) {
                is DaemonClientResult.Ok -> {
                    result.data.runtimeStatus?.let(statusRepository::publishBackendStatus)
                    _updateState.update {
                        it.copy(
                            status = if (result.data.accepted) UpdateStatus.INSTALLING else UpdateStatus.ERROR,
                            errorMessage = if (result.data.accepted) {
                                messages.get(com.rknnovpn.panel.R.string.operation_accepted)
                            } else {
                                messages.get(com.rknnovpn.panel.R.string.update_error_install_without_artifacts)
                            },
                        )
                    }
                }
                is DaemonClientResult.DaemonError -> {
                    _updateState.update {
                        it.copy(
                            status = UpdateStatus.ERROR,
                            errorMessage = formatUpdateError(result),
                        )
                    }
                }
                is DaemonClientResult.DaemonNotFound -> {
                    _updateState.update { it.copy(status = UpdateStatus.MODULE_TOO_OLD) }
                }
                else -> {
                    _updateState.update {
                        it.copy(
                            status = UpdateStatus.ERROR,
                            errorMessage = formatUpdateError(result),
                        )
                    }
                }
            }
        }
    }

    // ---- Internal ----

    /**
     * Load the current daemon profile config and project relevant settings
     * into [SettingsUiState].
     */
    private fun observeProfile() {
        viewModelScope.launch {
            profileRepository.profile
                .filterNotNull()
                .collect(::applyProfileConfig)
        }
        viewModelScope.launch {
            profileRepository.getOrLoad()
        }
    }

    private fun observeRuntimeStatus() {
        viewModelScope.launch {
            statusRepository.status
                .filterNotNull()
                .collect { status ->
                    val completedUpdateInstall = status.lastOperation
                        ?.takeIf { operation -> operation.kind == "update-install" && status.activeOperation == null }
                    val completedDaemonOperation = status.lastOperation
                        ?.takeIf { operation ->
                            operation.kind in pendingDaemonOperationKinds && status.activeOperation == null
                        }
                    if (completedUpdateInstall != null) {
                        _updateState.update { current ->
                            if (current.status != UpdateStatus.INSTALLING) {
                                current
                            } else if (completedUpdateInstall.succeeded) {
                                current.copy(
                                    status = UpdateStatus.INSTALLED,
                                    errorMessage = "",
                                )
                            } else {
                                current.copy(
                                    status = UpdateStatus.ERROR,
                                    errorMessage = completedUpdateInstall.errorMessage.ifBlank {
                                        messages.get(com.rknnovpn.panel.R.string.update_error_install_failed)
                                    },
                                )
                            }
                        }
                    }
                    status.updateInstall
                        ?.takeIf { install ->
                            status.activeOperation == null &&
                                install.status in setOf("running", "failed", "unknown")
                        }
                        ?.let { install ->
                            _updateState.update {
                                it.copy(
                                    status = UpdateStatus.ERROR,
                                    errorMessage = formatPersistedUpdateInstallState(install),
                                )
                            }
                        }
                    applyCachedUpdateCheck(status.updateCheck)
                    _uiState.update {
                        val resetResult = status.lastOperation
                            ?.takeIf { operation -> it.isResetting && operation.kind == "reset" && status.activeOperation == null }
                        if (resetResult != null) {
                            val report = resetResult.resetReport
                            val resetFailure = resetResult
                                .takeUnless { operation -> operation.succeeded }
                                ?.let { operation ->
                                    messages.formatOperationFailure(
                                        operation.kind.operationNameRes(),
                                        operation,
                                        status.health.rollbackApplied,
                                    )
                                }
                            it.copy(
                                daemonStatusText = if (resetResult.succeeded) {
                                    messages.get(com.rknnovpn.panel.R.string.daemon_status_stopped)
                                } else {
                                    messages.get(com.rknnovpn.panel.R.string.daemon_status_partial_reset)
                                },
                                errorMessage = resetFailure,
                                lastResetSummary = report?.let(::summarizeResetReport)
                                    ?: resetResult.errorMessage.ifBlank { messages.get(com.rknnovpn.panel.R.string.state_error) },
                                isResetting = false,
                                runtimeActionActive = false,
                            )
                        } else {
                            val completedDaemonFailure = completedDaemonOperation
                                ?.takeUnless { operation -> operation.succeeded }
                                ?.let { operation ->
                                    messages.formatOperationFailure(
                                        operation.kind.operationNameRes(),
                                        operation,
                                        status.health.rollbackApplied,
                                    )
                                }
                            it.copy(
                                daemonStatusText = when {
                                    completedDaemonOperation?.succeeded == true ->
                                        formatCompletedOperation(completedDaemonOperation)
                                    completedDaemonFailure != null ->
                                        messages.get(com.rknnovpn.panel.R.string.state_error)
                                    else ->
                                        formatRuntimeStatus(status, it.daemonStatusText)
                                },
                                errorMessage = completedDaemonFailure ?: it.errorMessage,
                                isResetting = status.activeOperation?.kind == "reset",
                                runtimeActionActive = status.activeOperation != null,
                            )
                        }
                    }
                    if (completedDaemonOperation != null) {
                        pendingDaemonOperationKinds = emptySet()
                    }
                }
        }
    }

    private fun applyCachedUpdateCheck(updateCheck: CachedUpdateCheckState?) {
        if (updateCheck == null || updateCheck.lastCheckedAt.isNullOrBlank()) {
            return
        }
        lastCheckedHasUpdate = updateCheck.hasUpdate
        _updateState.update { current ->
            if (current.status !in setOf(UpdateStatus.IDLE, UpdateStatus.UP_TO_DATE, UpdateStatus.AVAILABLE)) {
                return@update current
            }
            current.copy(
                currentVersion = updateCheck.currentVersion.ifBlank { current.currentVersion },
                latestVersion = updateCheck.latestVersion.ifBlank { current.latestVersion },
                changelog = updateCheck.changelog.ifBlank { current.changelog },
                status = when {
                    updateCheck.error.isNotBlank() -> UpdateStatus.ERROR
                    updateCheck.hasUpdate -> UpdateStatus.AVAILABLE
                    else -> UpdateStatus.UP_TO_DATE
                },
                errorMessage = updateCheck.error,
            )
        }
    }

    private fun applyProfileConfig(config: ProfileConfig) {
        val routingMode = when (config.routing.mode) {
            com.rknnovpn.panel.model.RoutingMode.PROXY_ALL -> RoutingMode.GLOBAL
            com.rknnovpn.panel.model.RoutingMode.PER_APP -> RoutingMode.WHITELIST
            com.rknnovpn.panel.model.RoutingMode.PER_APP_BYPASS -> RoutingMode.BYPASS
            com.rknnovpn.panel.model.RoutingMode.DIRECT -> RoutingMode.DIRECT
            com.rknnovpn.panel.model.RoutingMode.RULES -> RoutingMode.RULES
        }

        val dnsPreset = DnsPreset.entries.find {
            it != DnsPreset.CUSTOM &&
                it.remoteUrl == config.dns.remoteDns &&
                it.directUrl == config.dns.directDns
        }
            ?: DnsPreset.CUSTOM

        _uiState.update {
            it.copy(
                fallbackPolicy = config.runtime.fallbackPolicy,
                routingMode = routingMode,
                bypassRussia = config.routing.bypassRussia,
                directDomainsText = config.routing.directDomains.joinToString("\n"),
                proxyDomainsText = config.routing.proxyDomains.joinToString("\n"),
                blockDomainsText = config.routing.blockDomains.joinToString("\n"),
                directIpsText = config.routing.directIps.joinToString("\n"),
                proxyIpsText = config.routing.proxyIps.joinToString("\n"),
                blockIpsText = config.routing.blockIps.joinToString("\n"),
                dnsPreset = dnsPreset,
                remoteDnsUrl = config.dns.remoteDns,
                directDnsUrl = config.dns.directDns,
                bootstrapDnsIp = config.dns.bootstrapIp,
                dnsIpv6Mode = config.dns.ipv6Mode,
                fakeDns = config.dns.fakeDns,
                urlTestUrl = config.health.checkUrl,
                alwaysDirectPackagesText = config.routing.alwaysDirectAppList.joinToString("\n"),
                alwaysDirectExcludedPackagesText = config.routing.alwaysDirectExcludedAppList.joinToString("\n"),
                alwaysDirectSystemApps = config.routing.alwaysDirectSystemApps,
                sharingEnabled = config.sharing.enabled,
                sharingInterfacesText = config.sharing.interfaces.joinToString("\n"),
            )
        }
    }

    /**
     * Save DNS settings to the daemon profile config.
     */
    private fun saveDnsToProfile(previousPreset: DnsPreset) {
        val state = _uiState.value
        viewModelScope.launch {
            val ok = profileRepository.updateConfig { config ->
                config.copy(
                    dns = config.dns.copy(
                        remoteDns = state.remoteDnsUrl.trim(),
                        directDns = state.directDnsUrl.trim(),
                        bootstrapIp = state.bootstrapDnsIp.trim(),
                        ipv6Mode = state.dnsIpv6Mode,
                        fakeDns = state.fakeDns,
                    )
                )
            }
            if (!ok) {
                val err = profileRepository.error.value
                Log.w(TAG, "Failed to save DNS setting: $err")
                profileRepository.refresh()
                _uiState.update { it.copy(dnsPreset = previousPreset, errorMessage = err) }
            }
        }
    }

    private fun formatRuntimeStatus(status: DaemonStatus, fallback: String): String {
        if (!profileRepository.profile.value.hasAvailableNodes()) {
            return messages.get(com.rknnovpn.panel.R.string.daemon_status_no_nodes_first_setup)
        }
        val healthIssue = messages.formatHealthIssue(
            status.health.lastCode,
            status.health.lastError,
            status.health.lastUserMessage,
            status.health.stageReport,
        )
        return when (status.state) {
            ConnectionState.CONNECTED -> {
                if (status.health.healthy && !status.health.operationalHealthy) {
                    messages.get(
                        com.rknnovpn.panel.R.string.daemon_status_running_degraded,
                        healthIssue,
                    )
                } else {
                    fallback.ifBlank { messages.get(com.rknnovpn.panel.R.string.state_connected) }
                }
            }
            ConnectionState.CONNECTING ->
                when {
                    status.activeOperation?.stuck == true ->
                        formatStuckOperation(
                            status.activeOperation.stepDetail,
                            status.activeOperation.step,
                        )
                    status.activeOperation != null ->
                        formatActiveOperation(status.activeOperation)
                    else ->
                        messages.get(com.rknnovpn.panel.R.string.state_connecting)
                }
            ConnectionState.DISCONNECTED ->
                messages.get(com.rknnovpn.panel.R.string.daemon_status_stopped)
            ConnectionState.ERROR ->
                messages.get(
                    com.rknnovpn.panel.R.string.daemon_status_error_with_reason,
                    healthIssue,
                )
            ConnectionState.UNKNOWN ->
                messages.get(com.rknnovpn.panel.R.string.daemon_status_unknown_text)
        }
    }

    private fun formatActiveOperation(operation: RuntimeOperationStatus): String =
        messages.formatActiveOperationStep(operation.stepDetail, operation.step, operation.stepCode)

    private fun formatCompletedOperation(operation: RuntimeOperationResult): String =
        when (operation.kind) {
            "restart", "reload" -> messages.get(com.rknnovpn.panel.R.string.daemon_status_restarted)
            "start" -> messages.get(com.rknnovpn.panel.R.string.state_connected)
            "stop" -> messages.get(com.rknnovpn.panel.R.string.daemon_status_stopped)
            else -> messages.get(com.rknnovpn.panel.R.string.dns_ok)
        }

    private fun formatStuckOperation(stepDetail: String, step: String): String {
        val currentStep = stepDetail.ifBlank { step }.trim()
        return if (currentStep.isBlank()) {
            messages.get(com.rknnovpn.panel.R.string.daemon_status_operation_stuck)
        } else {
            messages.get(com.rknnovpn.panel.R.string.daemon_status_operation_stuck_with_step, currentStep)
        }
    }

    private fun <T> formatUpdateError(result: DaemonClientResult<T>): String =
        messages.formatDaemonFailure(result)

    private fun formatPersistedUpdateInstallState(state: UpdateInstallState): String {
        val step = state.step.ifBlank { state.code }.ifBlank { state.status }
        val detail = state.detail.ifBlank { state.code }.ifBlank { state.status }
        return messages.get(com.rknnovpn.panel.R.string.update_error_install_interrupted, step, detail)
    }

    private fun parsePackageList(raw: String): List<String> =
        raw.split('\n', ',', ';', ' ', '\t')
            .map(String::trim)
            .filter(String::isNotBlank)
            .distinct()

    private fun ProfileConfig?.hasAvailableNodes(): Boolean =
        this?.nodes?.any { !it.stale } == true

    private fun parseRuleList(raw: String): List<String> =
        raw.split('\n', ',', ';', ' ', '\t')
            .map(String::trim)
            .filter(String::isNotBlank)
            .distinct()

    private fun summarizeResetReport(report: com.rknnovpn.panel.model.ResetReport): String {
        if (report.errors.isEmpty() && report.warnings.isEmpty() && report.leftovers.isEmpty()) {
            return messages.get(com.rknnovpn.panel.R.string.reset_summary_all_done)
        }
        return buildString {
            append(
                messages.get(
                    if (report.errors.isEmpty()) {
                        com.rknnovpn.panel.R.string.reset_summary_warning_prefix
                    } else {
                        com.rknnovpn.panel.R.string.reset_summary_partial_prefix
                    },
                ),
            )
            append('\n')
            val stepLines = report.steps.filter { it.status != "ok" }.map { step ->
                val stepValue = if (step.detail.isBlank()) {
                    when (step.status.lowercase()) {
                        "error" -> messages.get(com.rknnovpn.panel.R.string.state_error)
                        "ok" -> messages.get(com.rknnovpn.panel.R.string.node_test_status_ok)
                        else -> step.status
                    }
                } else {
                    step.detail
                }
                messages.get(
                    com.rknnovpn.panel.R.string.reset_summary_step,
                    step.name,
                    stepValue,
                )
            }
            val warningLines = report.warnings.filter { it.isNotBlank() }
            append((stepLines + warningLines).joinToString("\n"))
            if (report.leftovers.isNotEmpty()) {
                append('\n')
                append(messages.get(com.rknnovpn.panel.R.string.reset_summary_leftovers_prefix))
                append('\n')
                append(report.leftovers.joinToString("\n"))
            }
            if (report.rebootRequired) {
                append('\n')
                append(messages.get(com.rknnovpn.panel.R.string.reset_summary_reboot_required))
            }
        }
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
