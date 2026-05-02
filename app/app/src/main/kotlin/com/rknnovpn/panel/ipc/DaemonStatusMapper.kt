package com.rknnovpn.panel.ipc

import com.rknnovpn.panel.BuildConfig
import com.rknnovpn.panel.model.BackendHealthSnapshot
import com.rknnovpn.panel.model.BackendPhase
import com.rknnovpn.panel.model.BackendStatusV2
import com.rknnovpn.panel.model.ConnectionState
import com.rknnovpn.panel.model.DaemonStatus
import com.rknnovpn.panel.model.HealthResult

private const val MAX_REASONABLE_RUNTIME_UPTIME_SECONDS = 366L * 24L * 60L * 60L

internal fun BackendStatusV2.toDaemonStatus(
    healthOverride: BackendHealthSnapshot? = null,
): DaemonStatus {
    val effectiveHealth = healthOverride ?: health
    val effectiveCanonical = canonical
    val canonicalReady = effectiveCanonical?.readiness?.ready ?: effectiveHealth.healthy
    val canonicalOperationalReady =
        effectiveCanonical?.readiness?.operationalHealthy ?: effectiveHealth.operationalHealthy
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
            if (canonicalReady && canonicalOperationalReady) {
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
        uptime = if (displayPhase == BackendPhase.STOPPED) {
            0L
        } else {
            normalizedRuntimeUptime(uptimeSeconds, appliedState.startedAt)
        },
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

private fun normalizedRuntimeUptime(rawSeconds: Long, startedAt: String?): Long {
    rawSeconds
        .takeIf { it in 1L..MAX_REASONABLE_RUNTIME_UPTIME_SECONDS }
        ?.let { return it }
    return startedAt
        ?.let(::elapsedSecondsSince)
        ?.takeIf { it in 1L..MAX_REASONABLE_RUNTIME_UPTIME_SECONDS }
        ?: 0L
}

private fun elapsedSecondsSince(raw: String): Long =
    runCatching {
        val startedAt = java.time.Instant.parse(raw)
        java.time.Duration.between(startedAt, java.time.Instant.now()).seconds.coerceAtLeast(0L)
    }.getOrDefault(0L)
