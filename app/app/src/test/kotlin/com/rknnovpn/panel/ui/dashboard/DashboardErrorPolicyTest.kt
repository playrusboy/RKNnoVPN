package com.rknnovpn.panel.ui.dashboard

import com.rknnovpn.panel.model.ConnectionState
import com.rknnovpn.panel.model.DaemonStatus
import com.rknnovpn.panel.model.RuntimeOperationResult
import com.rknnovpn.panel.model.RuntimeOperationStatus
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class DashboardErrorPolicyTest {
    @Test
    fun suppressesInitialPollErrorDuringBootBeforeFirstStatus() {
        assertTrue(
            DashboardErrorPolicy.shouldSuppressInitialPollError(
                latestStatus = null,
                userRequestedRuntimeOperation = false,
                deviceUptimeMs = 30_000L,
                initialBootErrorSuppressionMs = 180_000L,
            ),
        )
    }

    @Test
    fun doesNotSuppressInitialPollErrorAfterBootWindow() {
        assertFalse(
            DashboardErrorPolicy.shouldSuppressInitialPollError(
                latestStatus = null,
                userRequestedRuntimeOperation = false,
                deviceUptimeMs = 180_000L,
                initialBootErrorSuppressionMs = 180_000L,
            ),
        )
    }

    @Test
    fun doesNotSuppressInitialPollErrorAfterSuccessfulStatus() {
        assertFalse(
            DashboardErrorPolicy.shouldSuppressInitialPollError(
                latestStatus = DaemonStatus(state = ConnectionState.DISCONNECTED),
                userRequestedRuntimeOperation = false,
                deviceUptimeMs = 30_000L,
                initialBootErrorSuppressionMs = 180_000L,
            ),
        )
    }

    @Test
    fun doesNotSuppressInitialPollErrorAfterUserRuntimeAction() {
        assertFalse(
            DashboardErrorPolicy.shouldSuppressInitialPollError(
                latestStatus = null,
                userRequestedRuntimeOperation = true,
                deviceUptimeMs = 30_000L,
                initialBootErrorSuppressionMs = 180_000L,
            ),
        )
    }

    @Test
    fun hidesLastOperationFailureFromPreviousAppProcess() {
        assertFalse(
            DashboardErrorPolicy.shouldShowLastOperationFailure(
                status = DaemonStatus(state = ConnectionState.ERROR),
                operation = RuntimeOperationResult(
                    kind = "start",
                    finishedAt = "2026-05-01T09:59:59Z",
                    succeeded = false,
                    errorMessage = "daemon is not running",
                ),
                userRequestedRuntimeOperation = false,
                appStartedAtEpochSeconds = 1_777_629_600L,
            ),
        )
    }

    @Test
    fun showsLastOperationFailureFromCurrentAppProcess() {
        assertTrue(
            DashboardErrorPolicy.shouldShowLastOperationFailure(
                status = DaemonStatus(state = ConnectionState.ERROR),
                operation = RuntimeOperationResult(
                    kind = "start",
                    finishedAt = "2026-05-01T10:00:10Z",
                    succeeded = false,
                    errorMessage = "daemon is not running",
                ),
                userRequestedRuntimeOperation = false,
                appStartedAtEpochSeconds = 1_777_629_600L,
            ),
        )
    }

    @Test
    fun showsLastOperationFailureDuringActiveOperation() {
        assertTrue(
            DashboardErrorPolicy.shouldShowLastOperationFailure(
                status = DaemonStatus(
                    state = ConnectionState.ERROR,
                    activeOperation = RuntimeOperationStatus(kind = "start"),
                ),
                operation = RuntimeOperationResult(
                    kind = "start",
                    finishedAt = "2026-05-01T10:00:00Z",
                    succeeded = false,
                    errorMessage = "daemon is not running",
                ),
                userRequestedRuntimeOperation = false,
                appStartedAtEpochSeconds = 1_777_629_600L,
            ),
        )
    }
}
