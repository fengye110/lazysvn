# SVN 演示环境脚本（create_svn_proj.sh）

## 目的

`create_svn_proj.sh` 用于快速生成一套可抛弃的 SVN 演示环境，供 LazySVN 本地调试使用：一个 `file://` 协议仓库 + 一个已有多条提交记录的工作副本，并预置未提交变更。

生成内容与手工验证过的 `tmp/` 完全一致。

## 脚本行为

1. 以脚本所在目录为基准，默认在 `<脚本目录>/tmp` 下创建：

   ```
   tmp/
   ├── repo/   # svnadmin create 出来的仓库
   └── wc/     # svn checkout 出来的工作副本
   ```

   可通过 `DEMO_DIR=/some/path` 覆盖目标目录。

2. 每次执行都会 `rm -rf` 目标目录后重建，保证结果可重复（幂等）。
3. 修改 `repo/conf/svnserve.conf`，把 `anon-access` 置为 `write`，使 `file://` 提交无需密码。
4. 依次产出 6 次提交（作者 alice / bob / carol，中文提交信息）：

   | rev | 内容 |
   | --- | --- |
   | r1 | 初始化项目骨架：Makefile、main.c 与 util 版本接口 |
   | r2 | 支持 `--version` 与 `--verbose`，新增忽略大小写比较函数 |
   | r3 | 补充使用文档，README 增加入口 |
   | r4 | 新增 util 单元测试，Makefile 增加 test 目标 |
   | r5 | 添加链接 util 的示例程序 |
   | r6 | 修复空指针处理，版本号升至 0.3.0 |

5. 提交完成后执行一次 `svn update`，让工作副本根目录从 r0 统一到 r6；否则在 `wc` 根执行 `svn log` 只能看到 r0 之前的历史（为空）。
6. 制造 3 个未提交变更，便于观察 `svn status`：

   ```
   M  README.md
   M  src/main.c
   ?  src/pending.c
   ```

## 边界条件

- 依赖 `svn` 与 `svnadmin`（实测 1.14.1）；缺失时脚本报错退出。
- 会清空目标目录，`DEMO_DIR` 不要指向有真实数据的路径。
- `tmp/` 已加入 `.gitignore`，演示数据不会被 git 跟踪。
- 脚本只负责生成演示数据，不启动 LazySVN。

## 保留约定（重要）

`tmp/wc` 是用户保留的测试基准（当前 r6 + 3 个未提交变更），**测试过程中不要**：

- 执行 `svn revert` / 删除 `src/pending.c` 等方式清理工作副本；
- 重跑 `create_svn_proj.sh` 覆盖 `tmp/`；
- 用其它命令改动 `tmp/repo` 或 `tmp/wc` 的历史与文件。

需要干净的工作副本做测试时，另建独立副本：

```sh
svn checkout file:///root/yaoj/it/lazysvn/tmp/repo /tmp/opencode/wc-clean
# 测试完只删除这个临时副本
rm -rf /tmp/opencode/wc-clean
```

`create_svn_proj.sh` 每次执行都会 `rm -rf` 目标目录，只在明确需要重置演示环境时才运行。

## 使用与验证

```sh
./create_svn_proj.sh
cd tmp/wc
svn info --show-item revision   # 6
svn log -q                      # r6..r1 共 6 条
svn status                      # M/M/?
/root/yaoj/it/lazysvn/dist/lazysvn-linux-amd64   # 在该目录内启动 TUI
```
