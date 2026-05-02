package com.rknnovpn.panel.ui.apps

import android.content.Context
import android.content.pm.ApplicationInfo
import android.content.pm.PackageManager
import android.util.Log
import com.rknnovpn.panel.i18n.UserMessageFormatter
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.rknnovpn.panel.advisor.AlwaysDirectApps
import com.rknnovpn.panel.model.ProfileConfig
import com.rknnovpn.panel.model.RoutingMode
import com.rknnovpn.panel.repository.ProfileRepository
import dagger.hilt.android.lifecycle.HiltViewModel
import dagger.hilt.android.qualifiers.ApplicationContext
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.collect
import kotlinx.coroutines.flow.filterNotNull
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import javax.inject.Inject

private const val TAG = "AppPickerViewModel"

private data class RoutingSelection(
    val routingMode: RoutingMode,
    val selectedPackages: Set<String>,
    val alwaysDirectPackages: Set<String>,
    val alwaysDirectExcludedPackages: Set<String>,
    val alwaysDirectSystemApps: Boolean,
    val appGroupRoutes: Map<String, String>,
    val nodeGroups: List<String>,
)

/**
 * Lightweight representation of an installed Android app for the UI layer.
 */
data class AppInfo(
    val packageName: String,
    val label: String,
    val isSystemApp: Boolean,
    val isProxied: Boolean = false,
    val isAlwaysDirect: Boolean = false,
    val isAlwaysDirectExcluded: Boolean = false,
    val nodeGroup: String = "",
)

data class AppPickerUiState(
    val apps: List<AppInfo> = emptyList(),
    val routingMode: RoutingMode = RoutingMode.PER_APP,
    val searchQuery: String = "",
    val showSystemApps: Boolean = false,
    val nodeGroups: List<String> = emptyList(),
    val isLoading: Boolean = true,
    /** Error message from the last operation, or null. */
    val errorMessage: String? = null,
) {
    val filteredApps: List<AppInfo>
        get() {
            var list = apps
            if (!showSystemApps) {
                list = list.filter { !it.isSystemApp }
            }
            if (searchQuery.isNotBlank()) {
                val q = searchQuery.lowercase()
                list = list.filter {
                    it.label.lowercase().contains(q) ||
                        it.packageName.lowercase().contains(q)
                }
            }
            return list.sortedWith(
                compareBy<AppInfo> { it.label.lowercase() }
                    .thenBy { it.packageName }
            )
        }

    val proxiedCount: Int
        get() = apps.count { it.isProxied }

    val supportsPerAppSelection: Boolean
        get() = routingMode == RoutingMode.PER_APP || routingMode == RoutingMode.PER_APP_BYPASS
            || routingMode == RoutingMode.RULES
}

/** Well-known package names for quick-select templates. */
private val BROWSER_PACKAGES = setOf(
    "com.android.chrome", "org.mozilla.firefox", "com.brave.browser",
    "com.opera.browser", "com.opera.mini.native", "com.vivaldi.browser",
    "com.microsoft.emmx", "org.chromium.chrome",
)
private val SOCIAL_PACKAGES = setOf(
    "com.twitter.android", "com.instagram.android", "com.facebook.katana",
    "org.telegram.messenger", "com.whatsapp", "com.discord",
    "com.snapchat.android", "com.reddit.frontpage",
)
private val STREAMING_PACKAGES = setOf(
    "com.google.android.youtube", "com.netflix.mediaclient",
    "com.spotify.music", "tv.twitch.android.app",
    "com.amazon.avod", "com.disney.disneyplus",
)
private val BANK_PACKAGES = setOf(
    "ru.sberbankmobile", "ru.alfabank.mobile.android",
    "ru.tinkoff.investing", "ru.vtb24.mobilebanking.android",
    "com.idamob.tinkoff.android", "ru.raiffeisennews",
)

@HiltViewModel
class AppPickerViewModel @Inject constructor(
    @ApplicationContext private val context: Context,
    private val profileRepository: ProfileRepository,
    private val messages: UserMessageFormatter,
) : ViewModel() {

    private val _uiState = MutableStateFlow(AppPickerUiState())
    val uiState: StateFlow<AppPickerUiState> = _uiState.asStateFlow()

    init {
        observeProfile()
        loadApps()
    }

    fun setSearchQuery(query: String) {
        _uiState.update { it.copy(searchQuery = query) }
    }

    fun clearSearchQuery() {
        _uiState.update { state ->
            if (state.searchQuery.isBlank()) state else state.copy(searchQuery = "")
        }
    }

    fun toggleShowSystemApps() {
        _uiState.update { it.copy(showSystemApps = !it.showSystemApps) }
    }

    fun refreshProfile() {
        viewModelScope.launch {
            profileRepository.refresh()
        }
    }

    fun toggleApp(packageName: String) {
        if (!_uiState.value.supportsPerAppSelection) return
        _uiState.update { state ->
            state.copy(
                apps = state.apps.map {
                    if (it.packageName == packageName && !it.isAlwaysDirect) {
                        it.copy(isProxied = !it.isProxied)
                    }
                    else it
                },
            )
        }
    }

    fun selectAllVisible() {
        if (!_uiState.value.supportsPerAppSelection) return
        _uiState.update { state ->
            val visiblePackages = state.filteredApps.map { it.packageName }.toSet()
            state.copy(
                apps = state.apps.map { app ->
                    if (app.packageName !in visiblePackages) {
                        app
                    } else if (app.isAlwaysDirect && state.routingMode != RoutingMode.PER_APP_BYPASS) {
                        app.copy(isProxied = false)
                    } else {
                        app.copy(isProxied = true)
                    }
                },
            )
        }
    }

    fun clearSelection() {
        if (!_uiState.value.supportsPerAppSelection) return
        _uiState.update { state ->
            state.copy(
                apps = state.apps.map { app ->
                    app.copy(
                        isProxied = app.isAlwaysDirect && state.routingMode == RoutingMode.PER_APP_BYPASS,
                    )
                },
            )
        }
    }

    fun setAppGroup(packageName: String, group: String) {
        _uiState.update { state ->
            state.copy(
                apps = state.apps.map { app ->
                    if (app.packageName == packageName) {
                        app.copy(
                            nodeGroup = group,
                            isProxied = if (
                                group.isNotBlank() &&
                                (state.routingMode == RoutingMode.PER_APP || state.routingMode == RoutingMode.RULES) &&
                                !app.isAlwaysDirect
                            ) {
                                true
                            } else {
                                app.isProxied
                            },
                        )
                    } else {
                        app
                    }
                },
            )
        }
    }

    fun allowAlwaysDirectAppThroughProxy(packageName: String) {
        val cleanPackage = packageName.trim()
        if (cleanPackage.isBlank()) return
        val draft = _uiState.value
        val draftSelectedPackages = draft.selectedPackageListForSave(forceProxyPackage = cleanPackage)
        val draftAppGroupRoutes = draft.appGroupRoutesForSave()
        viewModelScope.launch {
            val ok = profileRepository.updateConfig { config ->
                val excluded = (config.routing.alwaysDirectExcludedAppList + cleanPackage)
                    .map(String::trim)
                    .filter(String::isNotBlank)
                    .distinct()
                val direct = config.routing.alwaysDirectAppList
                    .filterNot { it == cleanPackage }
                config.copy(
                    routing = config.routing.copy(
                        alwaysDirectAppList = direct,
                        alwaysDirectExcludedAppList = excluded,
                        appProxyList = if (draft.routingMode == RoutingMode.PER_APP || draft.routingMode == RoutingMode.RULES) {
                            draftSelectedPackages
                        } else {
                            config.routing.appProxyList
                        },
                        appBypassList = if (draft.routingMode == RoutingMode.PER_APP_BYPASS) {
                            draftSelectedPackages
                        } else {
                            config.routing.appBypassList
                        },
                        appGroupRoutes = draftAppGroupRoutes,
                    ),
                )
            }
            if (!ok) {
                val err = profileRepository.error.value
                Log.w(TAG, "Failed to exclude always-direct app: $err")
                _uiState.update { it.copy(errorMessage = err) }
            }
        }
    }

    fun restoreAlwaysDirectApp(packageName: String) {
        val cleanPackage = packageName.trim()
        if (cleanPackage.isBlank()) return
        viewModelScope.launch {
            val ok = profileRepository.updateConfig { config ->
                config.copy(
                    routing = config.routing.copy(
                        alwaysDirectExcludedAppList = config.routing.alwaysDirectExcludedAppList
                            .filterNot { it == cleanPackage },
                        appProxyList = config.routing.appProxyList
                            .filterNot { it == cleanPackage },
                        appGroupRoutes = config.routing.appGroupRoutes
                            .filterKeys { it != cleanPackage },
                    ),
                )
            }
            if (!ok) {
                val err = profileRepository.error.value
                Log.w(TAG, "Failed to restore always-direct app: $err")
                _uiState.update { it.copy(errorMessage = err) }
            }
        }
    }

    fun applyTemplate(template: AppTemplate) {
        if (!_uiState.value.supportsPerAppSelection) return
        _uiState.update { state ->
            val visiblePackages = state.filteredApps.map { it.packageName }.toSet()
            val targetPackages = when (template) {
                AppTemplate.BROWSERS -> BROWSER_PACKAGES intersect visiblePackages
                AppTemplate.SOCIAL -> SOCIAL_PACKAGES intersect visiblePackages
                AppTemplate.STREAMING -> STREAMING_PACKAGES intersect visiblePackages
                AppTemplate.ALL_EXCEPT_BANKS -> {
                    if (state.routingMode == RoutingMode.PER_APP_BYPASS) {
                        // In bypass mode selected means "direct", so select only
                        // protected apps instead of selecting everything else.
                        state.apps
                            .filter { it.packageName in visiblePackages }
                            .filter { it.isAlwaysDirect || it.packageName in BANK_PACKAGES }
                            .map { it.packageName }
                            .toSet()
                    } else {
                        // In whitelist mode selected means "proxied", so select
                        // visible apps except sensitive/RU/network clients.
                        state.apps
                            .filter { it.packageName in visiblePackages }
                            .filterNot { it.isAlwaysDirect || it.packageName in BANK_PACKAGES }
                            .map { it.packageName }
                            .toSet()
                    }
                }
            }

            state.copy(
                apps = state.apps.map { app ->
                    if (template == AppTemplate.ALL_EXCEPT_BANKS) {
                        app.copy(isProxied = app.packageName in targetPackages)
                    } else {
                        // For specific templates, toggle ON those packages, leave others unchanged
                        if (app.packageName in targetPackages && !app.isAlwaysDirect) app.copy(isProxied = true)
                        else app
                    }
                },
            )
        }
    }

    fun applySelection() {
        val selectedPackages = _uiState.value.selectedPackageListForSave()
        val routingMode = _uiState.value.routingMode
        val appGroupRoutes = _uiState.value.appGroupRoutesForSave()

        viewModelScope.launch {
            _uiState.update { it.copy(errorMessage = null) }

            if (!routingMode.usesPerAppSelection()) {
                _uiState.update {
                    it.copy(
                        errorMessage = messages.get(
                            com.rknnovpn.panel.R.string.app_picker_error_selection_unavailable
                        )
                    )
                }
                return@launch
            }

            val ok = profileRepository.updateConfig { config ->
                config.copy(
                    routing = config.routing.copy(
                        appProxyList = if (routingMode == RoutingMode.PER_APP || routingMode == RoutingMode.RULES) {
                            selectedPackages
                        } else {
                            emptyList()
                        },
                        appBypassList = if (routingMode == RoutingMode.PER_APP_BYPASS) selectedPackages else emptyList(),
                        appGroupRoutes = appGroupRoutes,
                    ),
                )
            }
            if (!ok) {
                val err = profileRepository.error.value
                Log.w(TAG, "Failed to save app selection: $err")
                profileRepository.refresh()
                _uiState.update {
                    it.copy(
                        errorMessage = err ?: messages.get(
                            com.rknnovpn.panel.R.string.app_picker_error_save_selection
                        )
                    )
                }
            } else {
                Log.d(TAG, "Saved ${selectedPackages.size} apps for routing mode $routingMode")
            }
        }
    }

    fun enablePerAppProxyMode() {
        viewModelScope.launch {
            val ok = profileRepository.updateConfig { config ->
                config.copy(routing = config.routing.copy(mode = RoutingMode.PER_APP))
            }
            if (!ok) {
                val err = profileRepository.error.value
                Log.w(TAG, "Failed to enable per-app proxy mode: $err")
                _uiState.update { it.copy(errorMessage = err) }
            }
        }
    }

    // ---- Internal ----

    /**
     * Load the real list of installed apps from PackageManager, then merge
     * with the daemon's per-app proxy list to mark which apps are proxied.
     */
    private fun loadApps() {
        viewModelScope.launch {
            _uiState.update { it.copy(isLoading = true, errorMessage = null) }

            try {
                // 1. Get installed apps from PackageManager (runs on IO)
                val installedApps = withContext(Dispatchers.IO) {
                    queryInstalledApps()
                }

                // 2. Get the current per-app proxy list from daemon config
                val daemonSelection = loadRoutingSelectionFromDaemon()

                // 3. Merge into UI model
                val appInfoList = installedApps.map { (pkg, label, isSystem) ->
                    val isAlwaysDirect = isAlwaysDirectApp(
                        packageName = pkg,
                        isSystemApp = isSystem,
                        selection = daemonSelection,
                    )
                    AppInfo(
                        packageName = pkg,
                        label = label,
                        isSystemApp = isSystem,
                        isAlwaysDirect = isAlwaysDirect,
                        isAlwaysDirectExcluded = pkg in daemonSelection.alwaysDirectExcludedPackages,
                        nodeGroup = daemonSelection.appGroupRoutes[pkg].orEmpty(),
                        isProxied = if (isAlwaysDirect && daemonSelection.routingMode == RoutingMode.PER_APP_BYPASS) {
                            true
                        } else {
                            pkg in daemonSelection.selectedPackages
                        },
                    )
                }.sortedBy { it.label.lowercase() }

                _uiState.update {
                    it.copy(
                        apps = appInfoList,
                        routingMode = daemonSelection.routingMode,
                        nodeGroups = daemonSelection.nodeGroups,
                        isLoading = false,
                    )
                }
            } catch (e: Exception) {
                Log.e(TAG, "Failed to load apps", e)
                _uiState.update {
                    it.copy(
                        isLoading = false,
                        errorMessage = messages.get(
                            com.rknnovpn.panel.R.string.app_picker_error_load_apps,
                            e.message ?: messages.get(com.rknnovpn.panel.R.string.daemon_status_unknown_text),
                        ),
                    )
                }
            }
        }
    }

    /**
     * Query all installed applications via Android PackageManager.
     * Returns a list of (packageName, label, isSystemApp) triples.
     */
    private fun queryInstalledApps(): List<Triple<String, String, Boolean>> {
        val pm = context.packageManager
        @Suppress("DEPRECATION")
        val applications = pm.getInstalledApplications(PackageManager.GET_META_DATA)

        return applications.mapNotNull { appInfo ->
            // Skip apps without a launcher intent (pure services, providers, etc.)
            // but keep system apps that the user might want to route
            val label = try {
                appInfo.loadLabel(pm).toString()
            } catch (_: Exception) {
                appInfo.packageName
            }

            val isSystem = (appInfo.flags and ApplicationInfo.FLAG_SYSTEM) != 0

            Triple(appInfo.packageName, label, isSystem)
        }
    }

    /**
     * Load the set of package names currently configured for per-app proxy
     * from the daemon's profile config.
     */
    private suspend fun loadRoutingSelectionFromDaemon(): RoutingSelection {
        val config = profileRepository.getOrLoad()
        if (config != null) {
            return config.toRoutingSelection()
        }

        val err = profileRepository.error.value
        if (err != null) {
            Log.w(TAG, "Could not load proxied apps from daemon: $err")
        }
        throw IllegalStateException(err ?: messages.get(com.rknnovpn.panel.R.string.error_no_profile_loaded))
    }

    private fun observeProfile() {
        viewModelScope.launch {
            profileRepository.profile
                .filterNotNull()
                .collect { config ->
                    val selection = config.toRoutingSelection()
                    _uiState.update { state ->
                        state.copy(
                            routingMode = selection.routingMode,
                            nodeGroups = selection.nodeGroups,
                            apps = state.apps.map { app ->
                                val isAlwaysDirect = isAlwaysDirectApp(
                                    packageName = app.packageName,
                                    isSystemApp = app.isSystemApp,
                                    selection = selection,
                                )
                                app.copy(
                                    isAlwaysDirect = isAlwaysDirect,
                                    isAlwaysDirectExcluded = app.packageName in selection.alwaysDirectExcludedPackages,
                                    nodeGroup = selection.appGroupRoutes[app.packageName].orEmpty(),
                                    isProxied = if (isAlwaysDirect && selection.routingMode == RoutingMode.PER_APP_BYPASS) {
                                        true
                                    } else if (
                                        (selection.routingMode == RoutingMode.PER_APP || selection.routingMode == RoutingMode.RULES) &&
                                        selection.appGroupRoutes.containsKey(app.packageName) &&
                                        !isAlwaysDirect
                                    ) {
                                        true
                                    } else {
                                        app.packageName in selection.selectedPackages
                                    },
                                )
                            },
                        )
                    }
                }
        }
    }

}

private fun AppPickerUiState.selectedPackageListForSave(forceProxyPackage: String? = null): List<String> {
    val forced = forceProxyPackage?.trim()?.takeIf(String::isNotBlank)
    return apps
        .filter { app ->
            app.isProxied ||
                (
                    (routingMode == RoutingMode.PER_APP || routingMode == RoutingMode.RULES) &&
                        app.nodeGroup.isNotBlank() &&
                        !app.isAlwaysDirect
                    )
        }
        .filterNot { app ->
            app.isAlwaysDirect && routingMode == RoutingMode.PER_APP
        }
        .map { it.packageName }
        .let { packages ->
            when {
                forced == null -> packages
                routingMode == RoutingMode.PER_APP || routingMode == RoutingMode.RULES -> packages + forced
                routingMode == RoutingMode.PER_APP_BYPASS -> packages.filterNot { it == forced }
                else -> packages
            }
        }
        .map(String::trim)
        .filter(String::isNotBlank)
        .distinct()
}

private fun AppPickerUiState.appGroupRoutesForSave(): Map<String, String> =
    apps
        .mapNotNull { app ->
            app.nodeGroup.trim().takeIf(String::isNotBlank)?.let { group -> app.packageName to group }
        }
        .toMap()

private fun RoutingMode.usesPerAppSelection(): Boolean =
    this == RoutingMode.PER_APP || this == RoutingMode.PER_APP_BYPASS || this == RoutingMode.RULES

private fun ProfileConfig.toRoutingSelection(): RoutingSelection {
    val routingMode = routing.mode
    val selected = when (routingMode) {
        RoutingMode.PER_APP -> routing.appProxyList.toSet()
        RoutingMode.RULES -> routing.appProxyList.toSet()
        RoutingMode.PER_APP_BYPASS -> routing.appBypassList.toSet()
        else -> emptySet()
    }
    val nodeGroups = nodes.map { it.group.trim() }
        .filter(String::isNotBlank)
        .distinct()
        .sorted()
    return RoutingSelection(
        routingMode,
        selected,
        routing.alwaysDirectAppList.toSet(),
        routing.alwaysDirectExcludedAppList.toSet(),
        routing.alwaysDirectSystemApps,
        routing.appGroupRoutes,
        nodeGroups,
    )
}

private fun isAlwaysDirectApp(
    packageName: String,
    isSystemApp: Boolean,
    selection: RoutingSelection,
): Boolean =
    when {
        packageName in selection.alwaysDirectExcludedPackages -> false
        packageName in selection.alwaysDirectPackages -> true
        selection.alwaysDirectSystemApps && isSystemApp -> true
        else -> AlwaysDirectApps.matches(
            packageName,
            selection.alwaysDirectPackages,
            selection.alwaysDirectExcludedPackages,
        )
    }

enum class AppTemplate {
    BROWSERS,
    SOCIAL,
    STREAMING,
    ALL_EXCEPT_BANKS,
}
