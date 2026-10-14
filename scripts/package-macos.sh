#!/usr/bin/env bash
# 构建 macOS 通用版（Apple Silicon + Intel）VSM.app，内置 vsm CLI，并打包为 zip。
#
#   用法: scripts/package-macos.sh <版本号> [输出目录]
#
# 必须在 macOS 上运行：Wails GUI 依赖系统 WebKit 与 CGO，无法从其它平台交叉编译。
# CI（每个 PR）与 Release（打 tag）都调用本脚本，保证发布前就验证过完整打包流程。
set -euo pipefail

VERSION="${1:?用法: $0 <版本号> [输出目录]}"
OUT="${2:-dist}"

cd "$(dirname "$0")/.."
mkdir -p "$OUT"
OUT="$(cd "$OUT" && pwd)"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

# 不设置时 CGO 产物的最低系统版本等于 runner 的 SDK 版本（macOS 14），
# macOS 12/13 用户会直接打不开。Go 1.25 本身要求 macOS 12+，与之对齐。
export MACOSX_DEPLOYMENT_TARGET=12.0
LDFLAGS="-s -w -X vsm/internal/cli.version=${VERSION}"

# build <输出> <GOARCH> <CGO> <包> [构建标签]
# setup-go 的构建缓存在并发 run 下偶发损坏，失败时清缓存重试。
build() {
  local out="$1" arch="$2" cgo="$3" pkg="$4" tags="${5:-}"
  local i
  for i in 1 2 3; do
    if CGO_ENABLED="$cgo" GOOS=darwin GOARCH="$arch" \
      go build -trimpath ${tags:+-tags "$tags"} -ldflags "$LDFLAGS" -o "$out" "$pkg"; then
      return 0
    fi
    echo "构建 $pkg ($arch) 第 $i 次失败，清理缓存后重试…" >&2
    go clean -cache
    sleep 3
  done
  echo "构建 $pkg ($arch) 失败" >&2
  return 1
}

echo "==> 编译 GUI 与 CLI（arm64 + amd64）"
for arch in arm64 amd64; do
  build "$WORK/vsm-gui-$arch" "$arch" 1 ./gui production
  build "$WORK/vsm-$arch" "$arch" 0 .
done
lipo -create -output "$WORK/vsm-gui" "$WORK/vsm-gui-arm64" "$WORK/vsm-gui-amd64"
lipo -create -output "$WORK/vsm" "$WORK/vsm-arm64" "$WORK/vsm-amd64"
for b in vsm-gui vsm; do
  archs="$(lipo -archs "$WORK/$b")"
  echo "    $b: $archs"
  [[ "$archs" == *arm64* && "$archs" == *x86_64* ]] || { echo "$b 不是通用二进制" >&2; exit 1; }
done

echo "==> 组装 VSM.app"
APP="$WORK/VSM.app"
mkdir -p "$APP/Contents/MacOS" "$APP/Contents/Resources"
cp "$WORK/vsm-gui" "$APP/Contents/MacOS/vsm-gui"
# 内置 CLI：GUI 生成的 shim 与 ~/.zshrc 激活语句都用它的绝对路径，
# 用户无论把 VSM.app 拖到哪里都能直接用。
cp "$WORK/vsm" "$APP/Contents/MacOS/vsm"
printf 'APPL????' > "$APP/Contents/PkgInfo"

# 由 1024px PNG 生成 .icns（Dock / Finder / ⌘-Tab 都靠它显示图标）
ICONSET="$WORK/VSM.iconset"
mkdir -p "$ICONSET"
for s in 16 32 128 256 512; do
  sips -z "$s" "$s" gui/appicon.png --out "$ICONSET/icon_${s}x${s}.png" >/dev/null
  d=$((s * 2))
  sips -z "$d" "$d" gui/appicon.png --out "$ICONSET/icon_${s}x${s}@2x.png" >/dev/null
done
iconutil -c icns "$ICONSET" -o "$APP/Contents/Resources/VSM.icns"

cat >"$APP/Contents/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>CFBundleName</key><string>VSM</string>
  <key>CFBundleDisplayName</key><string>VSM</string>
  <key>CFBundleExecutable</key><string>vsm-gui</string>
  <key>CFBundleIdentifier</key><string>io.github.hquip.vsm</string>
  <key>CFBundlePackageType</key><string>APPL</string>
  <key>CFBundleIconFile</key><string>VSM</string>
  <key>CFBundleShortVersionString</key><string>${VERSION}</string>
  <key>CFBundleVersion</key><string>${VERSION}</string>
  <key>LSMinimumSystemVersion</key><string>${MACOSX_DEPLOYMENT_TARGET}</string>
  <key>LSApplicationCategoryType</key><string>public.app-category.developer-tools</string>
  <key>NSHighResolutionCapable</key><true/>
  <key>NSRequiresAquaSystemAppearance</key><false/>
</dict>
</plist>
PLIST
plutil -lint "$APP/Contents/Info.plist"

echo "==> ad-hoc 签名"
# Apple Silicon 拒绝运行完全未签名的可执行文件；ad-hoc 签名即可满足（--deep 覆盖内置 CLI）
codesign --force --deep --sign - "$APP"
codesign --verify --deep --strict --verbose=2 "$APP"

echo "==> 冒烟测试"
"$APP/Contents/MacOS/vsm" version

ZIP="$OUT/vsm-gui-macos-universal.zip"
ditto -c -k --sequesterRsrc --keepParent "$APP" "$ZIP"
echo "==> 完成: $ZIP ($(du -h "$ZIP" | cut -f1))"
