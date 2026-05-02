#!/usr/bin/env bash
set -euo pipefail

rknnovpn_project_root() {
  local script_dir
  script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
  cd "${script_dir}/.." && pwd
}

rknnovpn_current_version() {
  local root
  root="$(rknnovpn_project_root)"
  printf '%s\n' "$(tr -d '[:space:]' < "${root}/VERSION")"
}

rknnovpn_version_code() {
  local version="${1#v}"
  local major minor patch
  if [[ "$version" =~ ^([0-9]+)\.([0-9]+)\.([0-9]+)$ ]]; then
    major="${BASH_REMATCH[1]}"
    minor="${BASH_REMATCH[2]}"
    patch="${BASH_REMATCH[3]}"
  else
    echo "invalid version: $1" >&2
    return 1
  fi
  if (( 10#$major > 9 || 10#$patch > 99 )); then
    echo "versionCode supports major 0..9 and patch 0..99: $1" >&2
    return 1
  fi
  printf '%d%d%02d\n' "$((10#$major))" "$((10#$minor))" "$((10#$patch))"
}

rknnovpn_ci_version() {
  local ref_name="$1"
  local run_number="${2:-0}"
  local sha="${3:-0000000}"
  if [[ "$ref_name" == v* ]]; then
    echo "$ref_name"
  else
    echo "dev-${run_number}-${sha:0:7}"
  fi
}

case "${1:-}" in
  current)
    rknnovpn_current_version
    ;;
  code)
    rknnovpn_version_code "${2:-$(rknnovpn_current_version)}"
    ;;
  ci)
    rknnovpn_ci_version "${2:-}" "${3:-}" "${4:-}"
    ;;
  *)
    echo "usage: $0 current | code [version] | ci <ref-name> <run-number> <sha>" >&2
    exit 2
    ;;
esac
