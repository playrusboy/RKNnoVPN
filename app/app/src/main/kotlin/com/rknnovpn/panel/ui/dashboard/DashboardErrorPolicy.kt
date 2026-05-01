package com.rknnovpn.panel.ui.dashboard

import com.rknnovpn.panel.model.DaemonStatus
import com.rknnovpn.panel.model.RuntimeOperationResult
import java.time.Instant

internal object DashboardErrorPolicy {
    fun shouldSuppressInitialPollError(
        latestStatus: DaemonStatus?,
        userRequestedRuntimeOperation: Boolean,
        deviceUptimeMs: Long,
        initialBootErrorSuppressionMs: Long,
    ): Boolean =
        latestStatus == null &&
            !userRequestedRuntimeOperation &&
            deviceUptimeMs < initialBootErrorSuppressionMs

    fun shouldShowLastOperationFailure(
        status: DaemonStatus,
        operation: RuntimeOperationResult,
        userRequestedRuntimeOperation: Boolean,
        appStartedAtEpochSeconds: Long,
    ): Boolean {
        if (userRequestedRuntimeOperation || status.activeOperation != null) return true
        val finishedAt = operation.finishedAt?.epochSecondsOrNull() ?: return false
        return finishedAt >= appStartedAtEpochSeconds
    }
}

private fun String.epochSecondsOrNull(): Long? =
    runCatching { Instant.parse(this).epochSecond }.getOrNull()
