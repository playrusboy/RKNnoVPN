package com.rknnovpn.panel.ipc

import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.put
import java.nio.charset.StandardCharsets

internal const val IPC_MAX_FRAME_BYTES = 2 * 1024 * 1024
internal const val IPC_IMPORT_BATCH_TARGET_BYTES = IPC_MAX_FRAME_BYTES - 128 * 1024
internal const val IPC_PAYLOAD_TOO_LARGE_MESSAGE =
    "Конфиг слишком большой: импортируйте подписку через URL или батчами."

internal fun jsonRpcRequestFrameByteSize(method: String, params: JsonObject): Int =
    buildJsonObject {
        put("jsonrpc", "2.0")
        put("id", 1)
        put("method", method)
        if (params.isNotEmpty()) {
            put("params", params)
        }
    }.toString().toByteArray(StandardCharsets.UTF_8).size
