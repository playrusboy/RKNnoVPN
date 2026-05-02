package com.rknnovpn.panel.ipc

import com.rknnovpn.panel.model.BackendStatusV2
import com.rknnovpn.panel.model.ProfileConfig
import com.rknnovpn.panel.model.Subscription
import com.rknnovpn.panel.model.SubscriptionSource
import kotlinx.serialization.builtins.ListSerializer
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.booleanOrNull
import kotlinx.serialization.json.contentOrNull
import kotlinx.serialization.json.decodeFromJsonElement
import kotlinx.serialization.json.intOrNull
import kotlinx.serialization.json.jsonArray
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive

internal fun Json.parseConfigMutationInfo(element: JsonElement): ConfigMutationInfo {
    val obj = element.jsonObject
    val ok = obj.booleanField("ok") ?: true
    val configSaved = obj.booleanField("config_saved", "configSaved") ?: ok
    val runtimeApplied = obj.booleanField("runtime_applied", "runtimeApplied") ?: ok
    return ConfigMutationInfo(
        ok = ok,
        status = obj["status"]?.jsonPrimitive?.contentOrNull.orEmpty(),
        reload = obj["reload"]?.jsonPrimitive?.booleanOrNull ?: false,
        updated = obj["updated"]?.jsonPrimitive?.intOrNull,
        configSaved = configSaved,
        runtimeApplied = runtimeApplied,
        runtimeApply = (
            obj["runtime_apply"]?.jsonPrimitive?.contentOrNull
                ?: obj["runtimeApply"]?.jsonPrimitive?.contentOrNull
            ).orEmpty(),
        runtimeApplyMode = obj["runtimeApplyMode"]?.jsonPrimitive?.contentOrNull.orEmpty(),
        runtimeApplyReason = obj["runtimeApplyReason"]?.jsonPrimitive?.contentOrNull.orEmpty(),
        requiresHotSwap = obj["requiresHotSwap"]?.jsonPrimitive?.booleanOrNull ?: false,
        code = obj["code"]?.jsonPrimitive?.contentOrNull.orEmpty(),
        message = obj["message"]?.jsonPrimitive?.contentOrNull.orEmpty(),
        operation = obj["operation"],
        runtimeStatus = obj["runtimeStatus"]?.let {
            decodeFromJsonElement(BackendStatusV2.serializer(), it)
        },
        source = obj["source"]?.let {
            decodeFromJsonElement(SubscriptionSource.serializer(), it)
        },
        subscription = obj["subscription"]?.let {
            decodeFromJsonElement(Subscription.serializer(), it)
        },
        imported = obj["imported"]?.jsonPrimitive?.intOrNull,
        parseFailures = obj["parseFailures"]?.jsonPrimitive?.intOrNull,
        rejected = obj["rejected"]?.jsonPrimitive?.intOrNull,
        rejectedNodes = obj["rejectedNodes"]?.jsonArray?.let {
            decodeFromJsonElement(ListSerializer(RejectedSubscriptionNode.serializer()), it)
        }.orEmpty(),
        profile = obj["profile"]?.let {
            decodeFromJsonElement(ProfileConfig.serializer(), it)
        },
    )
}

private fun JsonElement.booleanOrNull(): Boolean? = jsonPrimitive.booleanOrNull

private fun JsonObject.booleanField(vararg keys: String): Boolean? {
    for (key in keys) {
        val value = this[key]?.booleanOrNull()
        if (value != null) return value
    }
    return null
}
