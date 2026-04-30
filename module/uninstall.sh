#!/system/bin/sh
# RKNnoVPN — module removal entrypoint.
# Runtime cleanup is owned by scripts/rescue_reset.sh; this file only
# orchestrates uninstall-specific cleanup.

set +e

RKNNOVPN_DIR="${RKNNOVPN_DIR:-/data/adb/modules/rknnovpn}"
TAG="rknnovpn:uninstall"

if [ -f "${RKNNOVPN_DIR}/scripts/lib/rknnovpn_env.sh" ]; then
    . "${RKNNOVPN_DIR}/scripts/lib/rknnovpn_env.sh"
fi
SCRIPTS_DIR="${SCRIPTS_DIR:-${RKNNOVPN_DIR}/scripts}"
RENDERED_CONFIG_DIR="${RENDERED_CONFIG_DIR:-${RKNNOVPN_DIR}/config/rendered}"
DEFERRED_DIR="${DEFERRED_DIR:-/data/adb/rknnovpn-uninstall-cleanup}"
DEFERRED_SERVICE_SCRIPT="${DEFERRED_SERVICE_SCRIPT:-/data/adb/service.d/99-rknnovpn-uninstall-cleanup.sh}"

log_msg() {
    /system/bin/log -t "$TAG" -p i "$1" 2>/dev/null
    echo "[rknnovpn] $1"
}

log_err() {
    /system/bin/log -t "$TAG" -p e "$1" 2>/dev/null
    echo "[rknnovpn] ERROR: $1"
}

run_runtime_cleanup() {
    if [ -x "${SCRIPTS_DIR}/rescue_reset.sh" ]; then
        log_msg "Running canonical runtime cleanup"
        RKNNOVPN_DIR="$RKNNOVPN_DIR" "${SCRIPTS_DIR}/rescue_reset.sh" uninstall-clean
        return $?
    fi

    log_err "rescue_reset.sh missing; runtime cleanup is unavailable"
    return 1
}

stage_deferred_cleanup() {
    log_msg "Staging one-shot post-uninstall cleanup"
    mkdir -p "${DEFERRED_DIR}/scripts/lib" "/data/adb/service.d" 2>/dev/null || {
        log_err "Failed to create deferred cleanup directories"
        return 1
    }

    for file in rescue_reset.sh; do
        if [ -f "${SCRIPTS_DIR}/${file}" ]; then
            cp -f "${SCRIPTS_DIR}/${file}" "${DEFERRED_DIR}/scripts/${file}" 2>/dev/null || return 1
            chmod 0700 "${DEFERRED_DIR}/scripts/${file}" 2>/dev/null || true
        fi
    done
    for file in rknnovpn_env.sh rknnovpn_netstack.sh; do
        if [ -f "${SCRIPTS_DIR}/lib/${file}" ]; then
            cp -f "${SCRIPTS_DIR}/lib/${file}" "${DEFERRED_DIR}/scripts/lib/${file}" 2>/dev/null || return 1
            chmod 0600 "${DEFERRED_DIR}/scripts/lib/${file}" 2>/dev/null || true
        fi
    done

    cat > "$DEFERRED_SERVICE_SCRIPT" <<'EOF'
#!/system/bin/sh

MODULE_DIR="/data/adb/modules/rknnovpn"
BUNDLE_DIR="/data/adb/rknnovpn-uninstall-cleanup"
SERVICE_SCRIPT="/data/adb/service.d/99-rknnovpn-uninstall-cleanup.sh"
LOG_FILE="${BUNDLE_DIR}/cleanup.log"

log_msg() {
    mkdir -p "$BUNDLE_DIR" 2>/dev/null || true
    echo "$(date '+%Y-%m-%d %H:%M:%S' 2>/dev/null || echo '----') $*" >> "$LOG_FILE" 2>/dev/null || true
    /system/bin/log -t rknnovpn:uninstall-cleanup -p i "$*" 2>/dev/null || true
}

trigger_rollback_guard() {
    guard="/data/adb/service.d/99-rknnovpn-rollback-guard.sh"
    if [ -x "$guard" ]; then
        log_msg "Running installed rollback guard once"
        "$guard" >/dev/null 2>&1 || true
    fi
}

run_cleanup() {
    if [ -x "${MODULE_DIR}/scripts/rescue_reset.sh" ]; then
        log_msg "running module rescue cleanup"
        RKNNOVPN_DIR="$MODULE_DIR" "${MODULE_DIR}/scripts/rescue_reset.sh" uninstall-clean >> "$LOG_FILE" 2>&1
        return $?
    fi

    if [ -x "${BUNDLE_DIR}/scripts/rescue_reset.sh" ]; then
        log_msg "running bundled rescue cleanup"
        RKNNOVPN_DIR="$MODULE_DIR" \
        RUN_DIR="${BUNDLE_DIR}/run" \
        CONFIG_DIR="${BUNDLE_DIR}/config" \
        LOG_DIR="${BUNDLE_DIR}/logs" \
        SCRIPTS_DIR="${BUNDLE_DIR}/scripts" \
        "${BUNDLE_DIR}/scripts/rescue_reset.sh" uninstall-clean >> "$LOG_FILE" 2>&1
        return $?
    fi

    log_msg "no rescue cleanup script is available"
    return 1
}

collect_leftovers() {
    if [ -f "${BUNDLE_DIR}/scripts/lib/rknnovpn_netstack.sh" ]; then
        . "${BUNDLE_DIR}/scripts/lib/rknnovpn_netstack.sh"
        rknnovpn_collect_netstack_leftovers 2>/dev/null
    fi
}

sleep 10
run_cleanup || true
leftovers="$(collect_leftovers)"
if [ -z "$leftovers" ]; then
    log_msg "post-uninstall cleanup complete"
    rm -f "$SERVICE_SCRIPT" 2>/dev/null || true
    rm -rf "$BUNDLE_DIR" 2>/dev/null || true
else
    log_msg "leftovers remain: $leftovers"
fi

exit 0
EOF
    chmod 0700 "$DEFERRED_SERVICE_SCRIPT" 2>/dev/null || true
}

restore_kernel_params() {
    log_msg "Restoring kernel parameters"
    if [ -n "${SYSCTL_SNAPSHOT_DIR:-}" ] && [ -d "$SYSCTL_SNAPSHOT_DIR" ] && command -v rknnovpn_restore_sysctl_snapshots >/dev/null 2>&1; then
        rknnovpn_restore_sysctl_snapshots
        return
    fi

    for rp_path in /proc/sys/net/ipv4/conf/all/rp_filter \
                   /proc/sys/net/ipv4/conf/default/rp_filter; do
        if [ -f "$rp_path" ]; then
            echo 1 > "$rp_path" 2>/dev/null
        fi
    done
}

clean_runtime_files() {
    log_msg "Cleaning generated rendered configs"
    rm -f "${RENDERED_CONFIG_DIR}/"* 2>/dev/null
}

log_msg "========================================="
log_msg "RKNnoVPN module removal starting"
log_msg "========================================="

stage_deferred_cleanup || log_err "Deferred cleanup was not staged"
run_runtime_cleanup || log_err "Runtime cleanup reported leftovers"
restore_kernel_params
trigger_rollback_guard
clean_runtime_files

log_msg "========================================="
log_msg "RKNnoVPN module removal complete"
log_msg "Runtime data lives under the module directory: ${RKNNOVPN_DIR}/"
log_msg "Root manager removal may delete this directory with the module."
log_msg "========================================="
