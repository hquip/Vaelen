#!/usr/bin/env bash
# Install the official VSM CLI into a user-owned directory after SHA256 checking.
set -euo pipefail
if [[ "${1:-}" == --help ]]; then
  echo 'Usage: VSM_VERSION=v1.1.3 VSM_INSTALL_DIR="$HOME/.local/bin" bash install.sh'
  echo 'Without VSM_VERSION, installs the latest stable official GitHub release.'
  exit 0
fi
case "$(uname -s)" in Darwin) vsm_os=darwin ;; Linux) vsm_os=linux ;; *) echo 'Use install.ps1 on Windows.' >&2; exit 1 ;; esac
case "$(uname -m)" in x86_64|amd64) vsm_arch=amd64 ;; arm64|aarch64) vsm_arch=arm64 ;; *) echo 'Unsupported architecture.' >&2; exit 1 ;; esac
vsm_repository=https://github.com/hquip/Vaelen
vsm_version="${VSM_VERSION:-}"
if [[ -z "$vsm_version" ]]; then
  vsm_latest="$(curl --proto '=https' --tlsv1.2 -fsSLI --connect-timeout 15 --max-time 45 -o /dev/null -w '%{url_effective}' "$vsm_repository/releases/latest")"
  vsm_version="${vsm_latest##*/}"
fi
[[ "$vsm_version" == v* ]] || vsm_version="v$vsm_version"
[[ "$vsm_version" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] || { echo 'Expected a stable version such as v1.1.3.' >&2; exit 1; }
vsm_directory="${VSM_INSTALL_DIR:-$HOME/.local/bin}"
mkdir -p "$vsm_directory"
vsm_directory="$(cd "$vsm_directory" && pwd)"
vsm_stage="$(mktemp -d "$vsm_directory/.vsm-install.XXXXXXXX")"
vsm_asset="vsm-$vsm_os-$vsm_arch"
trap 'rm -f "$vsm_stage/$vsm_asset" "$vsm_stage/SHA256SUMS"; rmdir "$vsm_stage" 2>/dev/null || true' EXIT
vsm_base="$vsm_repository/releases/download/$vsm_version"
curl --proto '=https' --tlsv1.2 -fSL --retry 2 --connect-timeout 15 --max-time 600 "$vsm_base/$vsm_asset" -o "$vsm_stage/$vsm_asset"
curl --proto '=https' --tlsv1.2 -fsSL --retry 2 --connect-timeout 15 --max-time 60 "$vsm_base/SHA256SUMS" -o "$vsm_stage/SHA256SUMS"
vsm_expected="$(awk -v name="$vsm_asset" '$2 == name { print $1 }' "$vsm_stage/SHA256SUMS")"
[[ "$vsm_expected" =~ ^[0-9a-fA-F]{64}$ ]] || { echo 'Missing or ambiguous official checksum.' >&2; exit 1; }
if command -v sha256sum >/dev/null 2>&1; then
  vsm_actual="$(sha256sum "$vsm_stage/$vsm_asset" | awk '{print $1}')"
else
  vsm_actual="$(shasum -a 256 "$vsm_stage/$vsm_asset" | awk '{print $1}')"
fi
[[ "$vsm_actual" == "$vsm_expected" ]] || { echo 'SHA256 verification failed. Existing VSM was kept.' >&2; exit 1; }
chmod 755 "$vsm_stage/$vsm_asset"
if [[ -e "$vsm_directory/vsm" || -L "$vsm_directory/vsm" ]]; then
  [[ -f "$vsm_directory/vsm" && ! -L "$vsm_directory/vsm" ]] || { echo 'Destination is not a regular executable.' >&2; exit 1; }
  vsm_backup="$vsm_directory/vsm.old.$(date +%s).$$"
  mv "$vsm_directory/vsm" "$vsm_backup"
  if ! mv "$vsm_stage/$vsm_asset" "$vsm_directory/vsm"; then mv "$vsm_backup" "$vsm_directory/vsm"; exit 1; fi
  echo "Previous executable: $vsm_backup"
else
  mv "$vsm_stage/$vsm_asset" "$vsm_directory/vsm"
fi
echo "Installed VSM $vsm_version: $vsm_directory/vsm"
echo "Next: \"$vsm_directory/vsm\" path-init"
