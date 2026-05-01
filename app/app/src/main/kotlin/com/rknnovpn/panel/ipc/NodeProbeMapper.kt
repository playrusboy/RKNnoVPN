package com.rknnovpn.panel.ipc

import com.rknnovpn.panel.model.NodeProbeResultV2

internal fun List<NodeProbeResultV2>.toNodeTestInfo(url: String): NodeTestInfo =
    NodeTestInfo(
        url = url,
        results = map { probe ->
            val tcpStatus = probe.tcpStatus.ifBlank {
                if (probe.tcpDirect != null) "ok" else "fail"
            }
            val urlStatus = probe.urlStatus.ifBlank {
                if (probe.tunnelDelay != null) "ok" else "not_run"
            }
            val verdict = probe.verdict.ifBlank {
                when {
                    tcpStatus == "ok" && urlStatus != "ok" -> "unusable"
                    urlStatus == "ok" && probe.tunnelDelay != null -> "usable"
                    else -> "unknown"
                }
            }
            NodeTestResult(
                id = probe.id,
                name = probe.name,
                tag = probe.id,
                server = probe.server,
                port = probe.port,
                protocol = probe.protocol,
                tcpMs = probe.tcpDirect?.toInt(),
                urlMs = probe.tunnelDelay?.toInt(),
                throughputBps = probe.throughputBps,
                tcpStatus = tcpStatus,
                urlStatus = urlStatus,
                verdict = verdict,
                status = null,
                tcpError = if (probe.errorClass == "tcp_direct_failed") probe.errorClass else null,
                urlError = when (probe.errorClass) {
                    "tunnel_delay_failed",
                    "tunnel_unavailable",
                    "dns_bootstrap_failed",
                    "runtime_not_ready",
                    "runtime_degraded",
                    "proxy_dns_unavailable",
                    "outbound_url_failed",
                    "http_helper_unavailable",
                    "api_disabled",
                    "api_unavailable",
                    "outbound_missing",
                    "tls_handshake_failed" -> probe.errorClass
                    else -> null
                },
            )
        },
    )
