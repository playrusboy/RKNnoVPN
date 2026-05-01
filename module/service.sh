#!/system/bin/sh
# RKNnoVPN — service.sh
# Runs at boot after data is decrypted (non-blocking).
# Launches the RKNnoVPN daemon that manages sing-box + iptables.
# POSIX sh compatible (busybox ash).

# ============================================================================
# Constants
# ============================================================================

MODDIR="${0%/*}"
RKNNOVPN_DIR="${RKNNOVPN_DIR:-${MODDIR:-/data/adb/modules/rknnovpn}}"
RKNNOVPN_GID=23333

DAEMON_BIN="${RKNNOVPN_DIR}/bin/daemon"
DAEMON_PID_FILE="${RKNNOVPN_DIR}/run/daemon.pid"
DAEMON_SOCKET="${RKNNOVPN_DIR}/run/daemon.sock"
CONFIG_FILE="${RKNNOVPN_DIR}/config/config.json"
MANUAL_FLAG="${RKNNOVPN_DIR}/config/manual"
LOG_FILE="${RKNNOVPN_DIR}/logs/service.log"
PROFILE_FILE="${RKNNOVPN_DIR}/config/profile.json"
MODULE_PROP="${MODDIR}/module.prop"
LOG_VERSION_FILE="${RKNNOVPN_DIR}/logs/.version"
LOG_ARCHIVE_DIR="${RKNNOVPN_DIR}/logs/archive"

TAG="rknnovpn:service"
BOOT_TIMEOUT=120
SETTLE_DELAY=5
DAEMON_READY_TIMEOUT=15
OOM_SCORE_ADJ="${RKNNOVPN_OOM_SCORE_ADJ:-300}"
APP_REPAIR=0
SERVICE_LOCK_DIR="${RKNNOVPN_DIR}/config/service.lock"
SERVICE_LOCK_WAIT="${RKNNOVPN_SERVICE_LOCK_WAIT:-60}"
SERVICE_LOCK_HELD=0

case "${1:-}" in
    --app-repair)
        APP_REPAIR=1
        BOOT_TIMEOUT=5
        SETTLE_DELAY=0
        DAEMON_READY_TIMEOUT=20
        ;;
esac

if [ -f "${RKNNOVPN_DIR}/scripts/lib/rknnovpn_env.sh" ]; then
    . "${RKNNOVPN_DIR}/scripts/lib/rknnovpn_env.sh"
fi

# ============================================================================
# Logging
# ============================================================================

prepare_runtime_dirs() {
    mkdir -p "${RKNNOVPN_DIR}/logs" "${RKNNOVPN_DIR}/run" "${RKNNOVPN_DIR}/config" 2>/dev/null
    chown 0:0 "${RKNNOVPN_DIR}/logs" "${RKNNOVPN_DIR}/run" "${RKNNOVPN_DIR}/config" 2>/dev/null
    chmod 0700 "${RKNNOVPN_DIR}/logs" "${RKNNOVPN_DIR}/run" "${RKNNOVPN_DIR}/config" 2>/dev/null

    touch "$LOG_FILE" "${RKNNOVPN_DIR}/logs/daemon.log" 2>/dev/null
    chown 0:0 "$LOG_FILE" "${RKNNOVPN_DIR}/logs/daemon.log" 2>/dev/null
    chmod 0600 "$LOG_FILE" "${RKNNOVPN_DIR}/logs/daemon.log" 2>/dev/null
}

ts() {
    date "+%Y-%m-%d %H:%M:%S" 2>/dev/null || echo "----"
}

log_info() {
    msg="$(ts) [INFO] $1"
    echo "$msg" >> "$LOG_FILE" 2>/dev/null
    /system/bin/log -t "$TAG" -p i "$1" 2>/dev/null
}

log_warn() {
    msg="$(ts) [WARN] $1"
    echo "$msg" >> "$LOG_FILE" 2>/dev/null
    /system/bin/log -t "$TAG" -p w "$1" 2>/dev/null
}

log_error() {
    msg="$(ts) [ERROR] $1"
    echo "$msg" >> "$LOG_FILE" 2>/dev/null
    /system/bin/log -t "$TAG" -p e "$1" 2>/dev/null
}

module_version() {
    if [ -f "$MODULE_PROP" ]; then
        sed -n 's/^version=//p' "$MODULE_PROP" 2>/dev/null | head -n 1
    fi
}

rotate_logs_if_version_changed() {
    mkdir -p "${RKNNOVPN_DIR}/logs" 2>/dev/null || return 0
    chown 0:0 "${RKNNOVPN_DIR}/logs" 2>/dev/null
    chmod 0700 "${RKNNOVPN_DIR}/logs" 2>/dev/null

    CURRENT_VERSION="$(module_version)"
    [ -n "$CURRENT_VERSION" ] || return 0

    PREVIOUS_VERSION=""
    if [ -f "$LOG_VERSION_FILE" ]; then
        PREVIOUS_VERSION="$(sed -n '1p' "$LOG_VERSION_FILE" 2>/dev/null)"
    fi
    if [ "$PREVIOUS_VERSION" = "$CURRENT_VERSION" ]; then
        return 0
    fi

    STAMP="$(date "+%Y%m%d-%H%M%S" 2>/dev/null || echo "now")"
    FROM_VERSION="${PREVIOUS_VERSION:-unknown}"
    ARCHIVE_DIR="${LOG_ARCHIVE_DIR}/${FROM_VERSION}_to_${CURRENT_VERSION}_${STAMP}"
    MOVED=0

    for name in daemon.log sing-box.log xray.log service.log rescue_reset.log net_change.log; do
        src="${RKNNOVPN_DIR}/logs/${name}"
        if [ -s "$src" ]; then
            mkdir -p "$ARCHIVE_DIR" 2>/dev/null || continue
            if mv "$src" "${ARCHIVE_DIR}/${name}" 2>/dev/null; then
                MOVED=1
            fi
        fi
    done

    if [ "$MOVED" = "1" ]; then
        chown -R 0:0 "$ARCHIVE_DIR" 2>/dev/null
        chmod 0700 "$ARCHIVE_DIR" 2>/dev/null
        for f in "$ARCHIVE_DIR"/*; do
            [ -f "$f" ] && chmod 0600 "$f" 2>/dev/null
        done
        echo "$(ts) [INFO] Rotated logs for module ${FROM_VERSION} -> ${CURRENT_VERSION}: ${ARCHIVE_DIR}" >> "$LOG_FILE" 2>/dev/null
    fi

    echo "$CURRENT_VERSION" > "$LOG_VERSION_FILE" 2>/dev/null
    chown 0:0 "$LOG_VERSION_FILE" 2>/dev/null
    chmod 0600 "$LOG_VERSION_FILE" 2>/dev/null
}

# ============================================================================
# 1. Detect root manager and set busybox path
# ============================================================================

detect_busybox() {
    # KernelSU
    if [ -n "$KSU" ] && [ "$KSU" = "true" ]; then
        if [ -x "/data/adb/ksu/bin/busybox" ]; then
            BUSYBOX="/data/adb/ksu/bin/busybox"
            return
        fi
    fi

    # APatch
    if [ -n "$APATCH" ] && [ "$APATCH" = "true" ]; then
        if [ -x "/data/adb/ap/bin/busybox" ]; then
            BUSYBOX="/data/adb/ap/bin/busybox"
            return
        fi
    fi

    # Magisk
    if [ -x "/data/adb/magisk/busybox" ]; then
        BUSYBOX="/data/adb/magisk/busybox"
        return
    fi

    # System busybox
    if command -v busybox >/dev/null 2>&1; then
        BUSYBOX="busybox"
        return
    fi

    # Fallback — no busybox, use shell builtins only
    BUSYBOX=""
}

prepare_runtime_dirs

service_lock_owner_alive() {
    _pid="$(cat "${SERVICE_LOCK_DIR}/pid" 2>/dev/null)"
    [ -n "$_pid" ] || return 1
    kill -0 "$_pid" 2>/dev/null || return 1
    [ -r "/proc/${_pid}/cmdline" ] || return 1
    _cmd="$(cat "/proc/${_pid}/cmdline" 2>/dev/null | tr '\000' ' ')"
    case "$_cmd" in
        *"${RKNNOVPN_DIR}/service.sh"*)
            return 0
            ;;
    esac
    return 1
}

release_service_lock() {
    if [ "$SERVICE_LOCK_HELD" = "1" ]; then
        rm -rf "$SERVICE_LOCK_DIR" 2>/dev/null
        SERVICE_LOCK_HELD=0
    fi
}

acquire_service_lock() {
    _waited=0
    while ! mkdir "$SERVICE_LOCK_DIR" 2>/dev/null; do
        if ! service_lock_owner_alive; then
            rm -rf "$SERVICE_LOCK_DIR" 2>/dev/null
            continue
        fi
        if [ "$_waited" -ge "$SERVICE_LOCK_WAIT" ]; then
            log_warn "Another service launch is still running; skipping this invocation"
            return 1
        fi
        if [ "$_waited" = "0" ]; then
            log_info "Another service launch is running; waiting for lock"
        fi
        sleep 1
        _waited=$((_waited + 1))
    done

    SERVICE_LOCK_HELD=1
    echo "$$" > "${SERVICE_LOCK_DIR}/pid" 2>/dev/null
    trap release_service_lock EXIT HUP INT TERM
    return 0
}

if ! acquire_service_lock; then
    exit 0
fi

rotate_logs_if_version_changed
prepare_runtime_dirs
detect_busybox
log_info "Busybox: ${BUSYBOX:-not found}"
if [ "$APP_REPAIR" = "1" ]; then
    log_info "App repair launch requested"
fi

# ============================================================================
# 2. Wait for boot completion
# ============================================================================

wait_boot_completed() {
    log_info "Waiting for sys.boot_completed (timeout: ${BOOT_TIMEOUT}s)..."

    ELAPSED=0
    while [ "$ELAPSED" -lt "$BOOT_TIMEOUT" ]; do
        BOOT_DONE="$(getprop sys.boot_completed 2>/dev/null)"
        if [ "$BOOT_DONE" = "1" ]; then
            log_info "Boot completed after ${ELAPSED}s"
            return 0
        fi
        sleep 1
        ELAPSED=$((ELAPSED + 1))
    done

    log_warn "Boot completion timeout after ${BOOT_TIMEOUT}s — proceeding anyway"
    return 1
}

wait_boot_completed

# Post-boot settle delay — let other services finish starting
log_info "Settle delay: ${SETTLE_DELAY}s"
sleep "$SETTLE_DELAY"

first_pid_by_cmd_path() {
    wanted="$1"
    for p in /proc/[0-9]*; do
        pid="${p##*/}"
        [ "$pid" = "$$" ] && continue
        [ -r "$p/cmdline" ] || continue
        cmd="$(cat "$p/cmdline" 2>/dev/null | tr '\000' ' ')"
        case "$cmd" in
            *"$wanted"*)
                echo "$pid"
                return 0
                ;;
        esac
    done
    return 1
}

# ============================================================================
# 3. Boot rescue cleanup
# ============================================================================

has_boot_cleanup_markers() {
    if command -v rknnovpn_has_boot_cleanup_markers >/dev/null 2>&1; then
        rknnovpn_has_boot_cleanup_markers
        return $?
    fi
    log_warn "rknnovpn_env.sh marker helper unavailable; boot cleanup will run as probe"
    return 1
}

is_reset_active() {
    if command -v rknnovpn_is_reset_active >/dev/null 2>&1; then
        rknnovpn_is_reset_active
        return $?
    fi
    [ -f "${RKNNOVPN_DIR}/run/reset.lock" ]
}

daemon_socket_exists() {
    [ -S "$DAEMON_SOCKET" ] 2>/dev/null || [ -e "$DAEMON_SOCKET" ]
}

has_orphan_runtime_processes() {
    first_pid_by_cmd_path "$DAEMON_BIN" >/dev/null 2>&1 && return 0
    first_pid_by_cmd_path "${RKNNOVPN_DIR}/bin/sing-box" >/dev/null 2>&1 && return 0
    first_pid_by_cmd_path "${RKNNOVPN_DIR}/bin/xray" >/dev/null 2>&1 && return 0
    first_pid_by_cmd_path "${RKNNOVPN_DIR}/scripts/net_handler.sh" >/dev/null 2>&1 && return 0
    return 1
}

if [ "$APP_REPAIR" = "1" ]; then
    if is_reset_active; then
        log_info "Reset lock is active; app repair will not interrupt cleanup"
        exit 0
    fi
    log_info "App repair uses daemon-only ensure; boot cleanup is skipped"
elif [ -x "${RKNNOVPN_DIR}/scripts/rescue_reset.sh" ]; then
    EXISTING_DAEMON_PID="$(first_pid_by_cmd_path "$DAEMON_BIN" 2>/dev/null)"
    if [ -n "$EXISTING_DAEMON_PID" ] && daemon_socket_exists; then
        log_info "Daemon already has an IPC socket; boot cleanup is skipped"
    elif has_boot_cleanup_markers || has_orphan_runtime_processes; then
        log_info "Running boot rescue cleanup"
        if "${RKNNOVPN_DIR}/scripts/rescue_reset.sh" --boot-clean >> "$LOG_FILE" 2>&1; then
            log_info "Boot rescue cleanup completed"
        else
            log_warn "Boot rescue cleanup reported leftovers; daemon will still start for diagnostics"
        fi
    else
        log_info "No stale runtime markers or orphan processes; boot cleanup skipped"
    fi
else
    log_warn "Boot rescue cleanup script not found"
fi

stop_daemon_process_only() {
    pid="$1"
    [ -n "$pid" ] || return 0
    if kill -0 "$pid" 2>/dev/null; then
        log_warn "Stopping daemon-only process ${pid}; IPC socket did not become ready"
        kill -TERM "$pid" 2>/dev/null || true
        sleep 1
    fi
    if kill -0 "$pid" 2>/dev/null; then
        kill -KILL "$pid" 2>/dev/null || true
    fi
    rm -f "$DAEMON_PID_FILE" "$DAEMON_SOCKET" 2>/dev/null
}

if [ -f "$MANUAL_FLAG" ]; then
    log_info "Manual flag detected at ${MANUAL_FLAG} — proxy autostart stays disabled"
fi

# ============================================================================
# 4. Pre-launch validation
# ============================================================================

has_runtime_profile() {
    if command -v rknnovpn_has_runtime_profile >/dev/null 2>&1; then
        rknnovpn_has_runtime_profile "$PROFILE_FILE"
        return $?
    fi

    if [ -f "$PROFILE_FILE" ]; then
        compact="$(tr -d '\n\r\t ' < "$PROFILE_FILE" 2>/dev/null)"
        case "$compact" in
            *'"nodes":[{'*) return 0 ;;
        esac
    fi
    return 1
}

if [ "$APP_REPAIR" != "1" ] && ! has_runtime_profile; then
    log_info "No configured proxy nodes/keys; daemon launch skipped until the app imports a server"
    exit 0
fi

detect_arch_dir() {
    ABI="$(getprop ro.product.cpu.abi 2>/dev/null)"
    case "$ABI" in
        arm64-v8a|arm64*)
            echo "arm64"
            ;;
        armeabi-v7a|armeabi|armv7*|arm*)
            echo "armv7"
            ;;
        *)
            echo ""
            ;;
    esac
}

restore_missing_binaries() {
    ARCH_DIR="$(detect_arch_dir)"
    if [ -z "$ARCH_DIR" ]; then
        log_warn "Cannot determine CPU ABI for binary restore"
        return 0
    fi
    SRC_BIN="${RKNNOVPN_DIR}/binaries/${ARCH_DIR}"
    if [ ! -d "$SRC_BIN" ]; then
        return 0
    fi

    for bin_name in daemon daemonctl sing-box xray; do
        target="${RKNNOVPN_DIR}/bin/${bin_name}"
        source="${SRC_BIN}/${bin_name}"
        if [ ! -x "$target" ] && [ -f "$source" ]; then
            log_warn "Restoring missing or non-executable binary: ${target}"
            cp -f "$source" "$target" 2>/dev/null
            chown 0:0 "$target" 2>/dev/null
            chmod 0755 "$target" 2>/dev/null
        fi
    done
}

restore_missing_binaries

# Check that the daemon binary exists
if [ ! -x "$DAEMON_BIN" ]; then
    log_error "Daemon binary not found or not executable: ${DAEMON_BIN}"
    log_error "Install sing-box/daemon to ${RKNNOVPN_DIR}/bin/ and reboot"
    exit 1
fi

# Check that config exists
if [ ! -f "$CONFIG_FILE" ]; then
    log_error "Config file not found: ${CONFIG_FILE}"
    log_error "Copy config.json to ${RKNNOVPN_DIR}/config/ and reboot"
    exit 1
fi

wait_daemon_socket() {
    WAITED=0
    while [ "$WAITED" -lt "$DAEMON_READY_TIMEOUT" ]; do
        if [ -z "$DAEMON_PID" ] || ! kill -0 "$DAEMON_PID" 2>/dev/null; then
            DAEMON_PID="$(first_pid_by_cmd_path "$DAEMON_BIN" 2>/dev/null)"
            if [ -z "$DAEMON_PID" ]; then
                log_error "Daemon process exited before IPC socket became ready"
                return 1
            fi
        fi

        if [ -S "$DAEMON_SOCKET" ] 2>/dev/null || [ -e "$DAEMON_SOCKET" ]; then
            log_info "Daemon IPC socket is ready: ${DAEMON_SOCKET}"
            return 0
        fi

        sleep 1
        WAITED=$((WAITED + 1))
    done

    log_error "Daemon IPC socket did not appear within ${DAEMON_READY_TIMEOUT}s: ${DAEMON_SOCKET}"
    return 1
}

append_daemon_log_tail() {
    if [ -s "${RKNNOVPN_DIR}/logs/daemon.log" ]; then
        echo "$(ts) [ERROR] Last daemon.log lines:" >> "$LOG_FILE" 2>/dev/null
        tail -n 40 "${RKNNOVPN_DIR}/logs/daemon.log" >> "$LOG_FILE" 2>/dev/null
    fi
}

# ============================================================================
# 5. Set resource limits
# ============================================================================

# Raise file descriptor limit — sing-box may handle thousands of connections
ulimit -SHn 1000000 2>/dev/null
ACTUAL_ULIMIT="$(ulimit -n 2>/dev/null)"
log_info "File descriptor limit: ${ACTUAL_ULIMIT}"

# ============================================================================
# 6. Launch daemon
# ============================================================================

launch_daemon() {
    log_info "Launching RKNnoVPN daemon..."
    log_info "  Binary:  ${DAEMON_BIN}"
    log_info "  Config:  ${CONFIG_FILE}"
    log_info "  PID file: ${DAEMON_PID_FILE}"

    prepare_runtime_dirs

    EXISTING_PID="$(cat "$DAEMON_PID_FILE" 2>/dev/null)"
    if [ -z "$EXISTING_PID" ]; then
        EXISTING_PID="$(first_pid_by_cmd_path "$DAEMON_BIN" 2>/dev/null)"
    fi
    if [ -n "$EXISTING_PID" ] && kill -0 "$EXISTING_PID" 2>/dev/null; then
        DAEMON_PID="$EXISTING_PID"
        if daemon_socket_exists; then
            echo "$DAEMON_PID" > "$DAEMON_PID_FILE" 2>/dev/null
            chown 0:0 "$DAEMON_PID_FILE" 2>/dev/null
            chmod 0600 "$DAEMON_PID_FILE" 2>/dev/null
            log_info "Daemon is already running with PID ${DAEMON_PID}"
            return 0
        fi

        if [ "$APP_REPAIR" = "1" ]; then
            log_warn "Daemon process ${DAEMON_PID} is running without IPC socket; waiting before daemon-only repair"
            if wait_daemon_socket; then
                echo "$DAEMON_PID" > "$DAEMON_PID_FILE" 2>/dev/null
                chown 0:0 "$DAEMON_PID_FILE" 2>/dev/null
                chmod 0600 "$DAEMON_PID_FILE" 2>/dev/null
                log_info "Daemon IPC became ready for existing PID ${DAEMON_PID}"
                return 0
            fi
            append_daemon_log_tail
            stop_daemon_process_only "$DAEMON_PID"
        else
            log_error "Daemon process ${DAEMON_PID} is running but IPC socket is missing: ${DAEMON_SOCKET}"
            append_daemon_log_tail
            return 1
        fi
    fi

    rm -f "$DAEMON_SOCKET" "$DAEMON_PID_FILE" 2>/dev/null

    # Launch daemon with nohup + setsid to fully detach from init
    # - nohup: ignore SIGHUP when terminal closes
    # - setsid: create new session (no controlling terminal)
    # stdout/stderr go to daemon log file
    nohup setsid "${DAEMON_BIN}" \
        --config "${CONFIG_FILE}" \
        --data-dir "${RKNNOVPN_DIR}" \
        >> "${RKNNOVPN_DIR}/logs/daemon.log" 2>&1 &

    DAEMON_PID=$!

    # Brief wait to check if it crashed immediately
    sleep 2

    # Check if process is still alive
    if ! kill -0 "$DAEMON_PID" 2>/dev/null; then
        # Process died — try to find the actual PID (setsid may have forked)
        DAEMON_PID="$(first_pid_by_cmd_path "$DAEMON_BIN" 2>/dev/null)"
        if [ -z "$DAEMON_PID" ]; then
            log_error "Daemon failed to start — check ${RKNNOVPN_DIR}/logs/daemon.log"
            return 1
        fi
    fi

    if ! wait_daemon_socket; then
        append_daemon_log_tail
        return 1
    fi

    echo "$DAEMON_PID" > "$DAEMON_PID_FILE" 2>/dev/null
    chown 0:0 "$DAEMON_PID_FILE" 2>/dev/null
    chmod 0600 "$DAEMON_PID_FILE" 2>/dev/null
    log_info "Daemon started with PID ${DAEMON_PID}"

    return 0
}

launch_daemon
LAUNCH_RESULT=$?

# ============================================================================
# 7. Set OOM score — keep Android UI preferred over RKNnoVPN workers
# ============================================================================

if [ "$LAUNCH_RESULT" -eq 0 ]; then
    DAEMON_PID="$(cat "$DAEMON_PID_FILE" 2>/dev/null)"
    if [ -n "$DAEMON_PID" ] && [ -d "/proc/${DAEMON_PID}" ]; then
        # oom_score_adj range: -1000 to 1000
        # Positive values make RKNnoVPN easier to reclaim than SystemUI.
        echo "$OOM_SCORE_ADJ" > "/proc/${DAEMON_PID}/oom_score_adj" 2>/dev/null
        if [ $? -eq 0 ]; then
            log_info "Set oom_score_adj=${OOM_SCORE_ADJ} for PID ${DAEMON_PID}"
        else
            log_warn "Failed to set oom_score_adj for PID ${DAEMON_PID}"
        fi

        # Apply the same non-critical priority to sing-box if it exists.
        sleep 3
        SINGBOX_PID="$(first_pid_by_cmd_path "${RKNNOVPN_DIR}/bin/sing-box" 2>/dev/null)"
        if [ -n "$SINGBOX_PID" ] && [ -d "/proc/${SINGBOX_PID}" ]; then
            echo "$OOM_SCORE_ADJ" > "/proc/${SINGBOX_PID}/oom_score_adj" 2>/dev/null
            log_info "Set oom_score_adj=${OOM_SCORE_ADJ} for sing-box PID ${SINGBOX_PID}"
        fi
        XRAY_PID="$(first_pid_by_cmd_path "${RKNNOVPN_DIR}/bin/xray" 2>/dev/null)"
        if [ -n "$XRAY_PID" ] && [ -d "/proc/${XRAY_PID}" ]; then
            echo "$OOM_SCORE_ADJ" > "/proc/${XRAY_PID}/oom_score_adj" 2>/dev/null
            log_info "Set oom_score_adj=${OOM_SCORE_ADJ} for xray PID ${XRAY_PID}"
        fi
    fi
fi

# ============================================================================
# 8. Final verification
# ============================================================================

if [ "$LAUNCH_RESULT" -eq 0 ]; then
    # Final check after OOM adjustment
    DAEMON_PID="$(cat "$DAEMON_PID_FILE" 2>/dev/null)"
    if [ -n "$DAEMON_PID" ] && kill -0 "$DAEMON_PID" 2>/dev/null; then
        log_info "RKNnoVPN daemon is running (PID ${DAEMON_PID})"
        log_info "service.sh completed successfully"
    else
        log_error "Daemon PID ${DAEMON_PID} is no longer running"
        log_error "Check logs at ${RKNNOVPN_DIR}/logs/daemon.log"
        exit 1
    fi
else
    log_error "Failed to launch daemon"
    log_error "Check logs at ${RKNNOVPN_DIR}/logs/daemon.log"
    exit 1
fi
