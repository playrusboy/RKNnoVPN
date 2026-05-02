package com.rknnovpn.panel.ipc

import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Test

class ConfigMutationParserTest {
    private val json = Json {
        ignoreUnknownKeys = true
        isLenient = true
        coerceInputValues = true
        encodeDefaults = true
    }

    @Test
    fun parsesProfileReturnedWithMutationResult() {
        val result = json.parseConfigMutationInfo(
            json.parseToJsonElement(
                """
                {
                  "ok": true,
                  "status": "ok",
                  "profile": {
                    "profileSchemaVersion": 2,
                    "id": "default",
                    "name": "Default",
                    "activeNodeId": "node-1",
                    "nodes": [
                      {
                        "id": "node-1",
                        "name": "Primary",
                        "protocol": "vless",
                        "server": "example.com",
                        "port": 443,
                        "outbound": {}
                      }
                    ]
                  }
                }
                """.trimIndent(),
            ),
        )

        assertNotNull(result.profile)
        val profile = result.profile!!
        assertEquals("node-1", profile.activeNodeId)
        assertEquals("Primary", profile.nodes.single().name)
        assertEquals(JsonObject(emptyMap()), profile.nodes.single().outbound)
    }
}
