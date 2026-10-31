#!/usr/bin/env bash
#
# 本地构建脚本：编译 Linux 可执行文件并 strip。
# 设计与边界条件见 docs/build.md。
#
# 用法：
#   ./build.sh
#   VERSION=v1.2.3 ./build.sh
#   STRIP=0 ./build.sh        # 跳过 strip（仅排障用）
#
# 可选环境变量：
#   OUT_DIR   输出目录，默认 dist
#   VERSION   注入版本号，默认最近 git tag（无 tag 则不注入）
#   STRIP     1=执行 strip（默认），0=跳过
#
# 本脚本只构建 linux/x86_64（amd64），不提供交叉编译。
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
cd "${SCRIPT_DIR}"

log() { printf '[build] %s\n' "$*"; }
die() { printf '[build] 错误: %s\n' "$*" >&2; exit 1; }

command -v go >/dev/null 2>&1 || die "未找到 go，请先安装 Go 1.25+"

TARGET_GOOS="linux"
TARGET_GOARCH="amd64"

# 用 GOHOSTOS/GOHOSTARCH 而非 GOOS/GOARCH：后者会被同名环境变量覆盖，
# 无法反映构建机真实架构。
HOST_GOOS="$(go env GOHOSTOS)"
HOST_GOARCH="$(go env GOHOSTARCH)"

[ "${HOST_GOOS}" = "${TARGET_GOOS}" ] || die "构建机必须是 Linux（当前 ${HOST_GOOS}）"
[ "${HOST_GOARCH}" = "${TARGET_GOARCH}" ] || die "只构建 x86_64（amd64）产物，当前构建机为 ${HOST_GOARCH}；arm 等架构请使用 CI"

if [ -n "${GOOS:-}" ] && [ "${GOOS}" != "${TARGET_GOOS}" ]; then
	die "GOOS=${GOOS} 不受支持，本脚本只构建 linux/x86_64"
fi
if [ -n "${GOARCH:-}" ] && [ "${GOARCH}" != "${TARGET_GOARCH}" ]; then
	die "GOARCH=${GOARCH} 不受支持，本脚本只构建 linux/x86_64"
fi

GOOS="${TARGET_GOOS}"
GOARCH="${TARGET_GOARCH}"
OUT_DIR="${OUT_DIR:-dist}"
STRIP="${STRIP:-1}"

# 版本号会拼进 -ldflags；只接受 main.go normalizeVersion 认可的形式，
# 否则注入值会被静默丢弃（--version 显示 0.0.0）。
# 默认取最近 tag（与程序内置 gitTagVersion 一致）；无 tag 则不注入。
VERSION="${VERSION:-$(git describe --tags --abbrev=0 2>/dev/null || true)}"
if [ -n "${VERSION}" ] && ! [[ "${VERSION}" =~ ^v?[0-9]+(\.[0-9]+){0,2}$ ]]; then
	die "VERSION=${VERSION} 不是合法版本号（应为 v?X.Y[.Z]）"
fi

LDFLAGS="-s -w"
if [ -n "${VERSION}" ]; then
	LDFLAGS="${LDFLAGS} -X main.version=${VERSION}"
fi

BIN_NAME="lazysvn"
OUT_PATH="${OUT_DIR}/${BIN_NAME}-${GOOS}-${GOARCH}"

mkdir -p "${OUT_DIR}"

log "版本:   ${VERSION:-<未注入，由程序自行判断>}"
log "目标:   ${GOOS}/${GOARCH}"
log "产物:   ${OUT_PATH}"

CGO_ENABLED=0 GOOS="${GOOS}" GOARCH="${GOARCH}" \
	go build -trimpath \
	-ldflags "${LDFLAGS}" \
	-o "${OUT_PATH}" .

if [ "${STRIP}" != "0" ]; then
	command -v strip >/dev/null 2>&1 || die "未找到 strip（binutils），请安装后再试，或设置 STRIP=0 跳过"
	strip --strip-all "${OUT_PATH}"
	log "已 strip: ${OUT_PATH}"
else
	log "STRIP=0，跳过 strip"
fi

if command -v file >/dev/null 2>&1; then
	ELF_INFO="$(file -b "${OUT_PATH}")"
	log "类型:   ${ELF_INFO}"
	case "${ELF_INFO}" in
		*ELF*)
			;;
		*)
			die "产物不是 ELF 文件: ${OUT_PATH}"
			;;
	esac
	if [ "${STRIP}" != "0" ]; then
		case "${ELF_INFO}" in
			*stripped*)
				;;
			*)
				die "产物未被 strip: ${OUT_PATH}"
				;;
		esac
	fi
fi

if command -v du >/dev/null 2>&1; then
	log "大小:   $(du -h "${OUT_PATH}" | cut -f1)"
fi

log "完成"
