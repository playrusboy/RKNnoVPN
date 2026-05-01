package com.rknnovpn.panel.ipc

import com.rknnovpn.panel.BuildConfig
import com.rknnovpn.panel.model.BackendHealthSnapshot
import com.rknnovpn.panel.model.BackendPhase
import com.rknnovpn.panel.model.BackendStatusV2
import com.rknnovpn.panel.model.ConnectionState
import com.rknnovpn.panel.model.DaemonStatus
import com.rknnovpn.panel.model.HealthResult

internal fun BackendStatusV2.toDaemonStatus(
    healthOverride: BackendHealthSnapshot? = null,
): DaemonStatus {
    val effectiveHealth = healthOverride ?: health
    val effectiveCanonical = canonical
    val canonicalReady = effectiveCanonical?.readiness?.ready ?: effectiveHealth.healthy
    val compatibilityIssue = compatibility?.blockingCompatibilityIssue(
        apkVersion = BuildConfig.VERSION_NAME,
        requiredMethods = DaemonClient.REQUIRED_METHODS,
    )
    val displayPhase = effectiveCanonical?.phase ?: activeOperation?.phase ?: appliedState.phase
    val lastFailure = lastOperation?.takeIf { !it.succeeded && activeOperation == null }
    val connectionState = if (compatibilityIssue != null) {
        ConnectionState.ERROR
    } else when (displayPhase) {
        BackendPhase.HEALTHY,
        BackendPhase.RULES_APPLIED,
        BackendPhase.DNS_APPLIED,
        BackendPhase.OUTBOUND_CHECKED,
        BackendPhase.DEGRADED ->
            if (canonicalReady) {
                ConnectionState.CONNECTED
            } else {
                ConnectionState.ERROR
            }
        BackendPhase.STOPPED -> ConnectionState.DISCONNECTED
        BackendPhase.APPLYING,
        BackendPhase.STARTING,
        BackendPhase.CONFIG_CHECKED,
        BackendPhase.CORE_SPAWNED,
        BackendPhase.CORE_LISTENING,
        BackendPhase.STOPPING,
        BackendPhase.RESETTING -> ConnectionState.CONNECTING
        BackendPhase.FAILED -> ConnectionState.ERROR
    }

    return DaemonStatus(
        state = connectionState,
        activeNodeId = effectiveCanonical?.activeProfileId ?: desiredState.activeProfileId,
        uptime = uptimeSeconds.takeIf { it > 0L } ?: appliedState.startedAt?.let(::elapsedSecondsSince) ?: 0L,
        traffic = traffic,
        health = HealthResult(
            healthy = effectiveCanonical?.readiness?.ready ?: effectiveHealth.healthy,
            coreRunning = effectiveCanonical?.readiness?.coreReady ?: effectiveHealth.coreReady,
            tunActive = false,
            dnsOperational = effectiveCanonical?.readiness?.dnsReady ?: effectiveHealth.dnsReady,
            routingReady = effectiveCanonical?.readiness?.routingReady ?: effectiveHealth.routingReady,
            egressReady = effectiveCanonical?.readiness?.egressReady ?: effectiveHealth.egressReady,
            operationalHealthy = effectiveCanonical?.readiness?.operationalHealthy ?: effectiveHealth.operationalHealthy,
            backendKind = effectiveCanonical?.backendKind ?: appliedState.backendKind,
            phase = displayPhase,
            lastDebug = effectiveCanonical?.lastDebug?.ifBlank { null } ?: effectiveHealth.lastDebug.ifBlank { null },
            rollbackApplied = effectiveCanonical?.rollbackApplied ?: effectiveHealth.rollbackApplied,
            stageReport = effectiveHealth.stageReport,
            checkedAt = (effectiveCanonical?.checkedAt ?: effectiveHealth.checkedAt)?.let(::epochSeconds) ?: 0L,
            lastCode = if (compatibilityIssue != null) {
                "DAEMON_INCOMPATIBLE"
            } else {
                effectiveCanonical?.lastCode?.ifBlank { null }
                    ?: effectiveHealth.lastCode.ifBlank { lastFailure?.errorCode?.ifBlank { null } }
            },
            lastError = if (compatibilityIssue != null) {
                compatibilityIssue
            } else {
                effectiveCanonical?.lastError?.ifBlank { null }
                    ?: effectiveHealth.lastError.ifBlank { lastFailure?.errorMessage?.ifBlank { null } }
            },
            lastUserMessage = compatibilityIssue
                ?: effectiveCanonical?.lastUserMessage?.ifBlank { null }
                ?: effectiveHealth.lastUserMessage.ifBlank { null },
        ),
        compatibility = compatibility,
        activeOperation = activeOperation,
        lastOperation = lastOperation,
        updateInstall = updateInstall,
    )
}

private fun epochSeconds(raw: String): Long =
    runCatching { java.time.Instant.parse(raw).epochSecond }.getOrDefault(0L)

private fun elapsedSecondsSince(raw: String): Long =
    runCatching {
        val startedAt = java.time.Instant.parse(raw)
        java.time.Duration.between(startedAt, java.time.Instant.now()).seconds.coerceAtLeast(0L)
    }.getOrDefault(0L)
