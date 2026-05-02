# Changelog

## v2.3.5

- Fixed APK startup compatibility checks by keeping `compat.check` on the stable one-shot daemonctl path instead of the experimental bridge path.
- Explicitly removed transitive `INTERNET`, `ACCESS_NETWORK_STATE`, and `OTHER_SENSORS` permissions from the merged APK manifest.
- Updated the APK privacy guard to allow only manifest entries that remove forbidden transitive permissions.
- Synchronized release metadata to `v2.3.5`.

## v2.3.4

- Enabled fast active-node switching by auto-provisioning the local Clash API for multi-node profiles.
- Hardened selector switching with Xray sidecar guards, bearer-authenticated Clash API calls, selector verification, and visible `selector-switch` operation stages.
- Made subscription preview/apply use one-time `previewId` commits so previewed nodes are not fetched and parsed twice.
- Improved node diagnostics with bounded workers, latency-only default checks, authenticated Clash API delay probes, and APK timeouts sized for multi-node batches.
- Kept cached profile data visible during transient daemon/root failures with explicit fresh/stale/error state.
- Synchronized release metadata to `v2.3.4`.

## v2.3.2

- Cached APK compatibility checks by daemon compatibility fingerprint so most RPCs avoid re-fetching the full IPC contract.
- Exposed `contract_hash`, daemon PID, socket inode, and `compatibility_fingerprint` through daemon version/runtime compatibility metadata.
- Synchronized release metadata to `v2.3.2`.

## v2.3.1

- Made `VERSION` the only manual release version source; module metadata is a stamped template and `update.json` remains workflow-generated release feed metadata.
- Switched release `versionCode` generation to fixed semver slots (`v2.3.1` -> `2030100`) with explicit `major/minor/patch < 100` guards.
- Hardened GitHub Actions release stamping so only strict `vMAJOR.MINOR.PATCH` tags are treated as stable releases.

## v2.3.0

- Added a root `VERSION` file as the single manual release version source for local builds, Android Gradle metadata, and GitHub Actions release stamping.
- Replaced the colliding APK/module `versionCode` formula with compact semver digits (`v2.3.0` -> `2300`, `v2.10.0` -> `21000`) and validation that fails clearly outside the supported range.
- Removed hardcoded daemon, daemonctl, module template, and bundled script release versions from source defaults; release builds stamp artifacts and scripts read module metadata at runtime.
- Synchronized module and update feed metadata to `v2.3.0`.

## v2.2.11

- Fixed classic DNS interception so redirected `dns-in` traffic is always handled by sing-box DNS instead of falling through to the selected proxy node.
- Added an explicit TCP/UDP port 53 `hijack-dns` route for DNS that reaches the shared TPROXY inbound.
- Made runtime DNS health checks perform a real lookup through the local DNS listener.
- Fixed Dashboard uptime and TX/RX traffic counters so runtime status no longer stays at `00:00` and `0 B`.
- Synchronized app, daemon, daemonctl, module, update feed, and bundled script version metadata to `v2.2.11`.

## v2.2.10

- Removed duplicate Android ABI binary directories from the Magisk module ZIP so release size returns to the compact two-architecture layout.
- Kept installer, service restore, and daemon updater compatibility with Android ABI names by resolving `arm64-v8a` to packaged `arm64` and `armeabi-v7a` to packaged `armv7`.
- Added a release ZIP guard that rejects duplicate ABI alias binary directories in GitHub Actions.
- Synchronized app, daemon, daemonctl, module, update feed, and bundled script version metadata to `v2.2.10`.

## v2.2.9

- Fixed Magisk module updates on arm64-v8a devices by packaging Android ABI binary aliases alongside the normalized updater paths.
- Made module installation and service binary restore accept both Android ABI names (`arm64-v8a`, `armeabi-v7a`) and normalized names (`arm64`, `armv7`).
- Kept in-app updater compatibility with existing `binaries/arm64` and `binaries/armv7` release layouts.
- Synchronized app, daemon, daemonctl, module, update feed, and bundled script version metadata to `v2.2.9`.

## v2.2.8

- Suppressed early Dashboard daemon/module poll errors during the first boot window before the daemon reports its first successful status.
- Ignored stale failed runtime operations from a previous APK process when the user has not requested a new runtime action.
- Fixed app routing copy for "Proxy apps as processes" in English and Russian.
- Fixed sing-box DNS rendering so direct/bootstrap DNS servers no longer detour through a missing `direct` outbound.
- Sped up module boot cleanup by using `pidof` for daemon process discovery.
- Synchronized app, daemon, daemonctl, module, update feed, and bundled script version metadata to `v2.2.8`.

## v2.2.6

- Fixed VLESS subscription imports so WebSocket, gRPC, HTTP, HTTPUpgrade, and XHTTP transport settings are preserved instead of silently falling back to incomplete transport configs.
- Fixed VLESS/XHTTP sidecar startup for stored nodes by ensuring Xray receives `encryption: "none"` for every VLESS user.
- Added runtime fallback from the original proxy link so already imported WS/gRPC nodes can recover missing transport fields without manual re-import.
- Synchronized app, daemon, daemonctl, module, update feed, and bundled script version metadata to `v2.2.6`.

## v2.2.5

- Fixed XHTTP sidecar DNS routing so bootstrap/direct DNS no longer depends on the same proxy path that is being established.
- Preserved imported nodes across Magisk module updates by moving the runtime profile to persistent module-owned state.
- Sped up manual key import by avoiding unnecessary runtime reload during direct node import.
- Fixed manual imports so the newly imported live node becomes the active node.
- Synchronized app, daemon, daemonctl, module, update feed, and bundled script version metadata to `v2.2.5`.

## v2.2.4

- Fixed HTTPS URL checks on Android by loading system and user-added CA roots in daemon transparent probes.
- Made selected-node URL checks use the active transparent route even when multiple nodes are stored and Clash API is disabled.
- Fixed diagnostics so URL/TLS failures are not masked as DNS bootstrap failures.
- Fixed runtime status after profile apply so applied state follows the newly selected active node.
- Sped up node switching by skipping unnecessary netstack reapply during hot-swap when runtime env is unchanged.
- Synchronized app, daemon, daemonctl, module, update feed, and bundled script version metadata to `v2.2.4`.

## v2.2.3

- Fixed first-run daemon repair waits so the APK can survive slow rooted daemon startup before importing keys.
- Added direct import support for sing-box outbound JSON and full configs with `outbounds`, alongside proxy links and WireGuard configs.
- Fixed imported server visibility by selecting the normalized group after import.
- Removed the default Russia bypass dependency on remote GitHub SRS rule-set downloads during sing-box startup.
- Improved runtime diagnostics: full diagnostic bundles are shared from Settings, device-lab collection uses current daemonctl methods, and unavailable node helper probes no longer mark servers unusable.
- Clarified degraded URL-probe messaging so successful `commit-state` is not shown as the failed stage.
- Synchronized app, daemon, daemonctl, module, update feed, and bundled script version metadata to `v2.2.3`.

## v2.2.2

- Fixed remote sing-box rule-set downloads so startup does not depend on the selected proxy node while fetching GitHub SRS files.
- Fixed updater and subscription TLS on Android by sharing Android system and user-added CA certificate loading.
- Restored subscription server ordering: refreshed providers follow the subscription order, while stale removed nodes stay at the end.
- Made the server list default to source order and added an explicit sort option for the subscription order.
- Fixed Settings text wrapping in app process routing and recovery policy cards.
- Passed the APK version to daemon update checks/downloads so update status compares against the installed panel version.
- Made APK daemonctl calls namespace-aware so update/recovery operations can reach the active daemon environment.
- Synchronized app, daemon, daemonctl, module, update feed, and bundled script version metadata to `v2.2.2`.

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
