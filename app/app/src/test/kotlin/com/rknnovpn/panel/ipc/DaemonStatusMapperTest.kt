package com.rknnovpn.panel.ipc

import com.rknnovpn.panel.model.BackendHealthSnapshot
import com.rknnovpn.panel.model.BackendPhase
import com.rknnovpn.panel.model.BackendStatusV2
import com.rknnovpn.panel.model.CanonicalRuntimeStatus
import com.rknnovpn.panel.model.ConnectionState
import com.rknnovpn.panel.model.RuntimeReadinessStatus
import org.junit.Assert.assertEquals
import org.junit.Test

class DaemonStatusMapperTest {
    @Test
    fun mapsOperationalFailureToErrorEvenWhenCoreReadinessIsGreen() {
        val status = BackendStatusV2(
            canonical = CanonicalRuntimeStatus(
                phase = BackendPhase.OUTBOUND_CHECKED,
                readiness = RuntimeReadinessStatus(
                    ready = true,
                    operationalHealthy = false,
                    coreReady = true,
                    routingReady = true,
                    dnsReady = true,
                    egressReady = false,
                ),
                lastCode = "OUTBOUND_URL_FAILED",
            ),
            health = BackendHealthSnapshot(
                coreReady = true,
                routingReady = true,
                dnsReady = true,
                egressReady = false,
                lastCode = "OUTBOUND_URL_FAILED",
            ),
        ).toDaemonStatus()

        assertEquals(ConnectionState.ERROR, status.state)
        assertEquals("OUTBOUND_URL_FAILED", status.health.lastCode)
    }
}
