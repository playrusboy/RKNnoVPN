package com.rknnovpn.panel.ipc

import com.rknnovpn.panel.model.BackendStatusV2
import com.rknnovpn.panel.model.Node
import com.rknnovpn.panel.model.ProfileConfig
import com.rknnovpn.panel.model.Subscription
import com.rknnovpn.panel.model.SubscriptionSource
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonElement

data class ProfileSummary(
    val id: String,
    val name: String,
    val isActive: Boolean
)

data class VersionInfo(
    val daemonVersion: String,
    val coreVersion: String,
    val daemonctlVersion: String,
    val moduleVersion: String = "unknown",
    val moduleVersionCode: String = "",
    val modulePath: String = "",
    val currentReleaseVersion: String = "",
    val currentReleaseOK: Boolean? = null,
    val currentReleaseError: String = "",
    val runtimePreflightOK: Boolean = true,
    val runtimePreflightIssues: List<String> = emptyList(),
    val runtimePreflightWarnings: List<String> = emptyList(),
    val singBoxAvailable: Boolean = true,
    val singBoxError: String = "",
    val controlProtocolVersion: Int = 0,
    val schemaVersion: Int = 0,
    val ipcContractVersion: Int = 0,
    val contractHash: String = "",
    val compatibilityFingerprint: String = "",
    val daemonPid: Int = 0,
    val socketInode: String = "",
    val panelMinVersion: String = "",
    val capabilities: List<String> = emptyList(),
    val supportedMethods: List<String> = emptyList(),
    val apkRequiredMethods: List<String> = emptyList(),
    val errorCodes: List<String> = emptyList(),
    val compatibilityPolicies: List<String> = emptyList(),
    val operationPolicies: Map<String, IpcOperationPolicyInfo> = emptyMap(),
    val methods: List<IpcMethodContractInfo> = emptyList(),
)

data class UpdateCheckInfo(
    val currentVersion: String,
    val latestVersion: String,
    val hasUpdate: Boolean,
    val changelog: String,
    val moduleSize: Long = 0L,
    val apkSize: Long = 0L,
)

data class UpdateDownloadInfo(
    val modulePath: String,
    val apkPath: String,
    val checksums: Boolean,
    val manifestPath: String = "",
    val operation: JsonElement? = null,
)

data class UpdateInstallInfo(
    val accepted: Boolean,
    val operationGeneration: Long = 0L,
    val runtimeStatus: BackendStatusV2? = null,
)

data class ConfigMutationInfo(
    val ok: Boolean = true,
    val status: String = "",
    val reload: Boolean = false,
    val updated: Int? = null,
    val configSaved: Boolean = true,
    val runtimeApplied: Boolean = true,
    val runtimeApply: String = "",
    val code: String = "",
    val message: String = "",
    val operation: JsonElement? = null,
    val runtimeStatus: BackendStatusV2? = null,
    val source: SubscriptionSource? = null,
    val subscription: Subscription? = null,
    val imported: Int? = null,
    val parseFailures: Int? = null,
    val rejected: Int? = null,
    val rejectedNodes: List<RejectedSubscriptionNode> = emptyList(),
    val profile: ProfileConfig? = null,
)

@Serializable
data class RejectedSubscriptionNode(
    val link: String = "",
    val name: String = "",
    val protocol: String = "",
    val server: String = "",
    val port: Int = 0,
    val code: String = "",
    val reason: String = "",
)

@Serializable
data class SubscriptionPreviewInfo(
    val source: SubscriptionSource = SubscriptionSource(),
    val subscription: Subscription = Subscription(providerKey = "", url = ""),
    val nodes: List<Node> = emptyList(),
    val rejectedNodes: List<RejectedSubscriptionNode> = emptyList(),
    val rejected: Int = 0,
    val added: Int = 0,
    val updated: Int = 0,
    val unchanged: Int = 0,
    val stale: Int = 0,
    val parseFailures: Int = 0,
)

data class NodeTestInfo(
    val url: String,
    val results: List<NodeTestResult>,
)

data class RuntimeLogFile(
    val name: String,
    val path: String,
    val lines: List<String>,
    val missing: Boolean,
    val error: String,
)

data class RuntimeLogsInfo(
    val files: List<RuntimeLogFile>,
) {
    val text: String
        get() = files.joinToString("\n\n") { file ->
            buildString {
                append("== ")
                append(file.path.ifBlank { file.name })
                append(" ==")
                when {
                    file.missing -> append("\n(missing)")
                    file.error.isNotBlank() -> append("\n(error: ").append(file.error).append(")")
                    file.lines.isEmpty() -> append("\n(empty)")
                    else -> append('\n').append(file.lines.joinToString("\n"))
                }
            }
        }
}

@Serializable
data class SelfCheckSummary(
    val status: String = "unknown",
    val healthCode: String = "",
    val healthDetail: String = "",
    val operationalHealthy: Boolean = false,
    val rebootRequired: Boolean = false,
    val issueCount: Int = 0,
    val issues: List<String> = emptyList(),
    val compatibilityIssues: List<String> = emptyList(),
    val privacyIssues: List<String> = emptyList(),
    val compatibility: SelfCheckCompatibility = SelfCheckCompatibility(),
    val runtime: SelfCheckRuntime = SelfCheckRuntime(),
    val routing: SelfCheckRouting = SelfCheckRouting(),
    val nodeTests: SelfCheckNodeTests = SelfCheckNodeTests(),
)

@Serializable
data class SelfCheckCompatibility(
    val daemonVersion: String = "",
    val moduleVersion: String = "",
    val controlProtocolVersion: Int = 0,
    val schemaVersion: Int = 0,
    val ipcContractVersion: Int = 0,
    val apkRequiredMethodCount: Int = 0,
    val panelMinVersion: String = "",
    val currentReleaseVersion: String = "",
    val currentReleaseOk: Boolean = false,
    val singBoxCheckOk: Boolean = false,
)

@Serializable
data class SelfCheckRuntime(
    val stageOperation: String = "",
    val stageStatus: String = "",
    val failedStage: String = "",
    val lastCode: String = "",
    val rollbackApplied: Boolean = false,
    val runtimeReportAge: String = "",
)

@Serializable
data class SelfCheckRouting(
    val mode: String = "",
    val activeNodeMode: String = "",
    val activeNodeId: String = "",
    val activeNodeName: String = "",
    val activeNodeProtocol: String = "",
    val nodeCount: Int = 0,
    val groups: List<String> = emptyList(),
    val appGroupRouteCount: Int = 0,
    val sharingEnabled: Boolean = false,
)

@Serializable
data class SelfCheckNodeTests(
    val total: Int = 0,
    val usable: Int = 0,
    val unusable: Int = 0,
    val tcpOnly: Int = 0,
)

data class NodeTestResult(
    val id: String,
    val name: String,
    val tag: String,
    val server: String,
    val port: Int,
    val protocol: String,
    val tcpMs: Int?,
    val urlMs: Int?,
    val throughputBps: Long? = null,
    val tcpStatus: String = "",
    val urlStatus: String = "",
    val verdict: String = "",
    val status: Int?,
    val tcpError: String?,
    val urlError: String?,
)
