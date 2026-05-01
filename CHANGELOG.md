# Changelog

## v2.2.1

- Added collapsible server sections grouped by subscription and manual configs.
- Fixed manual active-server display so the main screen uses the selected profile node.
- Made app process routing explicit in Rule-sets mode: selected packages are forced through proxy before geo/domain/IP rules.
- Improved the app routing UI with selected apps at the top, clearer always-direct override handling, and a direct Settings shortcut.
- Preserved profile configuration during module updates and made first-install no-node status non-alarming.
- Fixed runtime log ZIP sharing to use lightweight runtime logs.
- Synchronized app, daemon, daemonctl, module, and update feed metadata to `v2.2.1`.

## v2.2.0

- Fixed single-node group selector rendering so sing-box no longer references a missing `node-*` outbound from `group-default`.
- Fixed subscription fetching on Android by loading system and user-added Android CA certificates for daemon-side TLS verification.
- Added user-editable exclusions from the built-in always-direct app policy so Russian/sensitive apps can be explicitly routed through RKNnoVPN when needed.
- Improved the app routing screen with an anchored Apply button and per-app controls to proxy or restore built-in always-direct apps.
- Added an in-app Back button to the Audit screen.
- Synchronized app, daemon, daemonctl, module, update feed, and bundled script version metadata to `v2.2.0`.

## v2.1.7

- Fixed Reality over gRPC rendering so sing-box gets `transport.type=grpc` together with Reality TLS instead of losing the gRPC transport.
- Fixed QUIC transport rendering by omitting Xray-only QUIC fields that sing-box does not support.
- Reject unsupported V2Ray transports such as mKCP explicitly instead of silently rendering them as plain TCP.
- Synchronized app, daemon, daemonctl, module, and update feed metadata to `v2.1.7`.

## v2.1.6

- Hardened no-reboot in-app module updates so the old daemon cannot remove the new daemon PID file during live handoff.
- Added regression coverage for daemon PID ownership during live daemon replacement.
- Synchronized app, daemon, daemonctl, module, and update feed metadata to `v2.1.6`.

## v2.1.5

- Fixed sing-box config rendering for imported VLESS/Trojan/VMess gRPC nodes by omitting Xray-only gRPC fields (`mode` and `authority`) that sing-box rejects.
- Added regression coverage for malformed imported gRPC `mode` values so one bad field cannot break sing-box config check.
- Synchronized app, daemon, daemonctl, module, and update feed metadata to `v2.1.5`.

## v2.1.4

- Added the standard Magisk installer envelope (`META-INF/com/google/android/update-binary` and `updater-script`) to the module ZIP so Magisk can launch module updates reliably from the Modules tab.
- Added release manifest checks that require the Magisk installer envelope in future release ZIPs.
- Synchronized app, daemon, daemonctl, module, and update feed metadata to `v2.1.4`.

## v2.1.3

- Show a reboot-required state when KernelSU/APatch has staged the module install or update but has not applied it yet.
- Keep daemon auto-repair for repairable IPC outages, but skip it for staged updates, disabled modules, removal markers, and incomplete installs.
- Create install, service, daemon, and sing-box log files during module installation so the logs directory is not empty before first boot service launch.
- Synchronized app, daemon, daemonctl, module, and update feed metadata to `v2.1.3`.

## v2.1.2

- Fixed daemon-side update checks and update downloads so GitHub requests use bootstrap DNS instead of a stale system resolver such as `[::1]:53`.
- Fixed subscription preview/refresh fetches to use the same bootstrap DNS path while keeping private, local, and reserved subscription endpoints blocked.
- Added regression coverage for updater and subscription resolver bootstrap behavior.
- Synchronized app, daemon, daemonctl, module, and update feed metadata to `v2.1.2`.

## v1.8.0

- Added a runtime actor for lifecycle operations so start, stop, restart, reset, reload, network-change, rescue, and update restore no longer wait on each other through hidden long-running locks.
- Added fail-fast `RUNTIME_BUSY` / `RESET_IN_PROGRESS` JSON-RPC errors with active operation metadata, plus APK-side Russian messaging for busy operations.
- Kept `reset.lock` owned by reset/rescue cleanup paths instead of normal start or hot-swap, preserving the root-level reset guard.
- Added regression coverage for lifecycle conflicts, readable status during active operations, reset lock ownership, and generation handling.
- Extended rooted device-lab helpers with Lineage/QEMU emulator install and smoke targets.
- Synchronized release metadata for app, daemon, module, update feed, workflow stamping, and bundled scripts to `v1.8.0`.

## v1.7.3

- Fixed APK compatibility gating so a damaged `current` release catalog is shown as a repair warning instead of blocking start/restart when APK, daemon, and module versions match.
- Fixed Settings log sharing and diagnostic copy actions by using explicit one-shot UI events, Android clipboard APIs, and visible feedback.
- Rejected localhost TPROXY destinations inside the rendered sing-box route to avoid self-looping listener probes.
- Made module updates boot-safe by default: fresh configs no longer autostart, upgrades set the manual start guard until the app starts the proxy, boot cleanup only runs when stale runtime markers exist, and RKNnoVPN workers no longer get stronger OOM priority than SystemUI.
- Hardened release catalog updates so a stale non-symlink `current` directory is moved aside and replaced with a valid release symlink in both module install and in-app update flows.

## v1.7.1

- Reworked root TPROXY runtime recovery with structured reset reports, netstack cleanup verification, and idempotent rescue behavior.
- Added `daemonctl diagnostics.report` diagnostics with redacted configs/logs, compatibility metadata, release integrity, netstack leftovers, node-test summary, and runtime stage reports.
- Hardened APK/module/daemon compatibility checks before mutating actions, including repair-safe reset handling.
- Split readiness, DNS, routing, egress, and node-test verdicts so TCP-only servers are not treated as usable routes.
- Kept production privacy defaults off for localhost SOCKS/HTTP/API helpers and tightened APK privacy guardrails.
- Unified runtime listener readiness for start/hot-swap across TPROXY, DNS, and optional API ports, and fixed successful start reports to finish cleanly in both status and diagnostics report payloads.
- Extended netstack/status privacy verification to check local listener DROP rules and tightened diagnostic redaction for WireGuard/Amnezia pre-shared keys.
- Expanded self-check/diagnostics report summary with compact compatibility, current-release, sing-box check, and runtime stage metadata for quick support triage.
- Added typed APK IPC access to `self-check` with `self.check` fallback for quick repair summaries.
- Exposed self-check through the APK status repository so Settings/Audit can use concise repair summaries without depending on raw IPC.
- Treated legacy `unknown command` audit responses like method-not-found so old daemons still fall back to local audit checks.

## v1.7.0

- Initial v1.7 runtime stabilization baseline.

## v1.6.4

- Split hard runtime readiness from soft DNS and egress diagnostics so cold-start DNS timeouts leave the runtime connected but degraded.
- Added deterministic readiness/operational diagnostics and clearer node-test reasons for runtime, proxy DNS, and HTTP helper failures.
- Added in-app display and sharing for `/data/adb/modules/rknnovpn/logs/daemon.log` and `/data/adb/modules/rknnovpn/logs/sing-box.log`.

## v1.6.3

- Split hard readiness from soft DNS and egress diagnostics so restart no longer tears the runtime down on a single cold-start DNS timeout.
- Made health error reporting deterministic instead of depending on random Go map iteration order.
- Kept DNS and egress signals visible as operational diagnostics without using them as restart blockers.

## v1.6.2

- Stabilized large config/panel payload handling between APK, `daemonctl`, and daemon IPC.
- Added `panel.json` install/upgrade support and migration regression coverage.
- Hardened runtime sync, reset cleanup, and mixed APK/module compatibility paths.
- Require matching module and APK artifacts for in-app updates after the storage/API split.

## v1.6.1

- Finalized the Russian-first UI pass across Dashboard, Nodes, Apps, Settings, Audit, and Advisor.
- Localized daemon-generated audit findings and removed audit mapping dependence on English titles by switching to stable finding `code` values.
- Tightened runtime and control-plane user messaging for import, update, root, timeout, and daemon status flows.
- Synchronized app, daemon, module, update feed, and bundled script version metadata to `v1.6.1`.

## v1.6.0

- Russian-first localization pass across Dashboard, Nodes, Apps, Settings, Audit, and Advisor.
- Translated audit findings, advisor recommendations, runtime/import/update errors, and settings statuses.
- Switched audit finding mapping to stable `code` values so localization no longer depends on English titles.
- Synchronized release metadata for app, daemon, module, update feed, and bundled scripts to `v1.6.0`.
