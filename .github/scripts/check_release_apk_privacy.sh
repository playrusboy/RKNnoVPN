#!/usr/bin/env bash
set -euo pipefail

apk="${1:-}"
if [ -z "${apk}" ] || [ ! -f "${apk}" ]; then
  echo "::error title=APK privacy surface::Usage: $0 path/to/app.apk" >&2
  exit 1
fi

sdk_root="${ANDROID_HOME:-${ANDROID_SDK_ROOT:-}}"
if [ -z "${sdk_root}" ]; then
  echo "::error title=APK privacy surface::ANDROID_HOME/ANDROID_SDK_ROOT is not set" >&2
  exit 1
fi

aapt="$(
  find "${sdk_root}/build-tools" -type f -name aapt 2>/dev/null |
    sort -V |
    tail -n 1
)"
if [ -z "${aapt}" ]; then
  echo "::error title=APK privacy surface::Android build-tools aapt not found under ${sdk_root}" >&2
  exit 1
fi

permissions="$("${aapt}" dump permissions "${apk}")"
manifest_tree="$("${aapt}" dump xmltree "${apk}" AndroidManifest.xml)"
fail=0

for permission in \
  android.permission.INTERNET \
  android.permission.ACCESS_NETWORK_STATE \
  android.permission.OTHER_SENSORS \
  android.permission.BIND_VPN_SERVICE; do
  if grep -Fq "${permission}" <<<"${permissions}"; then
    echo "::error title=APK privacy surface::Forbidden permission in release APK: ${permission}" >&2
    fail=1
  fi
done

if grep -Eq 'android[.]net[.]VpnService|android[.]permission[.]BIND_VPN_SERVICE|foregroundServiceType.*vpn' <<<"${manifest_tree}"; then
  echo "::error title=APK privacy surface::Forbidden VPN service surface in release APK manifest" >&2
  fail=1
fi

exit "${fail}"
