package com.rknnovpn.panel.`import`

import com.rknnovpn.panel.model.Node
import com.rknnovpn.panel.model.Protocol
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonObjectBuilder
import kotlinx.serialization.json.add
import kotlinx.serialization.json.addJsonObject
import kotlinx.serialization.json.booleanOrNull
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.contentOrNull
import kotlinx.serialization.json.intOrNull
import kotlinx.serialization.json.jsonArray
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import kotlinx.serialization.json.put
import kotlinx.serialization.json.putJsonArray
import kotlinx.serialization.json.putJsonObject
import java.util.UUID

/**
 * Imports sing-box outbound JSON into the app's canonical profile node shape.
 *
 * The daemon persists node outbounds in a V2Ray/Xray-like schema and renders
 * sing-box config from that normalized profile. This importer accepts common
 * sing-box outbound objects and converts the fields that the renderer needs.
 */
object SingBoxOutboundImporter {
    private val json = Json { ignoreUnknownKeys = true }

    private val ignoredTypes = setOf(
        "direct", "block", "dns", "selector", "urltest", "fallback", "loadbalance",
    )

    fun parse(text: String): List<Node> {
        val element = runCatching { json.parseToJsonElement(text.trim()) }.getOrNull() ?: return emptyList()
        return outboundObjects(element)
            .mapNotNull(::outboundToNode)
            .distinctBy { "${it.protocol.name}:${it.server}:${it.port}:${it.name}" }
    }

    private fun outboundObjects(element: JsonElement): List<JsonObject> {
        val root = element as? JsonObject
        if (root != null) {
            root.array("outbounds")?.let { return it.mapNotNull { raw -> raw as? JsonObject } }
            if (root.string("type").isNotBlank()) return listOf(root)
        }
        val array = element as? JsonArray ?: return emptyList()
        return array.mapNotNull { it as? JsonObject }
    }

    private fun outboundToNode(outbound: JsonObject): Node? {
        val type = outbound.string("type").lowercase()
        if (type in ignoredTypes) return null
        val protocol = Protocol.fromString(type) ?: return null
        val server = outbound.string("server").ifBlank { outbound.string("address") }
        val port = outbound.displayPort() ?: return null
        if (server.isBlank() || port !in 1..65535) return null

        val canonicalOutbound = when (protocol) {
            Protocol.VLESS -> buildVless(outbound, server, port) ?: return null
            Protocol.VMESS -> buildVmess(outbound, server, port) ?: return null
            Protocol.TROJAN -> buildTrojan(outbound, server, port) ?: return null
            Protocol.SHADOWSOCKS -> buildShadowsocks(outbound, server, port) ?: return null
            Protocol.SOCKS -> buildSocks(outbound, server, port)
            Protocol.HYSTERIA2 -> buildHysteria2(outbound, server, port) ?: return null
            Protocol.TUIC -> buildTuic(outbound, server, port) ?: return null
            Protocol.WIREGUARD -> buildWireGuard(outbound, server, port) ?: return null
        }

        val name = outbound.string("tag")
            .takeUnless { it.isBlank() || it.equals("proxy", ignoreCase = true) }
            ?: "${protocol.name.lowercase()} $server:$port"

        return Node(
            id = UUID.randomUUID().toString(),
            name = name,
            protocol = protocol,
            server = server,
            port = port,
            link = "",
            outbound = canonicalOutbound,
        )
    }

    private fun buildVless(outbound: JsonObject, server: String, port: Int): JsonObject? {
        val uuid = outbound.string("uuid").ifBlank { return null }
        return buildJsonObject {
            put("protocol", "vless")
            putJsonObject("settings") {
                putJsonArray("vnext") {
                    addJsonObject {
                        put("address", server)
                        put("port", port)
                        putJsonArray("users") {
                            addJsonObject {
                                put("id", uuid)
                                put("encryption", outbound.string("encryption").ifBlank { "none" })
                                outbound.string("flow").takeIf(String::isNotBlank)?.let { put("flow", it) }
                                outbound.string("packet_encoding").takeIf(String::isNotBlank)
                                    ?.let { put("packet_encoding", it) }
                            }
                        }
                    }
                }
            }
            put("streamSettings", buildStreamSettings(outbound))
        }
    }

    private fun buildVmess(outbound: JsonObject, server: String, port: Int): JsonObject? {
        val uuid = outbound.string("uuid").ifBlank { return null }
        return buildJsonObject {
            put("protocol", "vmess")
            putJsonObject("settings") {
                putJsonArray("vnext") {
                    addJsonObject {
                        put("address", server)
                        put("port", port)
                        putJsonArray("users") {
                            addJsonObject {
                                put("id", uuid)
                                put("alterId", outbound.int("alter_id") ?: 0)
                                put("security", outbound.string("security").ifBlank { "auto" })
                            }
                        }
                    }
                }
            }
            put("streamSettings", buildStreamSettings(outbound))
        }
    }

    private fun buildTrojan(outbound: JsonObject, server: String, port: Int): JsonObject? {
        val password = outbound.string("password").ifBlank { return null }
        return buildJsonObject {
            put("protocol", "trojan")
            putJsonObject("settings") {
                putJsonArray("servers") {
                    addJsonObject {
                        put("address", server)
                        put("port", port)
                        put("password", password)
                    }
                }
            }
            put("streamSettings", buildStreamSettings(outbound, defaultTLS = true))
        }
    }

    private fun buildShadowsocks(outbound: JsonObject, server: String, port: Int): JsonObject? {
        val method = outbound.string("method").ifBlank { return null }
        val password = outbound.string("password").ifBlank { return null }
        return buildJsonObject {
            put("protocol", "shadowsocks")
            putJsonObject("settings") {
                putJsonArray("servers") {
                    addJsonObject {
                        put("address", server)
                        put("port", port)
                        put("method", method)
                        put("password", password)
                        outbound.string("plugin").takeIf(String::isNotBlank)?.let { put("plugin", it) }
                        outbound.string("plugin_opts").takeIf(String::isNotBlank)?.let { put("plugin_opts", it) }
                    }
                }
            }
            putJsonObject("streamSettings") {
                put("network", "tcp")
            }
        }
    }

    private fun buildSocks(outbound: JsonObject, server: String, port: Int): JsonObject = buildJsonObject {
        put("protocol", "socks")
        putJsonObject("settings") {
            put("address", server)
            put("port", port)
            put("version", outbound.string("version").ifBlank { "5" })
            outbound.string("username").takeIf(String::isNotBlank)?.let { put("username", it) }
            outbound.string("password").takeIf(String::isNotBlank)?.let { put("password", it) }
            outbound.string("network").takeIf(String::isNotBlank)?.let { put("network", it) }
        }
        putJsonObject("streamSettings") {
            put("network", "tcp")
        }
    }

    private fun buildHysteria2(outbound: JsonObject, server: String, port: Int): JsonObject? {
        val password = outbound.string("password").ifBlank { return null }
        return buildJsonObject {
            put("protocol", "hysteria2")
            putJsonObject("settings") {
                put("address", server)
                put("port", port)
                put("password", password)
                outbound.serverPortTokens().takeIf { it.isNotEmpty() }?.let { ports ->
                    putJsonArray("server_ports") {
                        ports.forEach { add(it) }
                    }
                }
                outbound.obj("obfs")?.let { obfs ->
                    putJsonObject("obfs") {
                        put("type", obfs.string("type"))
                        put("password", obfs.string("password"))
                    }
                }
            }
            put("streamSettings", buildStreamSettings(outbound, defaultTLS = true))
        }
    }

    private fun buildTuic(outbound: JsonObject, server: String, port: Int): JsonObject? {
        val uuid = outbound.string("uuid").ifBlank { return null }
        val password = outbound.string("password").ifBlank { return null }
        return buildJsonObject {
            put("protocol", "tuic")
            putJsonObject("settings") {
                put("address", server)
                put("port", port)
                put("uuid", uuid)
                put("password", password)
                listOf(
                    "congestion_control",
                    "udp_relay_mode",
                    "udp_over_stream",
                    "zero_rtt_handshake",
                    "heartbeat",
                ).forEach { key ->
                    outbound.string(key).takeIf(String::isNotBlank)?.let { put(key, it) }
                }
            }
            put("streamSettings", buildStreamSettings(outbound, defaultTLS = true))
        }
    }

    private fun buildWireGuard(outbound: JsonObject, server: String, port: Int): JsonObject? {
        val privateKey = outbound.string("private_key").ifBlank { return null }
        val peerPublicKey = outbound.string("peer_public_key").ifBlank { return null }
        return buildJsonObject {
            put("protocol", "wireguard")
            putJsonObject("settings") {
                put("address", server)
                put("port", port)
                put("private_key", privateKey)
                put("peer_public_key", peerPublicKey)
                outbound.string("pre_shared_key").takeIf(String::isNotBlank)?.let { put("pre_shared_key", it) }
                outbound.array("local_address")?.let { addresses ->
                    putJsonArray("local_address") {
                        addresses.mapNotNull { it.jsonPrimitive.contentOrNull }
                            .filter(String::isNotBlank)
                            .forEach { add(it) }
                    }
                }
                outbound.string("allowed_ips").takeIf(String::isNotBlank)?.let { put("allowed_ips", it) }
                outbound.int("mtu")?.let { put("mtu", it) }
                outbound.array("reserved")?.let { reserved ->
                    putJsonArray("reserved") {
                        reserved.mapNotNull { it.jsonPrimitive.intOrNull }.forEach { add(it) }
                    }
                }
            }
        }
    }

    private fun buildStreamSettings(outbound: JsonObject, defaultTLS: Boolean = false): JsonObject {
        val transport = outbound.obj("transport")
        val network = transport?.string("type")?.lowercase()?.ifBlank { "tcp" } ?: "tcp"
        val tls = outbound.obj("tls")
        val reality = tls?.obj("reality")
        val security = when {
            reality?.bool("enabled") == true || reality?.string("public_key")?.isNotBlank() == true -> "reality"
            tls?.bool("enabled") == true || defaultTLS -> "tls"
            else -> ""
        }

        return buildJsonObject {
            put("network", network)
            if (security.isNotBlank()) {
                put("security", security)
                put(if (security == "reality") "realitySettings" else "tlsSettings", buildTLSSettings(tls, reality))
            }
            transport?.let { putTransport(network, it) }
        }
    }

    private fun buildTLSSettings(tls: JsonObject?, reality: JsonObject?): JsonObject = buildJsonObject {
        tls?.string("server_name")?.takeIf(String::isNotBlank)?.let { put("serverName", it) }
        tls?.obj("utls")?.string("fingerprint")?.takeIf(String::isNotBlank)?.let { put("fingerprint", it) }
        tls?.array("alpn")?.let { alpn ->
            putJsonArray("alpn") {
                alpn.mapNotNull { it.jsonPrimitive.contentOrNull }
                    .filter(String::isNotBlank)
                    .forEach { add(it) }
            }
        }
        if (tls?.bool("insecure") == true) {
            put("allowInsecure", true)
        }
        reality?.string("public_key")?.takeIf(String::isNotBlank)?.let { put("publicKey", it) }
        reality?.string("short_id")?.takeIf(String::isNotBlank)?.let { put("shortId", it) }
    }

    private fun JsonObjectBuilder.putTransport(
        network: String,
        transport: JsonObject,
    ) {
        when (network) {
            "ws" -> putJsonObject("wsSettings") {
                transport.string("path").takeIf(String::isNotBlank)?.let { put("path", it) }
                putJsonObject("headers") {
                    val host = transport.obj("headers")?.string("Host")
                        ?: transport.obj("headers")?.string("host")
                        ?: transport.string("host")
                    host.takeIf(String::isNotBlank)?.let { put("Host", it) }
                }
            }
            "grpc" -> putJsonObject("grpcSettings") {
                put("serviceName", transport.string("service_name").ifBlank { transport.string("serviceName") })
                transport.string("authority").takeIf(String::isNotBlank)?.let { put("authority", it) }
            }
            "http", "h2" -> putJsonObject("httpSettings") {
                transport.string("path").takeIf(String::isNotBlank)?.let { put("path", it) }
                val hosts = transport.array("host")
                    ?.mapNotNull { runCatching { it.jsonPrimitive.contentOrNull }.getOrNull() }
                    ?: transport.array("hosts")
                        ?.mapNotNull { runCatching { it.jsonPrimitive.contentOrNull }.getOrNull() }
                    ?: listOf(transport.string("host")).filter(String::isNotBlank)
                hosts.takeIf { it.isNotEmpty() }?.let {
                    putJsonArray("host") {
                        it.map(String::trim)
                            .filter(String::isNotBlank)
                            .forEach { add(it) }
                    }
                }
            }
            "httpupgrade" -> putJsonObject("httpupgradeSettings") {
                transport.string("path").takeIf(String::isNotBlank)?.let { put("path", it) }
                transport.string("host").takeIf(String::isNotBlank)?.let { put("host", it) }
            }
            "xhttp" -> putJsonObject("xhttpSettings") {
                transport.string("path").takeIf(String::isNotBlank)?.let { put("path", it) }
                transport.string("host").takeIf(String::isNotBlank)?.let { put("host", it) }
                transport.string("mode").takeIf(String::isNotBlank)?.let { put("mode", it) }
            }
        }
    }

    private fun JsonObject.string(key: String): String =
        runCatching { this[key]?.jsonPrimitive?.contentOrNull }.getOrNull().orEmpty()

    private fun JsonObject.int(key: String): Int? =
        runCatching { this[key]?.jsonPrimitive?.intOrNull }.getOrNull()

    private fun JsonObject.bool(key: String): Boolean? =
        runCatching { this[key]?.jsonPrimitive?.booleanOrNull }.getOrNull()

    private fun JsonObject.obj(key: String): JsonObject? =
        runCatching { this[key]?.jsonObject }.getOrNull()

    private fun JsonObject.array(key: String): JsonArray? =
        runCatching { this[key]?.jsonArray }.getOrNull()

    private fun JsonObject.displayPort(): Int? {
        int("server_port")?.let { return it }
        int("port")?.let { return it }
        return serverPortTokens()
            .firstNotNullOfOrNull { token -> Regex("""\d+""").find(token)?.value?.toIntOrNull() }
    }

    private fun JsonObject.serverPortTokens(): List<String> {
        string("server_ports").takeIf(String::isNotBlank)?.let { return listOf(it) }
        return array("server_ports")
            ?.mapNotNull { runCatching { it.jsonPrimitive.contentOrNull }.getOrNull() }
            ?.map(String::trim)
            ?.filter(String::isNotBlank)
            .orEmpty()
    }
}
