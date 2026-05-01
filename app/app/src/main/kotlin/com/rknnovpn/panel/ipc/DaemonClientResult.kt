package com.rknnovpn.panel.ipc

import kotlinx.serialization.json.JsonElement

internal object DaemonClientErrorCodes {
    const val METHOD_NOT_FOUND = -32601
    const val CONFIG_ERROR = -32003
    const val RUNTIME_BUSY = -32004
    const val COMPATIBILITY = -32090
}

sealed class DaemonClientResult<out T> {
    data class Ok<T>(val data: T) : DaemonClientResult<T>()
    data class DaemonError(
        val code: Int,
        val message: String,
        val details: JsonElement? = null,
        val envelope: JsonElement? = null,
    ) : DaemonClientResult<Nothing>()
    data class RootDenied(val reason: String) : DaemonClientResult<Nothing>()
    data class Timeout(val method: String) : DaemonClientResult<Nothing>()
    data class DaemonNotFound(val path: String) : DaemonClientResult<Nothing>()
    data class DaemonUnavailable(val reason: String) : DaemonClientResult<Nothing>()
    data class ParseError(val raw: String, val cause: Throwable) : DaemonClientResult<Nothing>()
    data class Failure(val throwable: Throwable) : DaemonClientResult<Nothing>()

    val isOk: Boolean get() = this is Ok

    fun dataOrNull(): T? = (this as? Ok)?.data

    fun dataOrThrow(): T = when (this) {
        is Ok -> data
        is DaemonError -> throw DaemonctlException("Daemon error $code: $message")
        is RootDenied -> throw DaemonctlException("Root denied: $reason")
        is Timeout -> throw DaemonctlException("Timeout on method: $method")
        is DaemonNotFound -> throw DaemonctlException("Daemon not found at: $path")
        is DaemonUnavailable -> throw DaemonctlException("Daemon unavailable: $reason")
        is ParseError -> throw DaemonctlException("Parse error on: ${raw.take(100)}", cause)
        is Failure -> throw DaemonctlException("Unexpected failure", throwable)
    }
}

internal const val NO_RUNTIME_PROFILE_REASON = "no runtime profile configured"

internal fun String.isNoRuntimeProfileReason(): Boolean =
    contains(NO_RUNTIME_PROFILE_REASON, ignoreCase = true)

internal fun <T> DaemonClientResult<T>.asFailure(): DaemonClientResult<Nothing> = when (this) {
    is DaemonClientResult.DaemonError -> this
    is DaemonClientResult.RootDenied -> this
    is DaemonClientResult.Timeout -> this
    is DaemonClientResult.DaemonNotFound -> this
    is DaemonClientResult.DaemonUnavailable -> this
    is DaemonClientResult.ParseError -> this
    is DaemonClientResult.Failure -> this
    is DaemonClientResult.Ok -> error("Success result cannot be converted to failure")
}

internal fun <T> DaemonctlResult.toDaemonClientResult(
    transform: (JsonElement) -> T,
): DaemonClientResult<T> =
    toDaemonClientResultEnvelope { element ->
        DaemonClientResult.Ok(transform(element))
    }

internal fun <T> DaemonctlResult.toDaemonClientResultEnvelope(
    success: (JsonElement) -> DaemonClientResult<T>,
): DaemonClientResult<T> = when (this) {
    is DaemonctlResult.Success -> try {
        success(data)
    } catch (e: Exception) {
        DaemonClientResult.ParseError(data.toString(), e)
    }
    is DaemonctlResult.Error -> DaemonClientResult.DaemonError(code, message, details, envelope)
    is DaemonctlResult.RootDenied -> DaemonClientResult.RootDenied(reason)
    is DaemonctlResult.Timeout -> DaemonClientResult.Timeout(method)
    is DaemonctlResult.DaemonNotFound -> DaemonClientResult.DaemonNotFound(path)
    is DaemonctlResult.DaemonUnavailable -> DaemonClientResult.DaemonUnavailable(reason)
    is DaemonctlResult.UnexpectedError -> DaemonClientResult.Failure(throwable)
}
