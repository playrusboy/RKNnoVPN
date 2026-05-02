package com.rknnovpn.panel.ipc

import android.util.Log
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.delay
import kotlinx.coroutines.suspendCancellableCoroutine
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.coroutines.withContext
import kotlinx.coroutines.withTimeoutOrNull
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.booleanOrNull
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.contentOrNull
import kotlinx.serialization.json.int
import kotlinx.serialization.json.intOrNull
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import kotlinx.serialization.json.put
import java.io.BufferedReader
import java.io.BufferedWriter
import java.io.IOException
import java.io.InputStream
import java.io.InputStreamReader
import java.io.InterruptedIOException
import java.io.OutputStreamWriter
import java.nio.charset.StandardCharsets
import java.util.concurrent.TimeUnit
import javax.inject.Inject
import javax.inject.Singleton
import kotlin.concurrent.thread
import kotlin.coroutines.resume
import kotlin.coroutines.resumeWithException

/**
 * Low-level executor that shells out to the `daemonctl` binary via `su`.
 *
 * Every call:
 * 1. Builds a JSON-RPC-style request for `daemonctl <method>`
 * 2. Streams optional JSON params via stdin to avoid argv length limits
 * 3. Runs it under `su -c "..."`
 * 4. Captures stdout, parses as JSON-RPC or typed daemon JSON
 * 5. Maps the response to [DaemonctlResult]
 *
 * Thread-safety: all calls are dispatched on [Dispatchers.IO].
 * Timeout default: 5 000 ms, configurable per-call.
 */
@Singleton
class DaemonctlExecutor @Inject constructor() {

    private val daemonctlPath = "/data/adb/modules/rknnovpn/bin/daemonctl"
    private val rootCommands = listOf(
        RootCommand("su", "su", "-c"),
        RootCommand("/system/bin/su", "/system/bin/su", "-c"),
        RootCommand("/system/xbin/su", "/system/xbin/su", "-c"),
        RootCommand("/sbin/su", "/sbin/su", "-c"),
        RootCommand("/su/bin/su", "/su/bin/su", "-c"),
        RootCommand("/vendor/bin/su", "/vendor/bin/su", "-c"),
        RootCommand("/data/adb/ksu/bin/su", "/data/adb/ksu/bin/su", "-c"),
        RootCommand("/system/bin/magisk su", "/system/bin/magisk", "su", "-c"),
        RootCommand("/sbin/magisk su", "/sbin/magisk", "su", "-c"),
    )

    private val json = Json {
        ignoreUnknownKeys = true
        isLenient = true
        coerceInputValues = true
    }

    companion object {
        private const val TAG = "DaemonctlExecutor"
        private const val DEFAULT_TIMEOUT_MS = 5_000L
        private const val DAEMON_PATH = "/data/adb/modules/rknnovpn/bin/daemon"
        private const val DAEMON_PID_PATH = "/data/adb/modules/rknnovpn/run/daemon.pid"
        private const val REPAIR_COOLDOWN_MS = 15_000L
        private const val REPAIR_RETRY_DELAY_MS = 1_500L
        private const val REPAIR_RETRY_TIMEOUT_MS = 5_000L
        private const val REPAIR_TOTAL_WAIT_MS = 120_000L
        private const val BRIDGE_RETRY_COOLDOWN_MS = 15_000L
        // Keep the single bridge lane for short, frequent calls; long RPCs use
        // one-shot daemonctl so they cannot block status/compat polling.
        private val BRIDGE_METHODS = setOf(
            "app.resolveUid",
            "backend.status",
            "compat.check",
            "config-list",
            "ipc.contract",
            "profile.get",
            "profile.dns.patch",
            "profile.inbound.patch",
            "profile.node.remove",
            "profile.node.upsert",
            "profile.patch",
            "profile.routing.patch",
            "profile.setActiveNode",
            "version",
        )

        /** Exit code returned by `su` when the user denies the superuser prompt. */
        private const val SU_DENIED_EXIT_CODE = 13
    }

    @Volatile
    private var lastRepairAttemptAtMs: Long = 0L
    @Volatile
    private var bridgeDisabledUntilMs: Long = 0L
    private val bridgeMutex = Mutex()
    private var bridgeSession: BridgeSession? = null
    private var nextBridgeRequestId: Int = 1

    /**
     * Execute a single daemonctl JSON-RPC method.
     *
     * @param method  The method name (e.g. "backend.status", "profile.get").
     * @param params  Optional parameter object.
     * @param timeoutMs  Maximum wall-clock time for the command.
     * @return A [DaemonctlResult] that is never null.
     */
    suspend fun execute(
        method: String,
        params: JsonObject = emptyJsonObject(),
        timeoutMs: Long = DEFAULT_TIMEOUT_MS,
        allowModuleRepair: Boolean = true,
        allowBridge: Boolean = true,
    ): DaemonctlResult = withContext(Dispatchers.IO) {
        try {
            val result = withTimeoutOrNull(timeoutMs) {
                executeRaw(method, params, allowBridge)
            }
            val checkedResult = withModuleInstallStateHint(
                result ?: DaemonctlResult.Timeout(timeoutMs, method),
                allowModuleRepair,
            )
            if (
                allowModuleRepair &&
                checkedResult is DaemonctlResult.DaemonUnavailable &&
                isRepairableDaemonUnavailable(checkedResult.reason) &&
                triggerModuleDaemonRepair(checkedResult.reason)
            ) {
                return@withContext retryAfterModuleDaemonRepair(method, params, timeoutMs)
            }
            checkedResult
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            Log.e(TAG, "execute($method) failed unexpectedly", e)
            DaemonctlResult.UnexpectedError(e)
        }
    }

    // ---- internals ----

    private suspend fun retryAfterModuleDaemonRepair(
        method: String,
        params: JsonObject,
        timeoutMs: Long,
    ): DaemonctlResult {
        val deadline = System.currentTimeMillis() + REPAIR_TOTAL_WAIT_MS
        var lastResult: DaemonctlResult = DaemonctlResult.DaemonUnavailable("daemon repair is still starting")
        while (System.currentTimeMillis() < deadline) {
            delay(REPAIR_RETRY_DELAY_MS)
            val retry = withTimeoutOrNull(timeoutMs.coerceAtLeast(REPAIR_RETRY_TIMEOUT_MS)) {
                executeRaw(method, params, allowBridge = false)
            } ?: DaemonctlResult.Timeout(timeoutMs, method)
            lastResult = retry
            if (retry !is DaemonctlResult.DaemonUnavailable && retry !is DaemonctlResult.Timeout) {
                return retry
            }
        }
        Log.w(TAG, "Daemon repair did not become ready within ${REPAIR_TOTAL_WAIT_MS}ms")
        return lastResult
    }

    private suspend fun executeRaw(
        method: String,
        params: JsonObject,
        allowBridge: Boolean,
    ): DaemonctlResult {
        val paramsJson = params.toString()
        val requestFrameBytes = jsonRpcRequestFrameByteSize(method, params)
        if (requestFrameBytes > IPC_MAX_FRAME_BYTES) {
            return DaemonctlResult.Error(
                code = -32602,
                message = IPC_PAYLOAD_TOO_LARGE_MESSAGE,
                details = buildJsonObject {
                    put("requestBytes", requestFrameBytes)
                    put("maxFrameBytes", IPC_MAX_FRAME_BYTES)
                },
            )
        }
        val useStdin = params.isNotEmpty()
        if (allowBridge) {
            executeViaBridge(method, params)?.let { return it }
        }
        val commandString = buildDaemonctlCommand(method, paramsJson, useStdin, params.isEmpty())
        Log.d(
            TAG,
            ">>> daemonctl method=$method params=${if (params.isEmpty()) "none" else "redacted"}"
        )

        return executeRootCommandWithFallbacks(
            commandString = commandString,
            stdinParams = if (useStdin) paramsJson else null,
            method = method,
        )
    }

    private suspend fun executeViaBridge(
        method: String,
        params: JsonObject,
    ): DaemonctlResult? {
        if (!canUseBridge(method, params)) return null
        return bridgeMutex.withLock {
            val now = System.currentTimeMillis()
            if (now < bridgeDisabledUntilMs) return@withLock null
            val session = activeBridgeSession()
                ?: startBridgeSessionOrNull()
                ?: return@withLock null
            val requestId = nextBridgeRequestId++
            val frame = buildJsonObject {
                put("jsonrpc", "2.0")
                put("id", requestId)
                put("method", method)
                if (params.isNotEmpty()) {
                    put("params", params)
                }
            }.toString()
            try {
                Log.d(
                    TAG,
                    ">>> daemonctl bridge method=$method params=${if (params.isEmpty()) "none" else "redacted"}"
                )
                val stdout = transactBridge(session, frame)
                if (!stdout.trimStart().startsWith("{")) {
                    throw IOException("daemonctl bridge returned non-JSON output")
                }
                Log.d(TAG, "<<< daemonctl bridge method=$method")
                val result = parseResponse(
                    exitCode = 0,
                    stdout = stdout,
                    stderr = "",
                    method = method,
                    transport = DaemonctlTransport.BRIDGE,
                    expectedJsonRpcId = requestId,
                )
                if (result is DaemonctlResult.Error && result.isBridgeProtocolFailure()) {
                    throw IOException(result.message)
                }
                result
            } catch (e: Exception) {
                Log.d(TAG, "daemonctl bridge unavailable for $method: ${e.message}")
                closeBridgeSession("bridge unavailable")
                bridgeDisabledUntilMs = System.currentTimeMillis() + BRIDGE_RETRY_COOLDOWN_MS
                null
            }
        }
    }

    private fun canUseBridge(method: String, params: JsonObject): Boolean {
        if (method in BRIDGE_METHODS) return true
        if (method == "profile.patch") {
            return params.keys.all { it == "activeNodeId" || it == "reload" }
        }
        return false
    }

    fun disableBridge(reason: String) {
        Log.d(TAG, "daemonctl bridge disabled: $reason")
        closeBridgeSession(reason)
        bridgeDisabledUntilMs = System.currentTimeMillis() + BRIDGE_RETRY_COOLDOWN_MS
    }

    private fun activeBridgeSession(): BridgeSession? =
        bridgeSession?.takeIf { it.process.isAlive }

    private fun startBridgeSessionOrNull(): BridgeSession? {
        val commandString = buildDaemonctlBridgeCommand()
        return try {
            val process = startRootProcess(commandString)
            val session = BridgeSession(
                process = process,
                stdin = BufferedWriter(OutputStreamWriter(process.outputStream, StandardCharsets.UTF_8)),
                stdout = BufferedReader(InputStreamReader(process.inputStream, StandardCharsets.UTF_8)),
            )
            session.stderrReader = thread(start = true, name = "daemonctl-bridge-stderr") {
                session.stderr = readStreamSafely(process.errorStream, "bridge-stderr")
            }
            bridgeSession = session
            session
        } catch (e: Exception) {
            Log.d(TAG, "daemonctl bridge launch failed: ${e.message}")
            bridgeDisabledUntilMs = System.currentTimeMillis() + BRIDGE_RETRY_COOLDOWN_MS
            null
        }
    }

    private fun buildDaemonctlBridgeCommand(): String {
        val prelude = namespaceAwareDaemonctlPrelude()
        val ctl = daemonctlShellRef()
        return "$prelude RKNNOVPN_DAEMONCTL_RAW=1 $ctl --raw bridge"
    }

    private suspend fun transactBridge(session: BridgeSession, frame: String): String =
        suspendCancellableCoroutine { cont ->
            cont.invokeOnCancellation {
                closeBridgeSession("bridge cancelled")
            }
            try {
                session.stdin.write(frame)
                session.stdin.newLine()
                session.stdin.flush()
                val line = session.stdout.readLine()
                    ?: throw IOException(session.stderr.ifBlank { "daemonctl bridge closed stdout" })
                if (cont.isActive) {
                    cont.resume(line)
                }
            } catch (e: Exception) {
                if (cont.isActive) {
                    cont.resumeWithException(e)
                }
            }
        }

    private fun closeBridgeSession(reason: String) {
        val session = bridgeSession ?: return
        bridgeSession = null
        terminateProcess(session.process, reason)
    }

    private fun buildDaemonctlCommand(
        method: String,
        paramsJson: String,
        useStdin: Boolean,
        paramsEmpty: Boolean,
    ): String {
        val prelude = namespaceAwareDaemonctlPrelude()
        val ctl = daemonctlShellRef()
        val machineMode = "RKNNOVPN_DAEMONCTL_RAW=1"
        return when {
            paramsEmpty -> "$prelude $machineMode $ctl --raw ${shellQuote(method)}"
            useStdin -> "$prelude $machineMode RKNNOVPN_STDIN_PARAMS=1 $ctl --raw ${shellQuote(method)}"
            else -> "$prelude $machineMode $ctl --raw ${shellQuote(method)} ${shellQuote(paramsJson)}"
        }
    }

    private fun namespaceAwareDaemonctlPrelude(): String {
        val dollar = "$"
        return "ctl=${shellQuote(daemonctlPath)}; " +
            "pid=''; " +
            "if [ -r ${shellQuote(DAEMON_PID_PATH)} ]; then " +
            "IFS= read -r pid < ${shellQuote(DAEMON_PID_PATH)}; " +
            "pid=${dollar}{pid%%[!0-9]*}; " +
            "fi; " +
            "if [ -n \"${dollar}pid\" ] && [ -d \"/proc/${dollar}pid\" ]; then " +
            "exe=${dollar}(readlink \"/proc/${dollar}pid/exe\" 2>/dev/null || true); " +
            "cmd=${dollar}(tr '\\000' ' ' < \"/proc/${dollar}pid/cmdline\" 2>/dev/null || true); " +
            "case \"${dollar}exe|${dollar}cmd\" in *${shellQuote(DAEMON_PATH)}*) " +
            "if [ -x \"/proc/${dollar}pid/root$daemonctlPath\" ]; then " +
            "ctl=\"/proc/${dollar}pid/root$daemonctlPath\"; " +
            "fi ;; " +
            "esac; " +
            "fi;"
    }

    private fun daemonctlShellRef(): String = "\"\$ctl\""

    private suspend fun executeRootCommandWithFallbacks(
        commandString: String,
        stdinParams: String?,
        method: String,
    ): DaemonctlResult {
        val rootUnavailableReasons = mutableListOf<String>()
        for (candidate in rootCommands) {
            val result = executeRootCommand(candidate, commandString, stdinParams, method)
            if (result is DaemonctlResult.RootDenied && shouldTryNextRootCommand(result.reason)) {
                rootUnavailableReasons += "${candidate.label}: ${result.reason}"
                continue
            }
            return result
        }
        return DaemonctlResult.RootDenied(
            "root command is not available: ${rootUnavailableReasons.joinToString("; ")}"
        )
    }

    private suspend fun executeRootCommand(
        candidate: RootCommand,
        commandString: String,
        stdinParams: String?,
        method: String,
    ): DaemonctlResult = suspendCancellableCoroutine { cont ->
        var process: Process? = null
        try {
            process = Runtime.getRuntime().exec(candidate.argv(commandString))
            cont.invokeOnCancellation {
                terminateProcess(process, "cancelled")
            }
            Log.d(TAG, "daemonctl root command=${candidate.label}")

            writeParamsSafely(process, stdinParams)

            var stdout = ""
            var stderr = ""
            val stdoutReader = thread(start = true, name = "daemonctl-stdout") {
                stdout = readStreamSafely(process.inputStream, "stdout")
            }
            val stderrReader = thread(start = true, name = "daemonctl-stderr") {
                stderr = readStreamSafely(process.errorStream, "stderr")
            }

            val exitCode = process.waitFor()
            stdoutReader.join()
            stderrReader.join()

            Log.d(TAG, "<<< daemonctl method=$method root=${candidate.label} exit=$exitCode")
            if (cont.isActive) {
                cont.resume(parseResponse(exitCode, stdout, stderr, method))
            }
        } catch (e: Exception) {
            terminateProcess(process, "failed")
            if (cont.isActive) {
                cont.resume(classifyLaunchException(e))
            }
        }
    }

    private fun classifyLaunchException(e: Exception): DaemonctlResult {
        if (e is RootCommandUnavailableException) {
            return DaemonctlResult.RootDenied("root command is not available: ${e.summary}")
        }
        if (e is IOException && e.message.orEmpty().contains("su", ignoreCase = true)) {
            return DaemonctlResult.RootDenied("su executable is not available: ${e.message}")
        }
        return DaemonctlResult.UnexpectedError(e)
    }

    private fun shouldTryNextRootCommand(reason: String): Boolean {
        val text = reason.lowercase()
        if (text.contains("permission denied") || text.contains("denied") || text.contains("code 13")) {
            return false
        }
        return text.contains("not available") ||
            text.contains("no such file") ||
            text.contains("not found") ||
            text.contains("no su program detected") ||
            text.contains("inaccessible or not found")
    }

    private fun startRootProcess(commandString: String): Process {
        val failures = mutableListOf<String>()
        var firstError: IOException? = null
        for (candidate in rootCommands) {
            try {
                val process = Runtime.getRuntime().exec(candidate.argv(commandString))
                Log.d(TAG, "daemonctl root command=${candidate.label}")
                return process
            } catch (e: IOException) {
                if (firstError == null) firstError = e
                failures += "${candidate.label}: ${e.message.orEmpty()}"
                Log.d(TAG, "root command ${candidate.label} is unavailable: ${e.message}")
            }
        }
        throw RootCommandUnavailableException(
            failures.joinToString("; ").ifBlank { "no executable candidates" },
            firstError,
        )
    }

    private fun triggerModuleDaemonRepair(reason: String): Boolean {
        val now = System.currentTimeMillis()
        if (now - lastRepairAttemptAtMs < REPAIR_COOLDOWN_MS) {
            Log.d(TAG, "Skipping daemon repair; cooldown is active")
            return false
        }
        lastRepairAttemptAtMs = now

        val commandString = buildDaemonctlCommand(
            method = "module.repair",
            paramsJson = "{}",
            useStdin = false,
            paramsEmpty = true,
        )
        var process: Process? = null
        return try {
            Log.w(TAG, "Daemon IPC unavailable; starting module repair: ${reason.take(160)}")
            process = startRootProcess(commandString)
            if (!process.waitFor(2, TimeUnit.SECONDS)) {
                terminateProcess(process, "app repair launch timeout")
                false
            } else {
                val stdout = readStreamSafely(process.inputStream, "module-repair-stdout")
                val stderr = readStreamSafely(process.errorStream, "module-repair-stderr")
                val result = parseResponse(process.exitValue(), stdout, stderr, "module.repair")
                val status = (result as? DaemonctlResult.Success)
                    ?.data
                    ?.jsonObject
                    ?.get("status")
                    ?.jsonPrimitive
                    ?.contentOrNull
                    .orEmpty()
                val ok = status == "repair_started" || status == "repair_running"
                if (!ok) {
                    Log.w(TAG, "Module repair did not start: status=$status result=$result")
                }
                ok
            }
        } catch (e: Exception) {
            Log.w(TAG, "Module repair launch failed: ${e.message}")
            false
        } finally {
            terminateProcess(process, "app repair launch")
        }
    }

    private fun isRepairableDaemonUnavailable(reason: String): Boolean {
        val text = reason.lowercase()
        return !text.contains("reboot required") &&
            !text.contains("module is disabled") &&
            !text.contains("module is marked for removal") &&
            !text.contains("service.sh is missing")
    }

    private fun withModuleInstallStateHint(
        result: DaemonctlResult,
        allowModuleRepair: Boolean,
    ): DaemonctlResult {
        if (result !is DaemonctlResult.DaemonUnavailable && result !is DaemonctlResult.DaemonNotFound) {
            return result
        }
        return when (probeModuleInstallState()) {
            "pending_update" -> DaemonctlResult.DaemonUnavailable(
                "module update is staged; reboot required"
            )
            "pending_remove" -> DaemonctlResult.DaemonUnavailable(
                "module is marked for removal; reboot required"
            )
            "disabled" -> DaemonctlResult.DaemonUnavailable("module is disabled")
            "missing" -> DaemonctlResult.DaemonNotFound(daemonctlPath)
            "service_missing" -> DaemonctlResult.DaemonUnavailable("service.sh is missing")
            "daemonctl_missing" -> DaemonctlResult.DaemonNotFound(daemonctlPath)
            "repair_running" -> DaemonctlResult.DaemonUnavailable("module repair is running")
            "repair_failed" -> DaemonctlResult.DaemonUnavailable("module repair failed")
            "missing_profile", "no_runtime_profile" -> if (!allowModuleRepair && result is DaemonctlResult.DaemonUnavailable) {
                DaemonctlResult.DaemonUnavailable(NO_RUNTIME_PROFILE_REASON)
            } else {
                result
            }
            else -> result
        }
    }

    private fun probeModuleInstallState(): String {
        val commandString = buildDaemonctlCommand(
            method = "module.state",
            paramsJson = "{}",
            useStdin = false,
            paramsEmpty = true,
        )
        var process: Process? = null
        return try {
            process = startRootProcess(commandString)
            if (!process.waitFor(1, TimeUnit.SECONDS)) {
                terminateProcess(process, "module state probe timeout")
                return "unknown"
            }
            val stdout = readStreamSafely(process.inputStream, "module-state-stdout")
            val stderr = readStreamSafely(process.errorStream, "module-state-stderr")
            val result = parseResponse(process.exitValue(), stdout, stderr, "module.state")
            val state = (result as? DaemonctlResult.Success)
                ?.data
                ?.jsonObject
            val status = state
                ?.get("status")
                ?.jsonPrimitive
                ?.contentOrNull
                ?.ifBlank { "unknown" }
                ?: "unknown"
            val repairStatus = state
                ?.get("repairStatus")
                ?.jsonPrimitive
                ?.contentOrNull
                .orEmpty()
            when (repairStatus) {
                "repair_running", "repair_failed" -> repairStatus
                else -> status
            }
        } catch (e: Exception) {
            Log.d(TAG, "Module install state probe failed: ${e.message}")
            "unknown"
        } finally {
            terminateProcess(process, "module state probe")
        }
    }

    private fun terminateProcess(process: Process?, reason: String) {
        if (process == null) return
        try {
            process.outputStream.close()
        } catch (_: IOException) {
        }
        try {
            process.inputStream.close()
        } catch (_: IOException) {
        }
        try {
            process.errorStream.close()
        } catch (_: IOException) {
        }
        try {
            process.destroy()
            if (!process.waitFor(300, TimeUnit.MILLISECONDS)) {
                Log.w(TAG, "daemonctl process still alive after $reason; forcing kill")
                process.destroyForcibly()
                process.waitFor(1, TimeUnit.SECONDS)
            }
        } catch (e: Exception) {
            Log.d(TAG, "daemonctl process cleanup after $reason failed: ${e.message}")
        }
    }

    private fun writeParamsSafely(process: Process, paramsJson: String?) {
        process.outputStream.use { output ->
            if (!paramsJson.isNullOrEmpty()) {
                output.write(paramsJson.toByteArray(StandardCharsets.UTF_8))
                output.write('\n'.code)
                output.flush()
            }
        }
    }

    private fun readStreamSafely(stream: InputStream, streamName: String): String {
        return try {
            BufferedReader(InputStreamReader(stream)).use { it.readText().trim() }
        } catch (e: InterruptedIOException) {
            Log.d(TAG, "daemonctl $streamName reader interrupted")
            ""
        } catch (e: IOException) {
            Log.d(TAG, "daemonctl $streamName reader closed: ${e.message}")
            ""
        }
    }

    private fun parseResponse(
        exitCode: Int,
        stdout: String,
        stderr: String,
        method: String,
        transport: DaemonctlTransport = DaemonctlTransport.ONE_SHOT,
        expectedJsonRpcId: Int? = null,
    ): DaemonctlResult {
        // Old daemonctl binary that doesn't know the requested command.
        // It prints "error: unknown command ..." to stderr and exits 1.
        // Detect this before trying to parse stdout (which may contain
        // the usage text and fail JSON parsing).
        if (exitCode != 0 &&
            stderr.contains("unknown command", ignoreCase = true)
        ) {
            return DaemonctlResult.Error(
                code = DaemonClientErrorCodes.METHOD_NOT_FOUND,
                message = "method not found: $method",
                details = methodNotFoundDetails(method),
                transport = transport,
            )
        }

        var stdoutParseError: Exception? = null
        if (stdout.isNotBlank()) {
            try {
                val jsonElement = json.parseToJsonElement(stdout)
                return parseJsonResponse(jsonElement, exitCode, stderr, method, transport, expectedJsonRpcId)
            } catch (e: Exception) {
                stdoutParseError = e
                Log.w(TAG, "Failed to parse stdout as JSON: ${stdout.take(100)}", e)
            }
        }

        // su denied. Valid daemon stdout is handled above, so stderr from an
        // inner daemon/runtime error cannot mask the typed response as RootDenied.
        if (exitCode == SU_DENIED_EXIT_CODE ||
            stderr.contains("permission denied", ignoreCase = true) ||
            stderr.contains("not found", ignoreCase = true) && stderr.contains("su")
        ) {
            return DaemonctlResult.RootDenied(stderr.ifBlank { "su exited with code $exitCode" })
        }

        // daemonctl binary missing
        if (stderr.contains("not found", ignoreCase = true) &&
            stderr.contains("daemonctl", ignoreCase = true)
        ) {
            return DaemonctlResult.DaemonNotFound(daemonctlPath)
        }

        if (looksLikeDaemonSocketFailure(stdout, stderr)) {
            return DaemonctlResult.DaemonUnavailable(
                stderr.ifBlank { stdout }.ifBlank { "daemon IPC socket is unavailable" }
            )
        }

        if (stdoutParseError != null) {
            return DaemonctlResult.Error(
                code = -32700,
                message = "Invalid JSON from daemon: ${stdoutParseError.message}",
                transport = transport,
            )
        }

        // No output at all. New daemons always return a typed IPC envelope;
        // a silent success would make mutating operations look safer than they are.
        return DaemonctlResult.Error(
            code = if (exitCode == 0) -32600 else exitCode,
            message = stderr.ifBlank {
                "Daemon response for $method is missing the typed IPC envelope"
            },
            transport = transport,
        )
    }

    private fun parseJsonResponse(
        jsonElement: JsonElement,
        exitCode: Int,
        stderr: String,
        method: String,
        transport: DaemonctlTransport,
        expectedJsonRpcId: Int?,
    ): DaemonctlResult {
        val obj = try {
            jsonElement.jsonObject
        } catch (e: Exception) {
            return DaemonctlResult.Error(
                code = -32700,
                message = "Expected JSON object, got: ${jsonElement::class.simpleName}",
                transport = transport,
            )
        }

        obj.daemonEnvelopeOrNull()?.let { envelope ->
            return resultFromEnvelope(envelope, transport)
        }

        if (expectedJsonRpcId != null && obj["jsonrpc"] != null) {
            val actualId = obj["id"]?.jsonPrimitive?.intOrNull
            if (actualId != expectedJsonRpcId) {
                return DaemonctlResult.Error(
                    code = -32600,
                    message = "Daemon bridge response id $actualId != request id $expectedJsonRpcId",
                    details = jsonElement,
                    transport = transport,
                )
            }
        }

        // JSON-RPC error field present. Error responses must carry the typed
        // daemon envelope in error.data so callers can inspect stable details
        // such as configSaved/runtimeApplied/activeOperation.
        val errorObj = obj["error"]
        if (errorObj != null) {
            return try {
                val errJson = errorObj.jsonObject
                val envelope = errJson["data"]?.daemonEnvelopeOrNull()
                    ?: return DaemonctlResult.Error(
                        code = errJson["code"]?.jsonPrimitive?.int ?: -32600,
                        message = "Daemon error for $method is missing the typed IPC envelope",
                        details = errJson["data"],
                        transport = transport,
                    )
                val envelopeError = envelope?.get("error")?.jsonObject
                DaemonctlResult.Error(
                    code = errJson["code"]?.jsonPrimitive?.int ?: -1,
                    message = envelopeError?.get("message")?.jsonPrimitive?.contentOrNull
                        ?: errJson["message"]?.jsonPrimitive?.content
                        ?: "Unknown daemon error",
                    details = envelopeError?.get("details") ?: errJson["data"],
                    envelope = envelope,
                    transport = transport,
                )
            } catch (e: Exception) {
                DaemonctlResult.Error(
                    code = -1,
                    message = errorObj.toString(),
                    transport = transport,
                )
            }
        }

        if (exitCode != 0) {
            return DaemonctlResult.Error(
                code = exitCode,
                message = stderr.ifBlank { "daemonctl exited with code $exitCode" },
                details = jsonElement,
                transport = transport,
            )
        }

        // JSON-RPC result field
        val resultField = obj["result"]
        if (resultField != null) {
            val envelope = resultField.daemonEnvelopeOrNull()
            if (envelope != null) {
                return resultFromEnvelope(envelope, transport)
            }
            return DaemonctlResult.Error(
                code = -32600,
                message = "Daemon result for $method is missing the typed IPC envelope",
                details = resultField,
                transport = transport,
            )
        }

        return DaemonctlResult.Error(
            code = -32600,
            message = "Daemon response for $method is missing result/error typed IPC envelope",
            details = jsonElement,
            transport = transport,
        )
    }

    private fun DaemonctlResult.Error.isBridgeProtocolFailure(): Boolean =
        transport == DaemonctlTransport.BRIDGE && (code == -32600 || code == -32700)

    private fun shellQuote(value: String): String {
        if (value.isEmpty()) return "''"
        return "'" + value.replace("'", "'\"'\"'") + "'"
    }

    private fun methodNotFoundDetails(method: String): JsonObject = buildJsonObject {
        put("requestedMethod", method)
        canonicalReplacement(method)?.let { put("replacement", it) }
    }

    private fun canonicalReplacement(method: String): String? = when (method) {
        "config.import" -> "Use config-import for full daemon config import, or profile.importNodes for Paste URI/node imports."
        "network.reset" -> "Use backend.reset."
        "node.test" -> "Use diagnostics.testNodes."
        "self.check" -> "Use self-check."
        "status" -> "Use backend.status."
        "start" -> "Use backend.start."
        "stop" -> "Use backend.stop."
        "reload" -> "Use backend.restart."
        "health" -> "Use diagnostics.health."
        "subscription-fetch" -> "Use subscription.preview or subscription.refresh."
        else -> null
    }

    private fun looksLikeDaemonSocketFailure(stdout: String, stderr: String): Boolean {
        val text = "$stderr\n$stdout"
        return text.contains("cannot connect to daemon", ignoreCase = true) ||
            text.contains("daemon.sock", ignoreCase = true) &&
            (
                text.contains("no such file or directory", ignoreCase = true) ||
                    text.contains("connection refused", ignoreCase = true) ||
                    text.contains("is daemon running", ignoreCase = true)
                )
    }

    private fun JsonElement.daemonEnvelopeOrNull(): JsonObject? {
        val obj = runCatching { jsonObject }.getOrNull() ?: return null
        return obj.daemonEnvelopeOrNull()
    }

    private fun JsonObject.daemonEnvelopeOrNull(): JsonObject? {
        val hasEnvelopeStatus = this["ok"]?.jsonPrimitive?.booleanOrNull != null
        val hasEnvelopePayload = containsKey("result") || containsKey("error")
        return if (hasEnvelopeStatus && hasEnvelopePayload) this else null
    }

    private fun resultFromEnvelope(envelope: JsonObject, transport: DaemonctlTransport): DaemonctlResult {
        val ok = envelope["ok"]?.jsonPrimitive?.booleanOrNull ?: true
        return if (ok) {
            DaemonctlResult.Success(envelope["result"] ?: JsonNull, envelope, transport)
        } else {
            val envelopeError = envelope["error"]?.jsonObject
            DaemonctlResult.Error(
                code = envelopeError?.get("rpcCode")?.jsonPrimitive?.intOrNull ?: -1,
                message = envelopeError?.get("message")?.jsonPrimitive?.contentOrNull
                    ?: "Unknown daemon error",
                details = envelopeError?.get("details"),
                envelope = envelope,
                transport = transport,
            )
        }
    }

}

private class RootCommand(
    val label: String,
    private vararg val prefix: String,
) {
    fun argv(commandString: String): Array<String> =
        prefix.toList().plus(commandString).toTypedArray()
}

private class RootCommandUnavailableException(
    val summary: String,
    cause: Throwable?,
) : IOException("No root command candidate could be launched: $summary", cause)

private class BridgeSession(
    val process: Process,
    val stdin: BufferedWriter,
    val stdout: BufferedReader,
) {
    @Volatile
    var stderr: String = ""
    lateinit var stderrReader: Thread
}

/** Convenience alias for an empty JsonObject. */
fun emptyJsonObject(): JsonObject = JsonObject(emptyMap())
