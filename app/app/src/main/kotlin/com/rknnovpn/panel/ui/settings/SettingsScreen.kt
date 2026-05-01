package com.rknnovpn.panel.ui.settings

import android.app.Activity
import android.content.ActivityNotFoundException
import android.content.Intent
import android.net.Uri
import android.widget.Toast
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Apps
import androidx.compose.material.icons.filled.Code
import androidx.compose.material.icons.filled.DarkMode
import androidx.compose.material.icons.filled.Dns
import androidx.compose.material.icons.filled.Info
import androidx.compose.material.icons.filled.Memory
import androidx.compose.material.icons.filled.Shield
import androidx.compose.material.icons.filled.RestartAlt
import androidx.compose.material.icons.filled.Route
import androidx.compose.material.icons.filled.Share
import androidx.compose.material.icons.filled.Tune
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FilledTonalButton
import androidx.compose.material3.Icon
import androidx.compose.material3.ListItem
import androidx.compose.material3.ListItemDefaults
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.SegmentedButton
import androidx.compose.material3.SegmentedButtonDefaults
import androidx.compose.material3.SingleChoiceSegmentedButtonRow
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.core.content.FileProvider
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import com.rknnovpn.panel.R
import com.rknnovpn.panel.model.DnsIpv6Mode
import com.rknnovpn.panel.model.FallbackPolicy
import com.rknnovpn.panel.ui.common.AppPackageListItem
import com.rknnovpn.panel.ui.common.AppPackagePickerDialog
import java.io.File
import java.io.FileOutputStream
import java.util.zip.ZipEntry
import java.util.zip.ZipOutputStream

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun SettingsScreen(
    onNavigateToAudit: () -> Unit = {},
    viewModel: SettingsViewModel = hiltViewModel(),
) {
    val state by viewModel.uiState.collectAsStateWithLifecycle()
    val updateState by viewModel.updateState.collectAsStateWithLifecycle()
    val context = LocalContext.current
    val vpnDetectionUrl = stringResource(R.string.vpn_detection_url)
    val bypassRussiaDisablePhrase = stringResource(R.string.bypass_russia_disable_phrase)
    var showAlwaysDirectAppPicker by remember { mutableStateOf(false) }
    var showAlwaysDirectExcludedAppPicker by remember { mutableStateOf(false) }
    var showBypassRussiaDisableDialog by remember { mutableStateOf(false) }
    var bypassRussiaDisableConfirmation by remember { mutableStateOf("") }
    val alwaysDirectPackages = remember(state.alwaysDirectPackagesText) {
        parsePackageSelection(state.alwaysDirectPackagesText)
    }
    val alwaysDirectExcludedPackages = remember(state.alwaysDirectExcludedPackagesText) {
        parsePackageSelection(state.alwaysDirectExcludedPackagesText)
    }

    LaunchedEffect(state.shareLogsEventId) {
        val logs = state.shareLogsText
        if (state.shareLogsEventId > 0 && logs != null) {
            val zipFile = try {
                createRuntimeLogsZip(context, logs)
            } catch (_: Exception) {
                Toast.makeText(context, R.string.logs_share_no_app, Toast.LENGTH_LONG).show()
                viewModel.clearSharedLogs()
                return@LaunchedEffect
            }
            val uri = FileProvider.getUriForFile(
                context,
                "${context.packageName}.fileprovider",
                zipFile,
            )
            val intent = Intent(Intent.ACTION_SEND).apply {
                type = "application/zip"
                putExtra(Intent.EXTRA_SUBJECT, context.getString(R.string.logs_share_subject))
                putExtra(Intent.EXTRA_STREAM, uri)
                addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION)
            }
            try {
                startTelegramShare(context, intent)
            } catch (_: ActivityNotFoundException) {
                val chooser = Intent.createChooser(intent, context.getString(R.string.logs_share_chooser))
                if (context !is Activity) {
                    chooser.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
                }
                try {
                    context.startActivity(chooser)
                } catch (_: ActivityNotFoundException) {
                    Toast.makeText(context, R.string.logs_share_no_app, Toast.LENGTH_LONG).show()
                }
            }
            viewModel.clearSharedLogs()
        }
    }

    Column(
        modifier = Modifier
            .fillMaxSize()
            .verticalScroll(rememberScrollState())
            .padding(horizontal = 16.dp, vertical = 8.dp),
    ) {
        if (state.errorMessage != null) {
            SettingsCard {
                ListItem(
                    headlineContent = {
                        Text(
                            text = state.errorMessage ?: "",
                            color = MaterialTheme.colorScheme.error,
                        )
                    },
                    colors = transparentListItemColors(),
                )
            }
            Spacer(modifier = Modifier.height(12.dp))
        }

        // ===== ROUTING =====
        SectionHeader(
            title = stringResource(R.string.settings_routing),
            icon = { Icon(Icons.Filled.Route, contentDescription = null) },
        )

        RoutingModeSelector(
            currentMode = state.routingMode,
            onModeChange = viewModel::setRoutingMode,
        )

        SettingsCard {
            ListItem(
                headlineContent = { Text(stringResource(R.string.bypass_russia)) },
                supportingContent = { Text(stringResource(R.string.bypass_russia_desc)) },
                trailingContent = {
                    Switch(
                        checked = state.bypassRussia,
                        onCheckedChange = { enabled ->
                            if (enabled) {
                                viewModel.setBypassRussia(true)
                            } else {
                                bypassRussiaDisableConfirmation = ""
                                showBypassRussiaDisableDialog = true
                            }
                        },
                    )
                },
                colors = transparentListItemColors(),
            )
        }

        RoutingRulesCard(
            directDomains = state.directDomainsText,
            proxyDomains = state.proxyDomainsText,
            blockDomains = state.blockDomainsText,
            directIps = state.directIpsText,
            proxyIps = state.proxyIpsText,
            blockIps = state.blockIpsText,
            onDirectDomainsChange = viewModel::setDirectDomainsText,
            onProxyDomainsChange = viewModel::setProxyDomainsText,
            onBlockDomainsChange = viewModel::setBlockDomainsText,
            onDirectIpsChange = viewModel::setDirectIpsText,
            onProxyIpsChange = viewModel::setProxyIpsText,
            onBlockIpsChange = viewModel::setBlockIpsText,
            onApply = viewModel::applyRoutingRules,
        )

        Spacer(modifier = Modifier.height(20.dp))

        // ===== DNS =====
        SectionHeader(
            title = stringResource(R.string.settings_dns),
            icon = { Icon(Icons.Filled.Dns, contentDescription = null) },
        )

        DnsSettingsCard(
            currentPreset = state.dnsPreset,
            remoteDnsUrl = state.remoteDnsUrl,
            directDnsUrl = state.directDnsUrl,
            bootstrapDnsIp = state.bootstrapDnsIp,
            ipv6Mode = state.dnsIpv6Mode,
            blockQuic = state.blockQuicDns,
            fakeDns = state.fakeDns,
            onPresetChange = viewModel::setDnsPreset,
            onRemoteDnsChange = viewModel::setRemoteDnsUrl,
            onDirectDnsChange = viewModel::setDirectDnsUrl,
            onBootstrapChange = viewModel::setBootstrapDnsIp,
            onIpv6ModeChange = viewModel::setDnsIpv6Mode,
            onBlockQuicChange = viewModel::setBlockQuicDns,
            onFakeDnsChange = viewModel::setFakeDns,
            onApply = viewModel::applyDnsSettings,
        )

        Spacer(modifier = Modifier.height(20.dp))

        // ===== RECOVERY =====
        SectionHeader(
            title = stringResource(R.string.settings_recovery),
            icon = { Icon(Icons.Filled.Memory, contentDescription = null) },
        )

        SettingsCard {
            FallbackPolicyPicker(
                currentPolicy = state.fallbackPolicy,
                onPolicyChange = viewModel::setFallbackPolicy,
            )
        }

        Spacer(modifier = Modifier.height(20.dp))

        // ===== ADVANCED =====
        SectionHeader(
            title = stringResource(R.string.settings_advanced),
            icon = { Icon(Icons.Filled.Tune, contentDescription = null) },
        )

        SettingsCard {
            // Log level picker
            LogLevelPicker(
                currentLevel = state.logLevel,
                onLevelChange = viewModel::setLogLevel,
            )

            Spacer(modifier = Modifier.height(8.dp))

            OutlinedTextField(
                value = state.urlTestUrl,
                onValueChange = viewModel::setUrlTestUrl,
                label = { Text(stringResource(R.string.urltest_url)) },
                singleLine = true,
                modifier = Modifier
                    .fillMaxWidth()
                    .padding(horizontal = 16.dp),
            )

            TextButton(
                onClick = viewModel::applyUrlTestUrl,
                modifier = Modifier.padding(horizontal = 8.dp),
            ) {
                Text(stringResource(R.string.apply))
            }

            Column(
                verticalArrangement = Arrangement.spacedBy(8.dp),
                modifier = Modifier
                    .fillMaxWidth()
                    .padding(horizontal = 16.dp),
            ) {
                Text(
                    text = stringResource(R.string.always_direct_apps),
                    style = MaterialTheme.typography.titleSmall,
                )
                Text(
                    text = stringResource(R.string.always_direct_apps_desc),
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
                ListItem(
                    headlineContent = { Text(stringResource(R.string.always_direct_system_apps)) },
                    supportingContent = { Text(stringResource(R.string.always_direct_system_apps_desc)) },
                    trailingContent = {
                        Switch(
                            checked = state.alwaysDirectSystemApps,
                            onCheckedChange = viewModel::setAlwaysDirectSystemApps,
                        )
                    },
                    colors = transparentListItemColors(),
                )
                if (alwaysDirectPackages.isEmpty()) {
                    Text(
                        text = stringResource(R.string.no_apps_selected),
                        style = MaterialTheme.typography.bodyMedium,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                } else {
                    Column {
                        alwaysDirectPackages.forEach { packageName ->
                            AppPackageListItem(
                                packageName = packageName,
                                onRemove = { viewModel.removeAlwaysDirectPackage(packageName) },
                            )
                        }
                    }
                }
                TextButton(
                    onClick = { showAlwaysDirectAppPicker = true },
                    modifier = Modifier.align(Alignment.Start),
                ) {
                    Icon(Icons.Filled.Apps, contentDescription = null)
                    Spacer(modifier = Modifier.width(8.dp))
                    Text(stringResource(R.string.add_app))
                }
                Text(
                    text = stringResource(R.string.always_direct_excluded_apps),
                    style = MaterialTheme.typography.titleSmall,
                )
                Text(
                    text = stringResource(R.string.always_direct_excluded_apps_desc),
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
                if (alwaysDirectExcludedPackages.isEmpty()) {
                    Text(
                        text = stringResource(R.string.no_apps_selected),
                        style = MaterialTheme.typography.bodyMedium,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                } else {
                    Column {
                        alwaysDirectExcludedPackages.forEach { packageName ->
                            AppPackageListItem(
                                packageName = packageName,
                                onRemove = { viewModel.removeAlwaysDirectExcludedPackage(packageName) },
                            )
                        }
                    }
                }
                TextButton(
                    onClick = { showAlwaysDirectExcludedAppPicker = true },
                    modifier = Modifier.align(Alignment.Start),
                ) {
                    Icon(Icons.Filled.Apps, contentDescription = null)
                    Spacer(modifier = Modifier.width(8.dp))
                    Text(stringResource(R.string.add_app))
                }
            }

            TextButton(
                onClick = viewModel::applyAlwaysDirectPackages,
                modifier = Modifier.padding(horizontal = 8.dp),
            ) {
                Text(stringResource(R.string.apply))
            }

            ListItem(
                headlineContent = { Text(stringResource(R.string.sharing_mode)) },
                supportingContent = { Text(stringResource(R.string.sharing_mode_desc)) },
                trailingContent = {
                    Switch(
                        checked = state.sharingEnabled,
                        onCheckedChange = viewModel::setSharingEnabled,
                    )
                },
                colors = transparentListItemColors(),
            )

            if (state.sharingEnabled) {
                OutlinedTextField(
                    value = state.sharingInterfacesText,
                    onValueChange = viewModel::setSharingInterfacesText,
                    label = { Text(stringResource(R.string.sharing_interfaces)) },
                    supportingText = { Text(stringResource(R.string.sharing_interfaces_desc)) },
                    minLines = 2,
                    maxLines = 4,
                    modifier = Modifier
                        .fillMaxWidth()
                        .padding(horizontal = 16.dp),
                )

                TextButton(
                    onClick = viewModel::applySharingInterfaces,
                    modifier = Modifier.padding(horizontal = 8.dp),
                ) {
                    Text(stringResource(R.string.apply))
                }
            }
        }

        Spacer(modifier = Modifier.height(20.dp))

        // ===== MODULE =====
        SectionHeader(
            title = stringResource(R.string.settings_module),
            icon = { Icon(Icons.Filled.Memory, contentDescription = null) },
        )

        SettingsCard {
            ListItem(
                headlineContent = { Text(stringResource(R.string.module_version)) },
                supportingContent = { Text(state.moduleVersion) },
                colors = transparentListItemColors(),
            )

            ListItem(
                headlineContent = { Text(stringResource(R.string.daemon_status)) },
                supportingContent = { Text(state.daemonStatusText) },
                colors = transparentListItemColors(),
            )

            ListItem(
                headlineContent = {
                    FilledTonalButton(
                        onClick = viewModel::restartDaemon,
                        enabled = !state.runtimeActionActive,
                    ) {
                        Icon(
                            Icons.Filled.RestartAlt,
                            contentDescription = null,
                            modifier = Modifier.padding(end = 4.dp),
                        )
                        Text(stringResource(R.string.restart_daemon))
                    }
                },
                colors = transparentListItemColors(),
            )

            ListItem(
                headlineContent = {
                    FilledTonalButton(
                        onClick = viewModel::resetNetworkRules,
                        enabled = !state.runtimeActionActive && !state.isResetting,
                        colors = ButtonDefaults.filledTonalButtonColors(
                            containerColor = MaterialTheme.colorScheme.errorContainer,
                            contentColor = MaterialTheme.colorScheme.onErrorContainer,
                        ),
                    ) {
                        Icon(
                            Icons.Filled.RestartAlt,
                            contentDescription = null,
                            modifier = Modifier.padding(end = 4.dp),
                        )
                        Text(stringResource(R.string.reset_network_rules))
                    }
                },
                supportingContent = {
                    Text(
                        state.lastResetSummary ?: stringResource(R.string.reset_network_rules_desc)
                    )
                },
                colors = transparentListItemColors(),
            )

            ListItem(
                headlineContent = { Text(stringResource(R.string.runtime_logs)) },
                supportingContent = {
                    Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                        Text(stringResource(R.string.runtime_logs_desc))
                        Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                            FilledTonalButton(
                                onClick = viewModel::refreshRuntimeLogs,
                                enabled = !state.isLoadingLogs,
                                modifier = Modifier.fillMaxWidth(),
                            ) {
                                Icon(
                                    Icons.Filled.RestartAlt,
                                    contentDescription = null,
                                    modifier = Modifier.padding(end = 4.dp),
                                )
                                Text(
                                    if (state.isLoadingLogs) {
                                        stringResource(R.string.loading_logs)
                                    } else {
                                        stringResource(R.string.show_logs)
                                    }
                                )
                            }
                            FilledTonalButton(
                                onClick = viewModel::shareRuntimeLogs,
                                enabled = !state.isLoadingLogs,
                                modifier = Modifier.fillMaxWidth(),
                            ) {
                                Icon(
                                    Icons.Filled.Share,
                                    contentDescription = null,
                                    modifier = Modifier.padding(end = 4.dp),
                                )
                                Text(stringResource(R.string.share_logs_telegram))
                            }
                        }
                        if (state.logsText.isNotBlank()) {
                            SelectionContainer {
                                Text(
                                    text = state.logsText,
                                    style = MaterialTheme.typography.bodySmall,
                                    fontFamily = FontFamily.Monospace,
                                    modifier = Modifier
                                        .fillMaxWidth()
                                        .heightIn(max = 260.dp)
                                        .verticalScroll(rememberScrollState()),
                                )
                            }
                        }
                    }
                },
                colors = transparentListItemColors(),
            )

            ListItem(
                headlineContent = {
                    FilledTonalButton(onClick = onNavigateToAudit) {
                        Icon(
                            Icons.Filled.Shield,
                            contentDescription = null,
                            modifier = Modifier.padding(end = 4.dp),
                        )
                        Text(stringResource(R.string.settings_audit))
                    }
                },
                colors = transparentListItemColors(),
            )
        }

        Spacer(modifier = Modifier.height(20.dp))

        // ===== UPDATE =====
        UpdateSection(
            state = updateState,
            runtimeMutationsEnabled = !state.runtimeActionActive,
            onCheckForUpdates = viewModel::checkForUpdates,
            onDownloadUpdate = viewModel::downloadUpdate,
            onInstallUpdate = viewModel::installUpdate,
        )

        Spacer(modifier = Modifier.height(20.dp))

        // ===== THEME =====
        SectionHeader(
            title = stringResource(R.string.settings_theme),
            icon = { Icon(Icons.Filled.DarkMode, contentDescription = null) },
        )

        ThemeModeSelector(
            currentMode = state.themeMode,
            onModeChange = viewModel::setThemeMode,
        )

        Spacer(modifier = Modifier.height(20.dp))

        // ===== ABOUT =====
        SectionHeader(
            title = stringResource(R.string.settings_about),
            icon = { Icon(Icons.Filled.Info, contentDescription = null) },
        )

        SettingsCard {
            ListItem(
                headlineContent = { Text(stringResource(R.string.about_version)) },
                supportingContent = { Text(state.appVersion) },
                colors = transparentListItemColors(),
            )

            ListItem(
                headlineContent = { Text(stringResource(R.string.about_github)) },
                supportingContent = { Text(state.githubUrl) },
                leadingContent = {
                    Icon(Icons.Filled.Code, contentDescription = null)
                },
                modifier = Modifier.clickable {
                    val intent = Intent(Intent.ACTION_VIEW, Uri.parse(state.githubUrl))
                    if (context !is Activity) {
                        intent.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
                    }
                    try {
                        context.startActivity(intent)
                    } catch (_: ActivityNotFoundException) {
                        Toast.makeText(context, R.string.link_open_no_app, Toast.LENGTH_LONG).show()
                    }
                },
                colors = transparentListItemColors(),
            )
        }

        Spacer(modifier = Modifier.height(24.dp))
    }
    if (showAlwaysDirectAppPicker) {
        AppPackagePickerDialog(
            title = stringResource(R.string.choose_app),
            warningText = stringResource(R.string.always_direct_picker_warning),
            onDismiss = { showAlwaysDirectAppPicker = false },
            onSelect = viewModel::addAlwaysDirectPackage,
        )
    }

    if (showAlwaysDirectExcludedAppPicker) {
        AppPackagePickerDialog(
            title = stringResource(R.string.choose_app),
            warningText = stringResource(R.string.always_direct_excluded_picker_warning),
            onDismiss = { showAlwaysDirectExcludedAppPicker = false },
            onSelect = viewModel::addAlwaysDirectExcludedPackage,
        )
    }

    if (showBypassRussiaDisableDialog) {
        AlertDialog(
            onDismissRequest = {
                showBypassRussiaDisableDialog = false
                bypassRussiaDisableConfirmation = ""
            },
            title = { Text(stringResource(R.string.bypass_russia_disable_title)) },
            text = {
                Column(verticalArrangement = Arrangement.spacedBy(12.dp)) {
                    Text(stringResource(R.string.bypass_russia_disable_warning))
                    TextButton(
                        onClick = {
                            val intent = Intent(Intent.ACTION_VIEW, Uri.parse(vpnDetectionUrl))
                            if (context !is Activity) {
                                intent.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
                            }
                            try {
                                context.startActivity(intent)
                            } catch (_: ActivityNotFoundException) {
                                Toast.makeText(context, R.string.link_open_no_app, Toast.LENGTH_LONG).show()
                            }
                        },
                    ) {
                        Text(vpnDetectionUrl)
                    }
                    OutlinedTextField(
                        value = bypassRussiaDisableConfirmation,
                        onValueChange = { bypassRussiaDisableConfirmation = it },
                        label = { Text(stringResource(R.string.bypass_russia_disable_input_label)) },
                        singleLine = true,
                        modifier = Modifier.fillMaxWidth(),
                    )
                }
            },
            confirmButton = {
                TextButton(
                    enabled = bypassRussiaDisableConfirmation.trim() == bypassRussiaDisablePhrase,
                    onClick = {
                        showBypassRussiaDisableDialog = false
                        bypassRussiaDisableConfirmation = ""
                        viewModel.setBypassRussia(false)
                    },
                ) {
                    Text(stringResource(R.string.disable))
                }
            },
            dismissButton = {
                TextButton(
                    onClick = {
                        showBypassRussiaDisableDialog = false
                        bypassRussiaDisableConfirmation = ""
                    },
                ) {
                    Text(stringResource(R.string.cancel))
                }
            },
        )
    }
}

private fun createRuntimeLogsZip(context: android.content.Context, logs: String): File {
    val dir = File(context.cacheDir, "shared_logs")
    dir.mkdirs()
    dir.listFiles()?.forEach { file ->
        if (file.isFile) file.delete()
    }
    val zipFile = File(dir, "rknnovpn-logs-${System.currentTimeMillis()}.zip")
    ZipOutputStream(FileOutputStream(zipFile)).use { zip ->
        zip.putNextEntry(ZipEntry("diagnostic-report.txt"))
        zip.write(logs.toByteArray(Charsets.UTF_8))
        zip.closeEntry()
    }
    return zipFile
}

private fun startTelegramShare(context: android.content.Context, baseIntent: Intent) {
    val packages = listOf("org.telegram.messenger", "org.telegram.messenger.web")
    var lastFailure: ActivityNotFoundException? = null
    for (packageName in packages) {
        val telegramIntent = Intent(baseIntent).setPackage(packageName)
        if (context !is Activity) {
            telegramIntent.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
        }
        try {
            context.startActivity(telegramIntent)
            return
        } catch (e: ActivityNotFoundException) {
            lastFailure = e
        }
    }
    throw lastFailure ?: ActivityNotFoundException()
}

// ---- Section helper composables ----

private fun parsePackageSelection(raw: String): List<String> =
    raw.split('\n', ',', ';', ' ', '\t')
        .map(String::trim)
        .filter(String::isNotBlank)
        .distinct()

@Composable
private fun SectionHeader(
    title: String,
    icon: @Composable () -> Unit,
) {
    ListItem(
        headlineContent = {
            Text(
                text = title,
                style = MaterialTheme.typography.titleSmall,
                color = MaterialTheme.colorScheme.primary,
            )
        },
        leadingContent = icon,
        colors = ListItemDefaults.colors(containerColor = Color.Transparent),
    )
}

@Composable
private fun SettingsCard(
    content: @Composable () -> Unit,
) {
    Card(
        modifier = Modifier.fillMaxWidth(),
        colors = CardDefaults.cardColors(
            containerColor = MaterialTheme.colorScheme.surfaceVariant.copy(alpha = 0.5f),
        ),
    ) {
        Column {
            content()
        }
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun RoutingModeSelector(
    currentMode: RoutingMode,
    onModeChange: (RoutingMode) -> Unit,
) {
    val options = listOf(
        RoutingMode.GLOBAL to stringResource(R.string.routing_global),
        RoutingMode.WHITELIST to stringResource(R.string.routing_whitelist),
        RoutingMode.BYPASS to stringResource(R.string.routing_bypass),
        RoutingMode.RULES to stringResource(R.string.routing_rules),
        RoutingMode.DIRECT to stringResource(R.string.routing_direct),
    )

    SingleChoiceSegmentedButtonRow(modifier = Modifier.fillMaxWidth()) {
        options.forEachIndexed { index, (mode, label) ->
            SegmentedButton(
                selected = currentMode == mode,
                onClick = { onModeChange(mode) },
                shape = SegmentedButtonDefaults.itemShape(
                    index = index,
                    count = options.size,
                ),
            ) {
                Text(label)
            }
        }
    }
}

@Composable
private fun RoutingRulesCard(
    directDomains: String,
    proxyDomains: String,
    blockDomains: String,
    directIps: String,
    proxyIps: String,
    blockIps: String,
    onDirectDomainsChange: (String) -> Unit,
    onProxyDomainsChange: (String) -> Unit,
    onBlockDomainsChange: (String) -> Unit,
    onDirectIpsChange: (String) -> Unit,
    onProxyIpsChange: (String) -> Unit,
    onBlockIpsChange: (String) -> Unit,
    onApply: () -> Unit,
) {
    SettingsCard {
        Column(
            verticalArrangement = Arrangement.spacedBy(10.dp),
            modifier = Modifier
                .fillMaxWidth()
                .padding(horizontal = 16.dp, vertical = 12.dp),
        ) {
            Text(
                text = stringResource(R.string.routing_rule_editor),
                style = MaterialTheme.typography.titleSmall,
            )
            Text(
                text = stringResource(R.string.routing_rule_editor_desc),
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            RoutingRuleTextField(
                value = directDomains,
                onValueChange = onDirectDomainsChange,
                label = stringResource(R.string.routing_direct_domains),
                hint = stringResource(R.string.routing_domain_hint),
            )
            RoutingRuleTextField(
                value = proxyDomains,
                onValueChange = onProxyDomainsChange,
                label = stringResource(R.string.routing_proxy_domains),
                hint = stringResource(R.string.routing_domain_hint),
            )
            RoutingRuleTextField(
                value = blockDomains,
                onValueChange = onBlockDomainsChange,
                label = stringResource(R.string.routing_block_domains),
                hint = stringResource(R.string.routing_domain_hint),
            )
            RoutingRuleTextField(
                value = directIps,
                onValueChange = onDirectIpsChange,
                label = stringResource(R.string.routing_direct_ips),
                hint = stringResource(R.string.routing_ip_hint),
            )
            RoutingRuleTextField(
                value = proxyIps,
                onValueChange = onProxyIpsChange,
                label = stringResource(R.string.routing_proxy_ips),
                hint = stringResource(R.string.routing_ip_hint),
            )
            RoutingRuleTextField(
                value = blockIps,
                onValueChange = onBlockIpsChange,
                label = stringResource(R.string.routing_block_ips),
                hint = stringResource(R.string.routing_ip_hint),
            )
            TextButton(
                onClick = onApply,
                modifier = Modifier.align(Alignment.Start),
            ) {
                Text(stringResource(R.string.apply))
            }
        }
    }
}

@Composable
private fun RoutingRuleTextField(
    value: String,
    onValueChange: (String) -> Unit,
    label: String,
    hint: String,
) {
    OutlinedTextField(
        value = value,
        onValueChange = onValueChange,
        label = { Text(label) },
        supportingText = { Text(hint) },
        minLines = 2,
        maxLines = 4,
        modifier = Modifier.fillMaxWidth(),
    )
}

@Composable
private fun DnsSettingsCard(
    currentPreset: DnsPreset,
    remoteDnsUrl: String,
    directDnsUrl: String,
    bootstrapDnsIp: String,
    ipv6Mode: DnsIpv6Mode,
    blockQuic: Boolean,
    fakeDns: Boolean,
    onPresetChange: (DnsPreset) -> Unit,
    onRemoteDnsChange: (String) -> Unit,
    onDirectDnsChange: (String) -> Unit,
    onBootstrapChange: (String) -> Unit,
    onIpv6ModeChange: (DnsIpv6Mode) -> Unit,
    onBlockQuicChange: (Boolean) -> Unit,
    onFakeDnsChange: (Boolean) -> Unit,
    onApply: () -> Unit,
) {
    SettingsCard {
        DnsPresetPicker(
            currentPreset = currentPreset,
            onPresetChange = onPresetChange,
        )

        OutlinedTextField(
            value = remoteDnsUrl,
            onValueChange = onRemoteDnsChange,
            label = { Text(stringResource(R.string.dns_remote_url)) },
            singleLine = true,
            modifier = Modifier
                .fillMaxWidth()
                .padding(horizontal = 16.dp),
        )

        Spacer(modifier = Modifier.height(8.dp))

        OutlinedTextField(
            value = directDnsUrl,
            onValueChange = onDirectDnsChange,
            label = { Text(stringResource(R.string.dns_direct_url)) },
            singleLine = true,
            modifier = Modifier
                .fillMaxWidth()
                .padding(horizontal = 16.dp),
        )

        Spacer(modifier = Modifier.height(8.dp))

        OutlinedTextField(
            value = bootstrapDnsIp,
            onValueChange = onBootstrapChange,
            label = { Text(stringResource(R.string.dns_bootstrap_ip)) },
            singleLine = true,
            modifier = Modifier
                .fillMaxWidth()
                .padding(horizontal = 16.dp),
        )

        DnsIpv6ModePicker(
            currentMode = ipv6Mode,
            onModeChange = onIpv6ModeChange,
        )

        ListItem(
            headlineContent = { Text(stringResource(R.string.dns_block_quic)) },
            supportingContent = { Text(stringResource(R.string.dns_block_quic_desc)) },
            trailingContent = {
                Switch(
                    checked = blockQuic,
                    onCheckedChange = onBlockQuicChange,
                )
            },
            colors = transparentListItemColors(),
        )

        ListItem(
            headlineContent = { Text(stringResource(R.string.dns_fake_dns)) },
            supportingContent = { Text(stringResource(R.string.dns_fake_dns_desc)) },
            trailingContent = {
                Switch(
                    checked = fakeDns,
                    onCheckedChange = onFakeDnsChange,
                )
            },
            colors = transparentListItemColors(),
        )

        TextButton(
            onClick = onApply,
            modifier = Modifier.padding(horizontal = 8.dp),
        ) {
            Text(stringResource(R.string.apply))
        }
    }
}

@Composable
private fun DnsPresetPicker(
    currentPreset: DnsPreset,
    onPresetChange: (DnsPreset) -> Unit,
) {
    var expanded by remember { mutableStateOf(false) }
    val currentLabel = dnsPresetLabel(currentPreset)

    ListItem(
        headlineContent = { Text(stringResource(R.string.dns_provider)) },
        supportingContent = { Text(currentLabel) },
        trailingContent = {
            TextButton(onClick = { expanded = true }) {
                Text(currentLabel)
            }
            DropdownMenu(
                expanded = expanded,
                onDismissRequest = { expanded = false },
            ) {
                DnsPreset.entries.forEach { preset ->
                    DropdownMenuItem(
                        text = { Text(dnsPresetLabel(preset)) },
                        onClick = {
                            onPresetChange(preset)
                            expanded = false
                        },
                    )
                }
            }
        },
        colors = transparentListItemColors(),
    )
}

@Composable
private fun DnsIpv6ModePicker(
    currentMode: DnsIpv6Mode,
    onModeChange: (DnsIpv6Mode) -> Unit,
) {
    var expanded by remember { mutableStateOf(false) }
    val currentLabel = dnsIpv6ModeLabel(currentMode)

    ListItem(
        headlineContent = { Text(stringResource(R.string.dns_ipv6_mode)) },
        supportingContent = { Text(currentLabel) },
        trailingContent = {
            TextButton(onClick = { expanded = true }) {
                Text(currentLabel)
            }
            DropdownMenu(
                expanded = expanded,
                onDismissRequest = { expanded = false },
            ) {
                DnsIpv6Mode.entries.forEach { mode ->
                    DropdownMenuItem(
                        text = { Text(dnsIpv6ModeLabel(mode)) },
                        onClick = {
                            onModeChange(mode)
                            expanded = false
                        },
                    )
                }
            }
        },
        colors = transparentListItemColors(),
    )
}

@Composable
private fun LogLevelPicker(
    currentLevel: LogLevel,
    onLevelChange: (LogLevel) -> Unit,
) {
    var expanded by remember { mutableStateOf(false) }
    val currentLabel = logLevelLabel(currentLevel)

    ListItem(
        headlineContent = { Text(stringResource(R.string.log_level)) },
        supportingContent = { Text(currentLabel) },
        trailingContent = {
            TextButton(onClick = { expanded = true }) {
                Text(currentLabel)
            }
            DropdownMenu(
                expanded = expanded,
                onDismissRequest = { expanded = false },
            ) {
                LogLevel.entries.forEach { level ->
                    DropdownMenuItem(
                        text = { Text(logLevelLabel(level)) },
                        onClick = {
                            onLevelChange(level)
                            expanded = false
                        },
                    )
                }
            }
        },
        colors = transparentListItemColors(),
    )
}

@Composable
private fun FallbackPolicyPicker(
    currentPolicy: FallbackPolicy,
    onPolicyChange: (FallbackPolicy) -> Unit,
) {
    var expanded by remember { mutableStateOf(false) }

    val currentLabel = when (currentPolicy) {
        FallbackPolicy.OFFER_RESET -> stringResource(R.string.fallback_offer_reset)
        FallbackPolicy.STAY_ON_SELECTED -> stringResource(R.string.fallback_stay_selected)
        FallbackPolicy.AUTO_RESET_ROOTED -> stringResource(R.string.fallback_auto_reset)
    }

    ListItem(
        headlineContent = { Text(stringResource(R.string.backend_fallback_policy)) },
        supportingContent = { Text(currentLabel) },
        trailingContent = {
            TextButton(onClick = { expanded = true }) {
                Text(currentLabel)
            }
            DropdownMenu(
                expanded = expanded,
                onDismissRequest = { expanded = false },
            ) {
                FallbackPolicy.entries.forEach { policy ->
                    val label = when (policy) {
                        FallbackPolicy.OFFER_RESET -> stringResource(R.string.fallback_offer_reset)
                        FallbackPolicy.STAY_ON_SELECTED -> stringResource(R.string.fallback_stay_selected)
                        FallbackPolicy.AUTO_RESET_ROOTED -> stringResource(R.string.fallback_auto_reset)
                    }
                    DropdownMenuItem(
                        text = { Text(label) },
                        onClick = {
                            onPolicyChange(policy)
                            expanded = false
                        },
                    )
                }
            }
        },
        colors = transparentListItemColors(),
    )
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun ThemeModeSelector(
    currentMode: ThemeMode,
    onModeChange: (ThemeMode) -> Unit,
) {
    val options = listOf(
        ThemeMode.LIGHT to stringResource(R.string.theme_light),
        ThemeMode.DARK to stringResource(R.string.theme_dark),
        ThemeMode.SYSTEM to stringResource(R.string.theme_system),
    )

    SingleChoiceSegmentedButtonRow(modifier = Modifier.fillMaxWidth()) {
        options.forEachIndexed { index, (mode, label) ->
            SegmentedButton(
                selected = currentMode == mode,
                onClick = { onModeChange(mode) },
                shape = SegmentedButtonDefaults.itemShape(
                    index = index,
                    count = options.size,
                ),
            ) {
                Text(label)
            }
        }
    }
}

@Composable
private fun transparentListItemColors() = ListItemDefaults.colors(
    containerColor = Color.Transparent,
)

@Composable
private fun logLevelLabel(level: LogLevel): String = when (level) {
    LogLevel.DEBUG -> stringResource(R.string.log_level_debug)
    LogLevel.INFO -> stringResource(R.string.log_level_info)
    LogLevel.WARNING -> stringResource(R.string.log_level_warning)
    LogLevel.ERROR -> stringResource(R.string.log_level_error)
    LogLevel.NONE -> stringResource(R.string.log_level_none)
}

@Composable
private fun dnsPresetLabel(preset: DnsPreset): String = when (preset) {
    DnsPreset.CLOUDFLARE -> stringResource(R.string.dns_cloudflare)
    DnsPreset.GOOGLE -> stringResource(R.string.dns_google)
    DnsPreset.MULLVAD -> stringResource(R.string.dns_mullvad)
    DnsPreset.ADGUARD -> stringResource(R.string.dns_adguard)
    DnsPreset.CUSTOM -> stringResource(R.string.dns_custom)
}

@Composable
private fun dnsIpv6ModeLabel(mode: DnsIpv6Mode): String = when (mode) {
    DnsIpv6Mode.MIRROR -> stringResource(R.string.dns_ipv6_mirror)
    DnsIpv6Mode.PREFER_IPV4 -> stringResource(R.string.dns_ipv6_prefer_ipv4)
    DnsIpv6Mode.PREFER_IPV6 -> stringResource(R.string.dns_ipv6_prefer_ipv6)
    DnsIpv6Mode.IPV4_ONLY -> stringResource(R.string.dns_ipv6_ipv4_only)
}
