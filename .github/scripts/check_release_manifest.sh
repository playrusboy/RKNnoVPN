#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

fail() {
  echo "release manifest: $*" >&2
  exit 1
}

version_code() {
  tools/version.sh code "$1" || fail "invalid versionCode input: $1"
}

extract_go_version() {
  local file="$1"
  sed -n 's/^[[:space:]]*\(var[[:space:]]\+\)\{0,1\}Version[[:space:]]*=[[:space:]]*"\([^"]*\)".*/\2/p' "$file" | head -n 1
}

release_version="$(tools/version.sh current)"
release_code="$(version_code "$release_version")"
daemon_version="$(extract_go_version daemon/cmd/daemon/main.go)"
ctl_version="$(extract_go_version daemon/cmd/daemonctl/main.go)"
module_version="$(sed -n 's/^version=//p' module/module.prop | head -n 1)"
module_code="$(sed -n 's/^versionCode=//p' module/module.prop | head -n 1)"
update_version="$(sed -n 's/[[:space:]]*"version"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' update.json | head -n 1)"
update_code="$(sed -n 's/[[:space:]]*"versionCode"[[:space:]]*:[[:space:]]*\([0-9][0-9]*\).*/\1/p' update.json | head -n 1)"

[ -n "$release_version" ] || fail "VERSION is empty"
[ -n "$release_code" ] || fail "VERSION does not produce a versionCode"
[ "$daemon_version" = "dev" ] || fail "daemon source default Version must stay dev; release builds stamp it with ldflags"
[ "$ctl_version" = "dev" ] || fail "daemonctl source default Version must stay dev; release builds stamp it with ldflags"
grep -q 'versionName = rknnoVpnVersionName' app/app/build.gradle.kts || fail "APK versionName must come from the shared VERSION source"
grep -q 'versionCode = rknnoVpnVersionCode' app/app/build.gradle.kts || fail "APK versionCode must come from the shared VERSION formula"

[ -n "$module_version" ] || fail "module version not found"
[ -n "$module_code" ] || fail "module versionCode not found"
[ "$module_code" = "$(version_code "$module_version")" ] || fail "module versionCode $module_code does not match $module_version"
[ -n "$update_version" ] || fail "update.json version not found"
[ -n "$update_code" ] || fail "update.json versionCode not found"
[ "$update_code" = "$(version_code "$update_version")" ] || fail "update.json versionCode $update_code does not match $update_version"

for required in \
  module/META-INF/com/google/android/update-binary \
  module/META-INF/com/google/android/updater-script \
  module/scripts/lib/rknnovpn_env.sh \
  module/scripts/lib/rknnovpn_install.sh \
  module/scripts/lib/rknnovpn_installer_flow.sh \
  module/scripts/lib/rknnovpn_netstack.sh \
  module/scripts/lib/rknnovpn_iptables_rules.sh \
  module/scripts/rescue_reset.sh \
  module/scripts/routing.sh \
  module/scripts/iptables.sh \
  module/scripts/dns.sh \
  module/OWNERSHIP.md \
  module/customize.sh \
  module/service.sh \
  module/post-fs-data.sh \
  module/uninstall.sh; do
  [ -f "$required" ] || fail "required module file missing: $required"
done

if [ "$(tr -d '\r\n' < module/META-INF/com/google/android/updater-script)" != "#MAGISK" ]; then
  fail "Magisk updater-script must contain only #MAGISK"
fi

if ! grep -q 'install_module' module/META-INF/com/google/android/update-binary; then
  fail "Magisk update-binary must invoke install_module"
fi

if ! grep -q 'ARCH_ABI_DIR="arm64-v8a"' module/scripts/lib/rknnovpn_installer_flow.sh; then
  fail "installer must support arm64-v8a binary alias"
fi

if ! grep -q 'arm64-v8a arm64' module/service.sh; then
  fail "service binary restore must support arm64-v8a and arm64 aliases"
fi

if grep -REn 'cp -a .*binaries/(arm64-v8a|armeabi-v7a)|Add Android ABI binary aliases' .github/workflows Makefile >/tmp/release-manifest-abi-copies.$$ 2>/dev/null; then
  cat /tmp/release-manifest-abi-copies.$$ >&2
  rm -f /tmp/release-manifest-abi-copies.$$
  fail "release packaging must resolve Android ABI aliases in scripts, not duplicate binary directories"
fi
rm -f /tmp/release-manifest-abi-copies.$$

expected_zip="https://github.com/youtubediscord/RKNnoVPN/releases/download/${update_version}/rknnovpn-${update_version}-module.zip"
expected_changelog="https://github.com/youtubediscord/RKNnoVPN/releases/tag/${update_version}"
update_zip="$(sed -n 's/[[:space:]]*"zipUrl"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' update.json | head -n 1)"
update_changelog="$(sed -n 's/[[:space:]]*"changelog"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' update.json | head -n 1)"
[ "$update_zip" = "$expected_zip" ] || fail "update.json zipUrl is not $expected_zip"
[ "$update_changelog" = "$expected_changelog" ] || fail "update.json changelog is not $expected_changelog"

if grep -REn 's/v//;s/\\?\.//g|s/v//;s/\.//g' .github/workflows Makefile >/tmp/release-manifest-old-code.$$ 2>/dev/null; then
  cat /tmp/release-manifest-old-code.$$ >&2
  rm -f /tmp/release-manifest-old-code.$$
  fail "old dot-stripping versionCode formula is forbidden"
fi
rm -f /tmp/release-manifest-old-code.$$

if grep -REn 'major \* 1000|minor \* 100 \+ patch' .github/workflows Makefile app/app/build.gradle.kts >/tmp/release-manifest-old-code.$$ 2>/dev/null; then
  cat /tmp/release-manifest-old-code.$$ >&2
  rm -f /tmp/release-manifest-old-code.$$
  fail "old colliding versionCode formula is forbidden"
fi
rm -f /tmp/release-manifest-old-code.$$

if [ "$(version_code v1.8.0)" != "1080000" ] || [ "$(version_code v1.7.13)" != "1071300" ]; then
  fail "canonical version_code formula must reserve separate two-digit minor, patch, and post-tag fields"
fi
if [ "$(version_code v1.10.0)" = "$(version_code v2.0.0)" ]; then
  fail "canonical version_code formula collides for v1.10.0 and v2.0.0"
fi
if [ "$(version_code v2.1.99)" = "$(version_code v2.1.0-99-gabcdef0)" ]; then
  fail "canonical version_code formula collides patch and post-tag builds"
fi
if ! grep -q 'VERSION' Makefile || ! grep -q 'rootProject.file("../VERSION")' app/app/build.gradle.kts; then
  fail "release version must stay centralized in the root VERSION file"
fi
if grep -REn 'SCRIPT_VERSION="v[0-9]|Version = "v[0-9]' daemon/cmd module/scripts >/tmp/release-manifest-hardcoded-version.$$ 2>/dev/null; then
  cat /tmp/release-manifest-hardcoded-version.$$ >&2
  rm -f /tmp/release-manifest-hardcoded-version.$$
  fail "runtime source must not hardcode release versions"
fi
rm -f /tmp/release-manifest-hardcoded-version.$$

if [ "$(version_code "$release_version")" != "$release_code" ]; then
  fail "canonical version_code formula must reserve separate two-digit minor, patch, and post-tag fields"
fi

echo "release manifest: ${release_version} (${release_code}) ok"
