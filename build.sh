#!/usr/bin/env bash
# 一键构建 MES 转换工具。
#
#   1. 校验 version.go 与 versioninfo.json 的版本号一致
#   2. go vet + go test
#   3. 生成图标/版本资源（需要 goversioninfo；没有就沿用已有 .syso）
#   4. 编译 exe（注入构建时间）
#   5. 同步到分享版目录
#
# 用法：./build.sh          正常构建
#      ./build.sh -fast     跳过 vet/test（只编译）

set -euo pipefail
cd "$(dirname "$0")"
ROOT="$(pwd)"
MES="$ROOT/mes_conv"
SHARE="$ROOT/MES物料档案转换工具_分享版"
EXE_NAME="物料档案转换工具.exe"
FAST=0
[ "${1:-}" = "-fast" ] && FAST=1

die() { echo "✗ $*" >&2; exit 1; }
step() { echo; echo "==> $*"; }

# ---------- 1. 版本号一致性 ----------
step "校验版本号"
VER="$(grep -oP 'const appVersion = "\K[^"]+' "$MES/version.go" || true)"
[ -n "$VER" ] || die "读不到 mes_conv/version.go 里的 appVersion"
VINFO_VER="$(grep -oP '"FileVersion": "\K[^"]+' "$MES/versioninfo.json" | head -1)"
[ -n "$VINFO_VER" ] || die "读不到 mes_conv/versioninfo.json 里的 FileVersion"
if [ "${VINFO_VER%.0}" != "$VER" ]; then
  die "版本号不一致：version.go=$VER，versioninfo.json=$VINFO_VER（应为 $VER.0）"
fi
echo "  version.go=$VER  versioninfo.json=$VINFO_VER  ✓"

cd "$MES"

# ---------- 2. 静态检查与测试 ----------
if [ "$FAST" = "0" ]; then
  step "go vet"
  GOPROXY=https://goproxy.cn,direct go vet ./...
  step "go test"
  GOPROXY=https://goproxy.cn,direct go test ./...
fi

# ---------- 3. 版本资源 ----------
step "生成版本资源"
GVI="${GOVERSIONINFO:-}"
if [ -z "$GVI" ]; then
  for c in "$HOME/go/bin/goversioninfo.exe" "$HOME/go/bin/goversioninfo"; do
    [ -x "$c" ] && GVI="$c" && break
  done
fi
if [ -n "$GVI" ] && [ -x "$GVI" ]; then
  "$GVI" -o resource_windows_amd64.syso versioninfo.json
  echo "  已用 $GVI 重新生成 resource_windows_amd64.syso"
else
  if [ -f resource_windows_amd64.syso ]; then
    echo "  ! 未找到 goversioninfo，沿用已有 resource_windows_amd64.syso（版本资源可能过期）"
    echo "    安装：go install github.com/josephspurrier/goversioninfo/cmd/goversioninfo@latest"
  else
    echo "  ! 未找到 goversioninfo 且没有 .syso，编译出的 exe 将没有图标与版本信息"
  fi
fi

# ---------- 4. 编译 ----------
step "编译 exe"
BT="$(date "+%Y-%m-%d %H:%M")"
GOPROXY=https://goproxy.cn,direct go build \
  -ldflags "-s -w -H=windowsgui -X \"main.buildTime=$BT\"" \
  -o "$EXE_NAME" .
SIZE="$(stat -c%s "$EXE_NAME")"
echo "  已生成 $EXE_NAME（$((SIZE/1048576)) MB，构建时间 $BT）"

# ---------- 5. 同步分享版 ----------
step "同步分享版目录"
mkdir -p "$SHARE"
cp -f "$EXE_NAME" "$SHARE/"
cp -f mapping.json "$SHARE/"
[ -f orders.json ] && cp -f orders.json "$SHARE/"
# 界面已拆到 webui/ 并内嵌进 exe，不再单独同步 webui.html。
# 若分享版里残留旧的 webui.html，程序会把它当作「磁盘覆盖」优先读取，务必清掉。
rm -f "$SHARE/webui.html"
echo "  已同步：$EXE_NAME / mapping.json"
echo "  提示：分享版的 README.md 是手写文档，脚本不覆盖"

echo
echo "完成 ✓  版本 $VER  构建时间 $BT"
