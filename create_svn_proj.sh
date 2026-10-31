#!/usr/bin/env bash
#
# 生成可抛弃的 SVN 演示环境：<脚本目录>/tmp/repo（仓库）+ tmp/wc（工作副本）。
# 说明见 docs/svn-demo.md。
#
# 用法：
#   ./create_svn_proj.sh
#   DEMO_DIR=/tmp/lazysvn-demo ./create_svn_proj.sh
#
# 注意：每次执行都会先删除目标目录再重建。
#      默认目标 <脚本目录>/tmp 是用户保留的测试基准（见 docs/svn-demo.md 保留约定），
#      非明确需要重置演示环境时不要运行。
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
DEMO="${DEMO_DIR:-${SCRIPT_DIR}/tmp}"

log() { printf '[svn-demo] %s\n' "$*"; }
die() { printf '[svn-demo] 错误: %s\n' "$*" >&2; exit 1; }

command -v svn >/dev/null 2>&1 || die "未找到 svn，请先安装 subversion"
command -v svnadmin >/dev/null 2>&1 || die "未找到 svnadmin，请先安装 subversion"

log "目标目录: ${DEMO}"
rm -rf "${DEMO}"
mkdir -p "${DEMO}"

svnadmin create "${DEMO}/repo"
# file:// 协议下默认匿名只读，放开写权限，便于无密码提交
sed -i 's/^# *anon-access *= *read/anon-access = write/' "${DEMO}/repo/conf/svnserve.conf"
grep -q '^anon-access = write' "${DEMO}/repo/conf/svnserve.conf" ||
	printf 'anon-access = write\n' >> "${DEMO}/repo/conf/svnserve.conf"

svn checkout -q "file://${DEMO}/repo" "${DEMO}/wc"
cd "${DEMO}/wc"

mkdir -p src include docs tests examples

# --- 提交 1：项目骨架 -------------------------------------------------------
cat > README.md <<'EOF'
# demo-svn

A tiny C demo project used to exercise LazySVN.

## Build

    make
EOF

cat > Makefile <<'EOF'
CC      ?= cc
CFLAGS  ?= -Wall -Wextra -O2

SRC := src/main.c src/util.c
OBJ := $(SRC:.c=.o)
BIN := demo

all: $(BIN)

$(BIN): $(OBJ)
	$(CC) $(CFLAGS) -o $@ $(OBJ)

clean:
	rm -f $(OBJ) $(BIN)
EOF

cat > src/main.c <<'EOF'
#include <stdio.h>
#include "util.h"

int main(int argc, char **argv) {
	(void)argc;
	(void)argv;
	printf("demo-svn %s\n", demo_version());
	return 0;
}
EOF

cat > include/util.h <<'EOF'
#ifndef DEMO_UTIL_H
#define DEMO_UTIL_H

const char *demo_version(void);

#endif
EOF

cat > src/util.c <<'EOF'
#include "util.h"

const char *demo_version(void) {
	return "0.1.0";
}
EOF

svn add -q README.md Makefile src include
svn commit -q -m "初始化项目骨架：Makefile、main.c 与 util 版本接口" --username alice

# --- 提交 2：命令行参数 -----------------------------------------------------
cat > src/main.c <<'EOF'
#include <stdio.h>
#include <string.h>
#include "util.h"

static void print_usage(const char *prog) {
	printf("usage: %s [--version] [--verbose]\n", prog);
}

int main(int argc, char **argv) {
	int verbose = 0;

	for (int i = 1; i < argc; i++) {
		if (strcmp(argv[i], "--version") == 0) {
			printf("demo-svn %s\n", demo_version());
			return 0;
		}
		if (strcmp(argv[i], "--verbose") == 0) {
			verbose = 1;
			continue;
		}
		print_usage(argv[0]);
		return 2;
	}

	printf("demo-svn %s%s\n", demo_version(), verbose ? " (verbose)" : "");
	return 0;
}
EOF

cat > src/util.c <<'EOF'
#include <ctype.h>
#include <string.h>
#include "util.h"

const char *demo_version(void) {
	return "0.2.0";
}

int demo_equals_ignore_case(const char *a, const char *b) {
	while (*a && *b) {
		if (tolower((unsigned char)*a) != tolower((unsigned char)*b)) {
			return 0;
		}
		a++;
		b++;
	}
	return *a == '\0' && *b == '\0';
}
EOF

cat > include/util.h <<'EOF'
#ifndef DEMO_UTIL_H
#define DEMO_UTIL_H

const char *demo_version(void);
int demo_equals_ignore_case(const char *a, const char *b);

#endif
EOF

svn commit -q -m "支持 --version 与 --verbose 参数，新增忽略大小写比较函数" --username bob

# --- 提交 3：文档 -----------------------------------------------------------
cat > docs/USAGE.md <<'EOF'
# Usage

    demo [--version] [--verbose]

Options:

- `--version` 打印版本号后退出
- `--verbose` 输出详细模式提示
EOF

cat > README.md <<'EOF'
# demo-svn

A tiny C demo project used to exercise LazySVN.

## Build

    make

## Usage

See [docs/USAGE.md](docs/USAGE.md).
EOF

svn add -q --parents docs/USAGE.md
svn commit -q -m "补充使用文档，README 增加入口" --username alice

# --- 提交 4：单元测试 -------------------------------------------------------
cat > tests/test_util.c <<'EOF'
#include <assert.h>
#include <stdio.h>
#include "util.h"

int main(void) {
	assert(demo_equals_ignore_case("SVN", "svn"));
	assert(!demo_equals_ignore_case("svn", "git"));
	printf("tests passed\n");
	return 0;
}
EOF

cat > Makefile <<'EOF'
CC      ?= cc
CFLAGS  ?= -Wall -Wextra -O2

SRC := src/main.c src/util.c
OBJ := $(SRC:.c=.o)
BIN := demo

all: $(BIN)

$(BIN): $(OBJ)
	$(CC) $(CFLAGS) -o $@ $(OBJ)

test: tests/test_util.c src/util.c
	$(CC) $(CFLAGS) -Isrc -Iinclude -o tests/test_util tests/test_util.c src/util.c
	./tests/test_util

clean:
	rm -f $(OBJ) $(BIN) tests/test_util
EOF

svn add -q --parents tests/test_util.c
svn commit -q -m "新增 util 单元测试，Makefile 增加 test 目标" --username carol

# --- 提交 5：示例程序 -------------------------------------------------------
cat > examples/demo.c <<'EOF'
#include <stdio.h>
#include "util.h"

/* 展示如何在外部程序中使用 demo_version()。 */
int main(void) {
	printf("linked demo version: %s\n", demo_version());
	return 0;
}
EOF

svn add -q --parents examples/demo.c
svn commit -q -m "添加链接 util 的示例程序" --username bob

# --- 提交 6：修复大小写比较的边界问题 ---------------------------------------
cat > src/util.c <<'EOF'
#include <ctype.h>
#include "util.h"

const char *demo_version(void) {
	return "0.3.0";
}

int demo_equals_ignore_case(const char *a, const char *b) {
	if (a == 0 || b == 0) {
		return a == b;
	}
	while (*a && *b) {
		if (tolower((unsigned char)*a) != tolower((unsigned char)*b)) {
			return 0;
		}
		a++;
		b++;
	}
	return *a == '\0' && *b == '\0';
}
EOF

svn commit -q -m "修复忽略大小写比较对空指针的处理，版本号升至 0.3.0" --username alice

# 根目录 checkpoint 仍在 r0，update 一次让工作副本统一到最新版本，
# 否则在 wc 根执行 svn log 只能看到 r0 之前（为空）。
svn update -q

# --- 制造未提交的工作区变更，便于观察 status -------------------------------
cat > src/pending.c <<'EOF'
#include <stdio.h>

/* 尚未 svn add 的实验文件。 */
int main(void) {
	printf("pending\n");
	return 0;
}
EOF

cat >> src/main.c <<'EOF'

/* TODO: 支持 --quiet 参数 */
EOF

printf '\n## 未提交的实验性改动\n\n这一行只存在于工作副本中。\n' >> README.md

log "完成：${DEMO}/wc（r$(svn info --show-item revision)）"
log "启动：cd ${DEMO}/wc && ${SCRIPT_DIR}/dist/lazysvn-linux-amd64"
