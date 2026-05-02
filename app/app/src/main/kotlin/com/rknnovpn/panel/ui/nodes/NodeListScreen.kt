package com.rknnovpn.panel.ui.nodes

import androidx.compose.animation.animateColorAsState
import androidx.compose.animation.core.tween
import androidx.compose.foundation.clickable
import androidx.compose.foundation.ExperimentalFoundationApi
import androidx.compose.foundation.combinedClickable
import androidx.compose.foundation.gestures.detectDragGesturesAfterLongPress
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Add
import androidx.compose.material.icons.filled.Check
import androidx.compose.material.icons.filled.ContentCopy
import androidx.compose.material.icons.filled.Delete
import androidx.compose.material.icons.filled.DragHandle
import androidx.compose.material.icons.filled.Edit
import androidx.compose.material.icons.filled.KeyboardArrowDown
import androidx.compose.material.icons.filled.KeyboardArrowRight
import androidx.compose.material.icons.filled.Speed
import androidx.compose.material.icons.filled.Tune
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FloatingActionButton
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Scaffold
import androidx.compose.material3.ScrollableTabRow
import androidx.compose.material3.SegmentedButton
import androidx.compose.material3.SegmentedButtonDefaults
import androidx.compose.material3.SingleChoiceSegmentedButtonRow
import androidx.compose.material3.Surface
import androidx.compose.material3.Tab
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateMapOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.platform.LocalClipboardManager
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.platform.LocalLifecycleOwner
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.DpOffset
import androidx.compose.ui.unit.dp
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.LifecycleEventObserver
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import com.rknnovpn.panel.R
import com.rknnovpn.panel.`import`.ClipboardWatcher
import com.rknnovpn.panel.model.Node
import com.rknnovpn.panel.model.NodeSourceType
import com.rknnovpn.panel.model.Protocol
import com.rknnovpn.panel.ui.common.AppPackagePickerDialog
import com.rknnovpn.panel.ui.common.AppPackageSelector
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.contentOrNull
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun NodeListScreen(
    viewModel: NodeListViewModel = hiltViewModel(),
) {
    val state by viewModel.uiState.collectAsStateWithLifecycle()
    val context = LocalContext.current
    val lifecycleOwner = LocalLifecycleOwner.current
    var nodeToDelete by remember { mutableStateOf<Node?>(null) }
    var nodeToEdit by remember { mutableStateOf<Node?>(null) }

    fun checkClipboardForImport() {
        ClipboardWatcher.check(context)?.let(viewModel::showClipboardImport)
    }

    LaunchedEffect(context) {
        checkClipboardForImport()
    }

    DisposableEffect(lifecycleOwner, context) {
        val observer = LifecycleEventObserver { _, event ->
            if (event == Lifecycle.Event.ON_RESUME) {
                checkClipboardForImport()
            }
        }
        lifecycleOwner.lifecycle.addObserver(observer)
        onDispose {
            lifecycleOwner.lifecycle.removeObserver(observer)
        }
    }

    Scaffold(
        floatingActionButton = {
            FloatingActionButton(onClick = viewModel::showImportSheet) {
                Icon(Icons.Filled.Add, contentDescription = stringResource(R.string.add_node))
            }
        },
    ) { innerPadding ->
        Column(
            modifier = Modifier
                .fillMaxSize()
                .padding(innerPadding),
        ) {
            if (state.groups.size > 1) {
                ScrollableTabRow(
                    selectedTabIndex = state.groups.indexOf(state.selectedGroup).coerceAtLeast(0),
                    edgePadding = 16.dp,
                ) {
                    state.groups.forEach { group ->
                        Tab(
                            selected = group == state.selectedGroup,
                            onClick = { viewModel.selectGroup(group) },
                            text = { Text(group) },
                        )
                    }
                }
            }

            val bannerMessage = state.errorMessage ?: state.profileBannerMessage ?: state.statusMessage
            if (!bannerMessage.isNullOrBlank()) {
                StatusBanner(
                    text = bannerMessage,
                    isError = state.errorMessage != null || state.profileBannerIsError,
                    modifier = Modifier.padding(horizontal = 16.dp, vertical = 8.dp),
                )
            }

            val filteredNodes = state.nodes.filter { it.group == state.selectedGroup }
            val selectableNodes = state.nodes.filterNot { it.stale }
            val selectableFilteredNodes = filteredNodes.filterNot { it.stale }
            val sections = buildNodeSections(
                nodes = filteredNodes,
                subscriptions = state.subscriptions,
            )
            val expandedSections = remember { mutableStateMapOf<String, Boolean>() }
            SelectionModeRow(
                isAuto = state.activeNodeId.isNullOrBlank(),
                hasNodes = selectableNodes.isNotEmpty(),
                onAutoSelect = viewModel::selectAuto,
                onManualSelect = {
                    (selectableFilteredNodes.firstOrNull() ?: selectableNodes.firstOrNull())
                        ?.let { viewModel.selectNode(it.id) }
                },
                modifier = Modifier.padding(horizontal = 16.dp),
            )
            Row(
                horizontalArrangement = Arrangement.SpaceBetween,
                verticalAlignment = Alignment.CenterVertically,
                modifier = Modifier
                    .fillMaxWidth()
                    .padding(horizontal = 16.dp, vertical = 4.dp),
            ) {
                SortMenuButton(
                    currentSort = state.sortMode,
                    onSortChange = viewModel::setSortMode,
                )
                TextButton(
                    onClick = viewModel::testAllNodes,
                    enabled = selectableNodes.isNotEmpty() && !state.isTestingNodes,
                ) {
                    Icon(Icons.Filled.Speed, contentDescription = null)
                    Spacer(modifier = Modifier.width(8.dp))
                    Text(
                        text = stringResource(
                            if (state.isTestingNodes) R.string.testing_nodes else R.string.test_all_nodes
                        ),
                    )
                }
            }

            if (filteredNodes.isEmpty()) {
                Box(
                    contentAlignment = Alignment.Center,
                    modifier = Modifier
                        .fillMaxSize()
                        .padding(32.dp),
                ) {
                    Text(
                        text = stringResource(R.string.no_nodes),
                        style = MaterialTheme.typography.bodyLarge,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
            } else {
                LazyColumn(
                    contentPadding = PaddingValues(horizontal = 16.dp, vertical = 8.dp),
                    verticalArrangement = Arrangement.spacedBy(8.dp),
                ) {
                    sections.forEach { section ->
                        val expanded = expandedSections[section.id] ?: true
                        item(key = "section-${section.id}") {
                            NodeSectionHeader(
                                section = section,
                                expanded = expanded,
                                onToggle = {
                                    expandedSections[section.id] = !expanded
                                },
                                onMoveSubscription = viewModel::moveSubscription,
                                onCommitSubscriptionOrder = viewModel::commitSubscriptionOrder,
                            )
                        }
                        if (expanded) {
                            items(section.nodes, key = { it.id }) { node ->
                                NodeCard(
                                    node = node,
                                    isActive = node.id == state.activeNodeId,
                                    onSelect = {
                                        if (!node.stale) {
                                            viewModel.selectNode(node.id)
                                        }
                                    },
                                    onEdit = { nodeToEdit = node },
                                    onTestLatency = { viewModel.testLatency(node.id) },
                                    onDelete = { nodeToDelete = node },
                                )
                            }
                        }
                    }
                }
            }
        }
    }

    if (state.showImportSheet) {
        ImportSheet(
            initialTab = state.importSheetTab,
            initialText = state.importInitialText,
            candidates = state.importCandidates,
            canApplyEmptySubscriptionPreview = state.pendingSubscriptionPreview != null &&
                state.importCandidates.isEmpty(),
            isLoading = state.isLoading,
            errorMessage = state.errorMessage,
            statusMessage = state.statusMessage,
            onDetectUris = viewModel::detectUris,
            onToggleCandidate = viewModel::toggleImportCandidate,
            onImportSelected = viewModel::importSelected,
            onFetchSubscription = viewModel::fetchSubscription,
            onDismiss = viewModel::hideImportSheet,
        )
    }

    nodeToDelete?.let { node ->
        AlertDialog(
            onDismissRequest = { nodeToDelete = null },
            title = { Text(stringResource(R.string.delete)) },
            text = { Text(stringResource(R.string.delete_node_confirm, node.name)) },
            confirmButton = {
                TextButton(onClick = {
                    viewModel.deleteNode(node.id)
                    nodeToDelete = null
                }) {
                    Text(stringResource(R.string.confirm))
                }
            },
            dismissButton = {
                TextButton(onClick = { nodeToDelete = null }) {
                    Text(stringResource(R.string.cancel))
                }
            },
        )
    }

    nodeToEdit?.let { node ->
        EditNodeDialog(
            node = node,
            onDismiss = { nodeToEdit = null },
            onSave = { name, group, ownerPackage ->
                viewModel.updateNodeMetadata(node.id, name, group, ownerPackage)
                nodeToEdit = null
            },
        )
    }
}

private data class NodeSection(
    val id: String,
    val title: String,
    val subtitle: String,
    val nodes: List<Node>,
    val providerKey: String? = null,
)

@Composable
private fun buildNodeSections(
    nodes: List<Node>,
    subscriptions: List<SubscriptionUiSummary>,
): List<NodeSection> {
    val sections = mutableListOf<NodeSection>()
    val manualNodes = nodes.filter { it.source.type != NodeSourceType.SUBSCRIPTION }
    if (manualNodes.isNotEmpty()) {
        sections += NodeSection(
            id = "manual",
            title = stringResource(R.string.node_section_manual_configs),
            subtitle = stringResource(R.string.node_section_nodes_count, manualNodes.size),
            nodes = manualNodes,
        )
    }
    nodes
        .filter { it.source.type == NodeSourceType.SUBSCRIPTION }
        .groupBy { it.source.providerKey.ifBlank { it.source.url.ifBlank { "subscription" } } }
        .let { byProvider ->
            val consumed = mutableSetOf<String>()
            subscriptions.forEach { summary ->
                val providerKey = summary.providerKey
                val providerNodes = byProvider[providerKey].orEmpty()
                if (providerNodes.isEmpty()) return@forEach
                consumed += providerKey
                sections += subscriptionSection(
                    providerKey = providerKey,
                    providerNodes = providerNodes,
                    summary = summary,
                )
            }
            byProvider
                .filterKeys { it !in consumed }
                .toSortedMap(
                    compareBy<String> { key -> byProvider[key]?.firstOrNull()?.source?.url?.let(::hostLabel) ?: key }
                        .thenBy { it },
                )
                .forEach { (providerKey, providerNodes) ->
                    sections += subscriptionSection(
                        providerKey = providerKey,
                        providerNodes = providerNodes,
                        summary = null,
                    )
                }
        }
    return sections
}

@Composable
private fun subscriptionSection(
    providerKey: String,
    providerNodes: List<Node>,
    summary: SubscriptionUiSummary?,
): NodeSection {
    val title = summary?.displayName ?: providerNodes.firstOrNull()?.source?.url
        ?.let(::hostLabel)
        ?.ifBlank { null }
        ?: stringResource(R.string.subscription_provider_fallback)
    val activeCount = summary?.activeNodeCount ?: providerNodes.count { !it.stale }
    val staleCount = summary?.staleNodeCount ?: providerNodes.count { it.stale }
    val parseFailures = summary?.parseFailures ?: 0
    return NodeSection(
        id = "subscription-$providerKey",
        title = title,
        subtitle = if (parseFailures > 0) {
            stringResource(
                R.string.node_section_subscription_counts_with_errors,
                activeCount,
                staleCount,
                parseFailures,
            )
        } else {
            stringResource(
                R.string.node_section_subscription_counts,
                activeCount,
                staleCount,
            )
        },
        nodes = providerNodes,
        providerKey = providerKey,
    )
}

private fun hostLabel(url: String): String =
    runCatching { java.net.URI(url).host.orEmpty().removePrefix("www.") }.getOrDefault("")

@Composable
private fun NodeSectionHeader(
    section: NodeSection,
    expanded: Boolean,
    onToggle: () -> Unit,
    onMoveSubscription: (String, Int) -> Unit,
    onCommitSubscriptionOrder: () -> Unit,
) {
    val providerKey = section.providerKey
    Surface(
        color = MaterialTheme.colorScheme.surfaceVariant.copy(alpha = 0.55f),
        contentColor = MaterialTheme.colorScheme.onSurfaceVariant,
        shape = MaterialTheme.shapes.small,
        modifier = Modifier
            .fillMaxWidth()
            .clickable(onClick = onToggle),
    ) {
        Row(
            verticalAlignment = Alignment.CenterVertically,
            modifier = Modifier.padding(horizontal = 12.dp, vertical = 10.dp),
        ) {
            Icon(
                imageVector = if (expanded) Icons.Filled.KeyboardArrowDown else Icons.Filled.KeyboardArrowRight,
                contentDescription = null,
                tint = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            Spacer(modifier = Modifier.width(8.dp))
            Column(modifier = Modifier.weight(1f)) {
                Text(
                    text = section.title,
                    style = MaterialTheme.typography.titleSmall,
                    color = MaterialTheme.colorScheme.onSurface,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
                Text(
                    text = section.subtitle,
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
            }
            if (providerKey != null) {
                val thresholdPx = with(LocalDensity.current) { 64.dp.toPx() }
                var draggedPx by remember(providerKey) { mutableStateOf(0f) }
                Box(
                    contentAlignment = Alignment.Center,
                    modifier = Modifier
                        .size(44.dp)
                        .pointerInput(providerKey) {
                            detectDragGesturesAfterLongPress(
                                onDragStart = { draggedPx = 0f },
                                onDragEnd = {
                                    draggedPx = 0f
                                    onCommitSubscriptionOrder()
                                },
                                onDragCancel = {
                                    draggedPx = 0f
                                    onCommitSubscriptionOrder()
                                },
                                onDrag = { change, dragAmount ->
                                    change.consume()
                                    draggedPx += dragAmount.y
                                    while (draggedPx >= thresholdPx) {
                                        onMoveSubscription(providerKey, 1)
                                        draggedPx -= thresholdPx
                                    }
                                    while (draggedPx <= -thresholdPx) {
                                        onMoveSubscription(providerKey, -1)
                                        draggedPx += thresholdPx
                                    }
                                },
                            )
                        },
                ) {
                    Icon(
                        imageVector = Icons.Filled.DragHandle,
                        contentDescription = stringResource(R.string.drag_subscription),
                        tint = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
            }
        }
    }
}

@Composable
private fun StatusBanner(
    text: String,
    isError: Boolean,
    modifier: Modifier = Modifier,
) {
    val container = if (isError) {
        MaterialTheme.colorScheme.errorContainer
    } else {
        MaterialTheme.colorScheme.secondaryContainer
    }
    val content = if (isError) {
        MaterialTheme.colorScheme.onErrorContainer
    } else {
        MaterialTheme.colorScheme.onSecondaryContainer
    }
    Surface(
        color = container,
        contentColor = content,
        shape = MaterialTheme.shapes.small,
        modifier = modifier.fillMaxWidth(),
    ) {
        Text(
            text = text,
            style = MaterialTheme.typography.bodyMedium,
            modifier = Modifier.padding(horizontal = 12.dp, vertical = 10.dp),
        )
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun SelectionModeRow(
    isAuto: Boolean,
    hasNodes: Boolean,
    onAutoSelect: () -> Unit,
    onManualSelect: () -> Unit,
    modifier: Modifier = Modifier,
) {
    SingleChoiceSegmentedButtonRow(modifier = modifier.fillMaxWidth()) {
        val options = listOf(
            true to stringResource(R.string.node_selector_auto),
            false to stringResource(R.string.node_selector_manual),
        )
        options.forEachIndexed { index, (autoMode, label) ->
            SegmentedButton(
                selected = isAuto == autoMode,
                enabled = hasNodes,
                onClick = {
                    if (autoMode) {
                        onAutoSelect()
                    } else {
                        onManualSelect()
                    }
                },
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
private fun SortMenuButton(
    currentSort: NodeSortMode,
    onSortChange: (NodeSortMode) -> Unit,
) {
    var expanded by remember { mutableStateOf(false) }
    val options = nodeSortOptions()
    Box {
        TextButton(onClick = { expanded = true }) {
            Icon(Icons.Filled.Tune, contentDescription = null)
            Spacer(modifier = Modifier.width(8.dp))
            Text(
                text = options.firstOrNull { it.first == currentSort }?.second.orEmpty(),
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
        }
        DropdownMenu(
            expanded = expanded,
            onDismissRequest = { expanded = false },
        ) {
            options.forEach { (mode, label) ->
                DropdownMenuItem(
                    text = { Text(label) },
                    leadingIcon = {
                        if (mode == currentSort) {
                            Icon(Icons.Filled.Check, contentDescription = null)
                        }
                    },
                    onClick = {
                        expanded = false
                        onSortChange(mode)
                    },
                )
            }
        }
    }
}

@Composable
private fun nodeSortOptions(): List<Pair<NodeSortMode, String>> =
    listOf(
        NodeSortMode.SOURCE_ORDER to stringResource(R.string.sort_by_source_order),
        NodeSortMode.NAME to stringResource(R.string.sort_by_name),
        NodeSortMode.LATENCY to stringResource(R.string.sort_by_latency),
        NodeSortMode.THROUGHPUT to stringResource(R.string.sort_by_throughput),
        NodeSortMode.COUNTRY to stringResource(R.string.sort_by_country),
    )

@OptIn(ExperimentalFoundationApi::class)
@Composable
private fun NodeCard(
    node: Node,
    isActive: Boolean,
    onSelect: () -> Unit,
    onEdit: () -> Unit,
    onTestLatency: () -> Unit,
    onDelete: () -> Unit,
) {
    var showContextMenu by remember { mutableStateOf(false) }
    val clipboardManager = LocalClipboardManager.current

    val borderColor by animateColorAsState(
        targetValue = if (isActive) MaterialTheme.colorScheme.primary
        else MaterialTheme.colorScheme.outlineVariant,
        animationSpec = tween(300),
        label = "node_border",
    )

    Card(
        modifier = Modifier
            .fillMaxWidth()
            .combinedClickable(
                onClick = onSelect,
                onLongClick = { showContextMenu = true },
            ),
        border = CardDefaults.outlinedCardBorder().let {
            androidx.compose.foundation.BorderStroke(
                width = if (isActive) 2.dp else 1.dp,
                color = borderColor,
            )
        },
        colors = CardDefaults.cardColors(
            containerColor = if (isActive)
                MaterialTheme.colorScheme.primaryContainer.copy(alpha = 0.3f)
            else MaterialTheme.colorScheme.surfaceVariant.copy(alpha = 0.5f),
        ),
    ) {
        Row(
            verticalAlignment = Alignment.CenterVertically,
            modifier = Modifier.padding(16.dp),
        ) {
            // Country flag
            Text(
                text = countryFlagForNode(node.name),
                style = MaterialTheme.typography.headlineMedium,
            )

            Spacer(modifier = Modifier.width(12.dp))

            val okStatus = stringResource(R.string.node_test_status_ok)
            val tcpOkStatus = stringResource(R.string.node_test_status_tcp_ok)
            val hasFailedDataPlane = node.testStatus != null &&
                node.testStatus != okStatus &&
                node.testStatus != tcpOkStatus

            // Name + transport stack + server
            Column(modifier = Modifier.weight(1f)) {
                val testSummary = listOfNotNull(
                    node.latencyMs?.takeIf { it >= 0 }?.let { stringResource(R.string.node_test_tcp_ms, it) },
                    node.responseMs?.let { stringResource(R.string.node_test_url_ms, it) },
                    node.throughputBps?.takeIf { it > 0 }?.let {
                        stringResource(R.string.node_test_speed, formatBytes(it))
                    },
                    node.testStatus?.takeIf { it != okStatus && it != tcpOkStatus },
                ).joinToString(" | ")
                val sourceText = node.sourceLabel()
                Text(
                    text = node.name,
                    style = MaterialTheme.typography.bodyLarge,
                    fontWeight = FontWeight.Medium,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
                Text(
                    text = node.transportSignature(),
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Text(
                        text = "${node.server}:${node.port}",
                        style = MaterialTheme.typography.labelSmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                        modifier = Modifier.weight(1f, fill = false),
                    )
                    if (sourceText.isNotBlank()) {
                        Spacer(modifier = Modifier.width(8.dp))
                        Text(
                            text = sourceText,
                            style = MaterialTheme.typography.labelSmall,
                            color = if (node.stale) {
                                MaterialTheme.colorScheme.error
                            } else {
                                MaterialTheme.colorScheme.onSurfaceVariant
                            },
                            maxLines = 1,
                            overflow = TextOverflow.Ellipsis,
                        )
                    }
                }
                if (testSummary.isNotBlank()) {
                    Text(
                        text = testSummary,
                        style = MaterialTheme.typography.labelSmall,
                        color = if (node.stale) {
                            MaterialTheme.colorScheme.error
                        } else {
                            MaterialTheme.colorScheme.onSurfaceVariant
                        },
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                    )
                }
            }

            Spacer(modifier = Modifier.width(8.dp))

            // Latency chip
            val chipMs = when {
                node.responseMs != null -> node.responseMs
                hasFailedDataPlane -> -1
                else -> null
            }
            chipMs?.let { ms ->
                LatencyChip(ms)
            }
        }

        // Context menu
        Box {
            DropdownMenu(
                expanded = showContextMenu,
                onDismissRequest = { showContextMenu = false },
                offset = DpOffset(16.dp, 0.dp),
            ) {
                DropdownMenuItem(
                    text = { Text(stringResource(R.string.edit)) },
                    leadingIcon = { Icon(Icons.Filled.Edit, contentDescription = null) },
                    onClick = {
                        showContextMenu = false
                        onEdit()
                    },
                )
                DropdownMenuItem(
                    text = { Text(stringResource(R.string.copy_link)) },
                    leadingIcon = { Icon(Icons.Filled.ContentCopy, contentDescription = null) },
                    onClick = {
                        showContextMenu = false
                        clipboardManager.setText(AnnotatedString(node.link))
                    },
                )
                DropdownMenuItem(
                    text = { Text(stringResource(R.string.test_latency)) },
                    leadingIcon = { Icon(Icons.Filled.Speed, contentDescription = null) },
                    enabled = !node.stale,
                    onClick = {
                        showContextMenu = false
                        onTestLatency()
                    },
                )
                DropdownMenuItem(
                    text = {
                        Text(
                            stringResource(R.string.delete),
                            color = MaterialTheme.colorScheme.error,
                        )
                    },
                    leadingIcon = {
                        Icon(
                            Icons.Filled.Delete,
                            contentDescription = null,
                            tint = MaterialTheme.colorScheme.error,
                        )
                    },
                    onClick = {
                        showContextMenu = false
                        onDelete()
                    },
                )
            }
        }
    }
}

@Composable
private fun Node.sourceLabel(): String = when {
    stale -> stringResource(R.string.node_source_subscription_stale)
    source.type == NodeSourceType.SUBSCRIPTION -> stringResource(R.string.node_source_subscription)
    else -> ""
}

private fun Node.transportSignature(): String =
    listOf(
        protocol.displayName(),
        outbound.transportDisplayName(),
        outbound.securityDisplayName(),
    )
        .filter(String::isNotBlank)
        .joinToString(" | ")

private fun Protocol.displayName(): String = when (this) {
    Protocol.VLESS -> "Vless"
    Protocol.VMESS -> "Vmess"
    Protocol.TROJAN -> "Trojan"
    Protocol.SHADOWSOCKS -> "Shadowsocks"
    Protocol.SOCKS -> "Socks"
    Protocol.HYSTERIA2 -> "Hysteria2"
    Protocol.TUIC -> "Tuic"
    Protocol.WIREGUARD -> "WireGuard"
}

private fun JsonObject.transportDisplayName(): String =
    streamSettings()
        ?.string("network")
        ?.ifBlank { null }
        ?.let(::formatTransportName)
        ?: ""

private fun JsonObject.securityDisplayName(): String =
    streamSettings()
        ?.string("security")
        ?.ifBlank { null }
        ?.takeUnless { it.equals("none", ignoreCase = true) }
        ?.replaceFirstChar { it.uppercase() }
        ?: ""

private fun JsonObject.streamSettings(): JsonObject? = obj("streamSettings")

private fun JsonObject.obj(key: String): JsonObject? =
    runCatching { this[key]?.jsonObject }.getOrNull()

private fun JsonObject.string(key: String): String =
    runCatching { this[key]?.jsonPrimitive?.contentOrNull }.getOrNull().orEmpty()

private fun formatTransportName(raw: String): String = when (raw.lowercase()) {
    "ws" -> "WebSocket(WS)"
    "grpc" -> "gRPC"
    "http", "h2" -> "HTTP/2"
    "httpupgrade" -> "HTTPUpgrade"
    "xhttp" -> "XHTTP"
    "splithttp" -> "SplitHTTP"
    "tcp" -> "TCP"
    "kcp" -> "mKCP"
    "quic" -> "QUIC"
    else -> raw.replaceFirstChar { it.uppercase() }
}

@Composable
private fun EditNodeDialog(
    node: Node,
    onDismiss: () -> Unit,
    onSave: (String, String, String) -> Unit,
) {
    var name by remember(node.id) { mutableStateOf(node.name) }
    var group by remember(node.id) { mutableStateOf(node.group) }
    var ownerPackage by remember(node.id) { mutableStateOf(node.ownerPackage) }
    var showOwnerPicker by remember(node.id) { mutableStateOf(false) }

    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(stringResource(R.string.edit_node_title)) },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(12.dp)) {
                OutlinedTextField(
                    value = name,
                    onValueChange = { name = it },
                    label = { Text(stringResource(R.string.node_name_label)) },
                    singleLine = true,
                    modifier = Modifier.fillMaxWidth(),
                )
                OutlinedTextField(
                    value = group,
                    onValueChange = { group = it },
                    label = { Text(stringResource(R.string.node_group_label)) },
                    singleLine = true,
                    modifier = Modifier.fillMaxWidth(),
                )
                if (node.isLoopbackNode()) {
                    AppPackageSelector(
                        label = stringResource(R.string.node_owner_package_label),
                        selectedPackage = ownerPackage,
                        onChoose = { showOwnerPicker = true },
                        onClear = { ownerPackage = "" },
                    )
                }
            }
        },
        confirmButton = {
            TextButton(
                onClick = { onSave(name, group, ownerPackage) },
                enabled = name.isNotBlank(),
            ) {
                Text(stringResource(R.string.save))
            }
        },
        dismissButton = {
            TextButton(onClick = onDismiss) {
                Text(stringResource(R.string.cancel))
            }
        },
    )
    if (showOwnerPicker) {
        AppPackagePickerDialog(
            title = stringResource(R.string.choose_app),
            onDismiss = { showOwnerPicker = false },
            onSelect = { ownerPackage = it },
        )
    }
}

@Composable
private fun LatencyChip(ms: Int) {
    val color = when {
        ms < 0 -> MaterialTheme.colorScheme.outline
        ms < 200 -> MaterialTheme.colorScheme.primary // green/teal
        ms < 500 -> MaterialTheme.colorScheme.tertiary // yellow/amber
        else -> MaterialTheme.colorScheme.error // red
    }
    val text = if (ms < 0) {
        stringResource(R.string.node_latency_error)
    } else {
        stringResource(R.string.ms_format, ms)
    }

    Card(
        colors = CardDefaults.cardColors(containerColor = color.copy(alpha = 0.15f)),
    ) {
        Text(
            text = text,
            style = MaterialTheme.typography.labelSmall,
            color = color,
            fontWeight = FontWeight.Bold,
            modifier = Modifier.padding(horizontal = 8.dp, vertical = 4.dp),
        )
    }
}

/**
 * Derive a country flag emoji from the node name heuristic.
 */
private fun countryFlagForNode(name: String): String {
    val lower = name.lowercase()
    return when {
        lower.startsWith("frankfurt") || lower.startsWith("berlin") ||
            lower.contains("-de") || lower.startsWith("de-") -> "\uD83C\uDDE9\uD83C\uDDEA"
        lower.startsWith("amsterdam") || lower.contains("-nl") ||
            lower.startsWith("nl-") -> "\uD83C\uDDF3\uD83C\uDDF1"
        lower.startsWith("helsinki") || lower.contains("-fi") ||
            lower.startsWith("fi-") -> "\uD83C\uDDEB\uD83C\uDDEE"
        lower.startsWith("tokyo") || lower.contains("-jp") ||
            lower.startsWith("jp-") -> "\uD83C\uDDEF\uD83C\uDDF5"
        lower.startsWith("us-") || lower.contains("-us") ||
            lower.startsWith("new york") || lower.startsWith("la-") -> "\uD83C\uDDFA\uD83C\uDDF8"
        lower.startsWith("london") || lower.contains("-uk") ||
            lower.contains("-gb") -> "\uD83C\uDDEC\uD83C\uDDE7"
        lower.startsWith("paris") || lower.contains("-fr") ||
            lower.startsWith("fr-") -> "\uD83C\uDDEB\uD83C\uDDF7"
        lower.startsWith("moscow") || lower.startsWith("ru-") ||
            lower.contains("-ru") -> "\uD83C\uDDF7\uD83C\uDDFA"
        lower.startsWith("singapore") || lower.contains("-sg") ||
            lower.startsWith("sg-") -> "\uD83C\uDDF8\uD83C\uDDEC"
        else -> "\uD83C\uDF10" // globe
    }
}

private fun Node.isLoopbackNode(): Boolean {
    val host = server.trim().removePrefix("[").removeSuffix("]").lowercase()
    return host == "localhost" ||
        host == "ip6-localhost" ||
        host == "127.0.0.1" ||
        host == "::1" ||
        host == "0.0.0.0" ||
        host == "::"
}

private fun formatBytes(bytes: Long): String {
    if (bytes <= 0) return "0 B"
    val units = arrayOf("B", "KB", "MB", "GB", "TB")
    var value = bytes.toDouble()
    var idx = 0
    while (value >= 1024 && idx < units.lastIndex) {
        value /= 1024
        idx++
    }
    return if (value == value.toLong().toDouble()) "${value.toLong()} ${units[idx]}"
    else "%.1f ${units[idx]}".format(value)
}
