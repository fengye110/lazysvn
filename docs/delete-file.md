# 删除文件（文件系统删除）

## 需求

- 新增 `D` 键：删除标记文件（无标记时删除当前文件）。
- 删除的是**文件系统中的文件**（`os.Remove`），不是 `svn delete`。
- 执行前必须二次确认，并在提示中显示待删除文件的名称。
- 暂不支持删除目录。

## 为什么不用 svn delete

`变化` 列表里的条目大多是带本地修改的文件（`M`）或未版本化文件（`?`）：

- `svn delete` 对 `M` 文件默认以 `E195006 ... has local modifications` 拒绝；
- `svn delete` 对 `?` 文件报 `is not under version control`。

用 `--force` 虽然能删 `M`，但对 `?` 仍然无效，且语义上是登记删除而非删除磁盘文件。因此改为直接 `os.Remove`：

- `?` 文件：从磁盘删除，下次 `svn status` 后从列表消失；
- `M` / 版本控制文件：从磁盘删除，`svn status` 显示为 `!`（missing），可用 `v`（revert）恢复原本的版本内容；
- 未版本化文件删除后无法恢复（无 svn 备份）。

## 交互

1. `D`
   - 目标来自 `selectedTargets()`：有标记时取全部标记，否则取光标所在文件。
   - 无目标：状态栏提示 `未选择文件。`，不进入确认。
   - 目标中有目录（`os.Stat` 判断 `IsDir()`）：不进入确认，状态栏提示
     `暂不支持删除目录：<path>`。
2. 确认时右侧预览显示 `待删除文件` 列表（含状态码），footer（红色）同时显示文件名：

   ```
   删除文件 README.md？此操作不可撤销，按 y 确认，n 或 Esc 取消。
   删除 2 个文件（本地修改将丢失）：README.md、notes.txt？此操作不可撤销，按 y 确认，n 或 Esc 取消。
   ```

   - 单个文件直接显示完整路径；多个文件显示数量 + 名称列表。
   - 目标中有本地修改（status 为 `M`）时追加 `（本地修改将丢失）`。
   - 名称列表超宽时按显示宽度截断（`…`）。
3. `y`：状态栏 `正在删除文件...`，逐个执行 `os.Remove`。
4. `n` / `Esc`：取消，状态栏 `已取消删除。`。
5. 完成后预览面板显示：

   ```
   $ rm README.md notes.txt

   已删除：README.md
   已删除：notes.txt
   ```

   删除失败时显示 `删除失败：<path>` 与错误信息，状态栏 `操作失败。`。
6. 自动刷新 `svn status`：`?` 条目消失；版本控制文件变为 `!`。

## 目录限制

`os.Remove` 只能删除空目录，且本版本不做目录删除：

- 以 `os.Stat(path).IsDir()` 判断，命中即拒绝整次操作，不做部分删除。
- 目标不存在时不视为目录，交给 `os.Remove`（会返回 not exist 错误并展示）。

## 与其它操作的互斥

- `deleteConfirm` 期间只接受 `y` / `n` / `Esc`，其余按键被忽略。
- 与 `commitPrompt`、`revertConfirm` 同为模态状态，按优先级串行判断（commit → delete → revert）。

## 实现点

- model 增加 `deleteConfirm bool`、`deletePaths []string`。
- `deleteFilesCmd(paths)`：逐个 `os.Remove`，汇总成功/失败，返回
  `actionMsg{title: "删除文件", refresh: true, failed: ...}`。
- `actionMsg` 增加 `failed bool`，失败时状态栏显示 `操作失败。`
- `renderFooter` 增加删除确认分支，显示文件名与不可撤销提醒。
- 帮助面板、footer 帮助行、README 键位表补充 `D 删除`。

## 验证

不修改演示环境 `tmp/wc`（见 `docs/svn-demo.md`），使用独立 checkout：

```sh
svn checkout file:///root/yaoj/it/lazysvn/tmp/repo /tmp/opencode/wc-os
cd /tmp/opencode/wc-os
echo x >> README.md   # M
touch notes.txt       # ?
mkdir newdir          # 目录
svn status
```

1. 选中 `README.md` 按 `D`：footer 显示
   `删除文件 README.md（本地修改将丢失）？此操作不可撤销...`。
2. `n`：取消，文件仍在。
3. 再次 `D` → `y`：文件从磁盘删除，列表变为 `[!] README.md`。
4. 选中 `notes.txt`（`?`）按 `D` → `y`：文件删除，刷新后条目从列表消失。
5. 选中 `newdir` 按 `D`：状态栏 `暂不支持删除目录：newdir`，不进入确认。
6. 标记两个文件后 `D`：提示显示数量与两个文件名；`y` 后两者均被删除。
7. 单元测试使用 `go test ./...`；删除流程通过独立副本验证。
