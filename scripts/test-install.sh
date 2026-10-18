#!/usr/bin/env bash
set -euo pipefail
vsm_test_root="$(mktemp -d)"
vsm_test_script="$(cd "$(dirname "$0")" && pwd)/install.sh"
mkdir -p "$vsm_test_root/mock"
cat > "$vsm_test_root/mock/curl" <<'MOCK'
#!/usr/bin/env bash
set -euo pipefail
vsm_mock_url= vsm_mock_out=
while (( $# )); do
  case "$1" in -o) shift; vsm_mock_out="$1" ;; https://*) vsm_mock_url="$1" ;; esac
  shift
done
case "$vsm_mock_url" in
  https://github.com/hquip/Vaelen/releases/download/v1.0.8/SHA256SUMS) cp "$VSM_TEST_FIXTURE/SHA256SUMS" "$vsm_mock_out" ;;
  https://github.com/hquip/Vaelen/releases/download/v1.0.8/vsm-*) cp "$VSM_TEST_FIXTURE/binary" "$vsm_mock_out" ;;
  *) echo "Unexpected network request: $vsm_mock_url" >&2; exit 1 ;;
esac
MOCK
chmod 755 "$vsm_test_root/mock/curl"
case "$(uname -s)" in Darwin) vsm_test_os=darwin ;; *) vsm_test_os=linux ;; esac
case "$(uname -m)" in arm64|aarch64) vsm_test_arch=arm64 ;; *) vsm_test_arch=amd64 ;; esac
for vsm_test_mode in success bad-checksum; do
  vsm_test_case="$vsm_test_root/$vsm_test_mode with spaces"
  mkdir -p "$vsm_test_case/fixtures" "$vsm_test_case/bin"
  printf 'old-program' > "$vsm_test_case/bin/vsm"
  printf 'new-program' > "$vsm_test_case/fixtures/binary"
  if command -v sha256sum >/dev/null 2>&1; then
    vsm_test_hash="$(sha256sum "$vsm_test_case/fixtures/binary" | awk '{print $1}')"
  else
    vsm_test_hash="$(shasum -a 256 "$vsm_test_case/fixtures/binary" | awk '{print $1}')"
  fi
  if [[ "$vsm_test_mode" == bad-checksum ]]; then vsm_test_hash=0000000000000000000000000000000000000000000000000000000000000000; fi
  printf '%s  vsm-%s-%s\n' "$vsm_test_hash" "$vsm_test_os" "$vsm_test_arch" > "$vsm_test_case/fixtures/SHA256SUMS"
  if PATH="$vsm_test_root/mock:$PATH" VSM_TEST_FIXTURE="$vsm_test_case/fixtures" VSM_VERSION=v1.0.8 VSM_INSTALL_DIR="$vsm_test_case/bin" bash "$vsm_test_script"; then
    [[ "$vsm_test_mode" == success && "$(cat "$vsm_test_case/bin/vsm")" == new-program ]]
  else
    [[ "$vsm_test_mode" == bad-checksum && "$(cat "$vsm_test_case/bin/vsm")" == old-program ]]
  fi
done
echo 'Installer checksum, backup and paths with spaces passed.'
