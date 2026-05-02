package com.rknnovpn.panel.ipc

import kotlinx.serialization.json.Json
import org.junit.Assert.assertEquals
import org.junit.Test

class DaemonResponseParsersTest {
    @Test
    fun parsesCompatibilityFingerprintFieldsFromVersion() {
        val info = parseVersionInfo(
            Json.parseToJsonElement(
                """
                {
                  "daemon": "v2.0.0",
                  "core": "v2.0.0",
                  "daemonctl": "v2.0.0",
                  "module": {"version": "v2.0.0"},
                  "control_protocol_version": 5,
                  "schema_version": 5,
                  "ipc_contract_version": 1,
                  "contract_hash": "abc123",
                  "compatibility_fingerprint": "fp123",
                  "daemon_pid": 1234,
                  "socket_inode": "5678",
                  "supported_methods": ["version", "ipc.contract"]
                }
                """.trimIndent(),
            ),
        )

        assertEquals("abc123", info.contractHash)
        assertEquals("fp123", info.compatibilityFingerprint)
        assertEquals(1234, info.daemonPid)
        assertEquals("5678", info.socketInode)
    }
}
