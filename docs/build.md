# 本地构建脚本（build.sh）

## 目的

在仓库根目录提供可重复执行的本地构建入口 `build.sh`，用于编译 **linux/x86_64（amd64）** 可执行文件，并对产物执行 strip，得到体积最小、可直接运行的 `lazysvn` 二进制。脚本不提供交叉编译，也不产出 arm 等架构。

现有 CI（`.github/workflows/release.yml`）使用：

```sh
CGO_ENABLED=0 GOOS=<os> GOARCH=<arch> go build -trimpath -ldflags "-s -w" -o <bin> .
```

`build.sh` 复用同一套参数，额外在链接完成后调用 binutils 的 `strip` 二次精简，并注入版本号。

## 脚本行为

1. 以脚本所在目录（仓库根目录）为工作目录执行，不依赖调用者的当前路径。
2. 读取环境变量（均可选）：

   | 变量 | 默认值 | 说明 |
   | --- | --- | --- |
   | `OUT_DIR` | `dist` | 产物目录 |
   | `VERSION` | 最近 tag（`git describe --tags --abbrev=0`）；无 tag 则不注入 | 通过 `-ldflags -X main.version=` 注入 |
   | `STRIP` | `1` | 设为 `0` 可跳过 strip（仅用于排障） |

   目标平台固定为 `GOOS=linux`、`GOARCH=amd64`，不接受覆盖。

3. 构建命令：

   ```sh
   CGO_ENABLED=0 GOOS="$GOOS" GOARCH="$GOARCH" \
     go build -trimpath -ldflags "-s -w [-X main.version=$VERSION]" \
     -o "$OUT_DIR/lazysvn-$GOOS-$GOARCH" .
   ```

   - `CGO_ENABLED=0`：静态链接，避免运行环境缺少 glibc 版本兼容问题。
   - `-trimpath`：去掉构建机绝对路径，产物可复现、体积更小。
   - `-s -w`：省略符号表与 DWARF 调试信息。
   - 仅当 `VERSION` 非空时才追加 `-X main.version=`。`main.go` 的 `normalizeVersion` 只接受 `v?X.Y[.Z]`，注入 commit hash 会被丢弃（`--version` 显示 `0.0.0`），因此默认取最近 tag，无 tag 时交给程序自行判断。
4. 目标平台硬编码为 `linux/amd64`：
   - 构建机本身不是 `linux/amd64` 时直接报错（宿主 strip 无法处理非本机 ELF，且不做交叉编译）。
   - 若外部设置了 `GOOS` 或 `GOARCH` 且值不是 `linux` / `amd64`，脚本报错退出，避免误以为支持 arm 等架构。
   - 多架构发布仍由 CI 负责。
5. strip：链接完成后执行 `strip --strip-all`。
   - 因目标固定为本机 x86_64，宿主 binutils 一定可以处理该 ELF。
   - 宿主未安装 `strip` 时脚本报错退出（可通过 `STRIP=0` 显式跳过，仅用于排障）。
6. 输出产物路径、大小，并用 `file` 打印 ELF 类型，便于确认确实是 Linux 可执行文件。

## 边界条件与风险

- 产物依赖 `svn` 命令行可执行文件（运行时调用 `svn`），构建本身不打包 svn。
- `strip --strip-all` 作用在已带 `-s -w` 的 Go 二进制上，只会进一步去除剩余符号/节区，不影响 Go 运行时反射、panic 栈符号与 `.gopclntab`，程序可正常运行。
- 版本注入值必须匹配 `v?X.Y[.Z]`，否则脚本报错退出（避免注入后被程序静默丢弃）。
- 若仓库没有 git 元数据（如源码包），`git describe` 失败后不注入版本，不影响构建。
- 不修改 `.gitignore`：`dist/` 与根目录 `lazysvn` 已被忽略。
- 本脚本只产出 linux/x86_64；arm64 等架构不在支持范围，需要时走 `.github/workflows/release.yml`。

## 验证方式

```sh
./build.sh
./dist/lazysvn-linux-amd64 --version   # 应输出版本号
file ./dist/lazysvn-linux-amd64        # 应为 ELF 64-bit LSB executable, x86-64, statically linked, stripped
./dist/lazysvn-linux-amd64 --help      # 冒烟检查
```

回归检查点：

1. 连续执行两次脚本均成功，产物 SHA 一致（可复现）。
2. `VERSION=v1.2.3 ./build.sh` 后 `--version` 输出 `1.2.3`；不设置 `VERSION` 且仓库无 tag 时输出 `0.0.0`（程序内置逻辑）。
3. `file` 输出包含 `stripped`。
4. `STRIP=0 ./build.sh` 可跳过 strip 并正常构建。
5. `GOARCH=arm64 ./build.sh` 应直接报错退出，且不产生 arm 产物。

## 实测记录

- Go 1.25.0 / Ubuntu（GNU strip 2.38），`linux/amd64` 产物：
  - 仅 `-ldflags "-s -w"`：7,131,320 字节
  - 再执行 `strip --strip-all`：7,130,136 字节（再减 1,184 字节）
- 因为 `-s -w` 已移除 `.symtab`/DWARF，`file` 在 `STRIP=0` 时也会显示 `stripped`；额外 strip 的收益很小，但按要求保留。
- 两次构建 `sha256` 完全一致。

## 踩坑记录

### `go env GOARCH` 会读取同名环境变量，导致宿主架构判断失效

- 现象：执行 `GOARCH=arm64 ./build.sh` 时脚本没有拦截，反而真的交叉编译出 `dist/lazysvn-linux-arm64`，随后被宿主 `strip` 报 `Unable to recognise the format of the input file`，退出码 1。
- 根因：脚本用 `go env GOARCH` 求宿主架构，而 `go env` 会优先返回环境变量 `GOARCH` 的值，于是"宿主架构"和"目标架构"都变成 `arm64`，检查恒通过。
- 修复：目标平台改为硬编码 `linux/amd64`，宿主架构用 `go env GOHOSTOS` / `go env GOHOSTARCH` 获取（不受环境变量影响）；同时对 `GOOS`/`GOARCH` 覆盖值做白名单校验，非 `linux`/`amd64` 直接报错。
- 影响边界：仅影响脚本的架构校验逻辑，不影响 Go 构建本身。
- 防回归：`GOARCH=arm64 ./build.sh` 必须立即报错，且 `dist/` 下不出现 `lazysvn-linux-arm64`。
