#!/system/bin/sh
# privacy_guard.sh — package visibility guard for sensitive direct apps.
#
# This intentionally does not promise procfs hiding. /proc/net/tcp is a kernel
# procfs read surface; hiding it per-app needs a process namespace hook,
# Zygisk/LSPosed, or kernel support. Here we apply the safe root-side guard
# Android exposes: deny broad package inventory via QUERY_ALL_PACKAGES app-op.

set -u

SCRIPT_DIR="${0%/*}"
if [ -f "${SCRIPT_DIR}/lib/rknnovpn_env.sh" ]; then
    . "${SCRIPT_DIR}/lib/rknnovpn_env.sh"
fi

TAG="rknnovpn:privacy"
PACKAGES="${PRIVACY_GUARD_PACKAGES:-}"
MODE="${PRIVACY_GUARD_MODE:-appops}"

log_info() {
    if command -v rknnovpn_log_info >/dev/null 2>&1; then
        rknnovpn_log_info "$@"
    else
        echo "[$TAG] INFO: $*"
    fi
}

log_warn() {
    if command -v rknnovpn_log_warn >/dev/null 2>&1; then
        rknnovpn_log_warn "$@"
    else
        echo "[$TAG] WARN: $*" >&2
    fi
}

package_installed() {
    _pkg="$1"
    cmd package path "$_pkg" >/dev/null 2>&1 && return 0
    pm path "$_pkg" >/dev/null 2>&1 && return 0
    return 1
}

set_query_all_packages_ignore() {
    _pkg="$1"
    cmd appops set "$_pkg" QUERY_ALL_PACKAGES ignore >/dev/null 2>&1 && return 0
    cmd appops set "$_pkg" android:query_all_packages ignore >/dev/null 2>&1 && return 0
    appops set "$_pkg" QUERY_ALL_PACKAGES ignore >/dev/null 2>&1 && return 0
    appops set "$_pkg" android:query_all_packages ignore >/dev/null 2>&1 && return 0
    return 1
}

apply_guard() {
    if [ "$MODE" = "off" ]; then
        log_info "privacy guard disabled"
        return 0
    fi
    if [ -z "$PACKAGES" ]; then
        log_info "no packages for privacy guard"
        return 0
    fi

    _applied=0
    _skipped=0
    _unsupported=0
    for _pkg in $PACKAGES; do
        case "$_pkg" in
            *[!A-Za-z0-9._-]*|"") continue ;;
        esac
        if ! package_installed "$_pkg"; then
            _skipped=$((_skipped + 1))
            continue
        fi
        if set_query_all_packages_ignore "$_pkg"; then
            _applied=$((_applied + 1))
        else
            _unsupported=$((_unsupported + 1))
            log_warn "QUERY_ALL_PACKAGES app-op not applied for $_pkg"
        fi
    done
    log_info "package visibility guard applied=${_applied} skipped=${_skipped} unsupported=${_unsupported}"
    log_warn "/proc/net/tcp per-app hiding is unsupported without a process namespace hook or kernel support"
    return 0
}

case "${1:-start}" in
    start|apply|status)
        apply_guard
        ;;
    stop)
        # AppOps are a deliberate privacy hardening setting, not netfilter
        # runtime state. Do not undo them on ordinary RKNnoVPN stop/reset.
        log_info "privacy guard stop is a no-op"
        ;;
    *)
        log_warn "unknown command: ${1:-}"
        ;;
esac

exit 0
