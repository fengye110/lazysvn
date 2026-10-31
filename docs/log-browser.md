# 日志浏览模式（`L`）交互设计

## 需求

按 `L` 进入"当前目录最近日志"浏览模式：

1. **左侧**显示 `svn log -v -l 20 .` 的 20 个 entry，**右侧**显示当前版本的文件列表。
2. 左侧用 `j`/`k`（及上下方向键）在版本之间导航，右侧文件列表同步刷新。
3. 按 `→` 或 `Enter` 进入文件列表：**布局交换**——文件列表移到左侧并获得焦点，文件 diff 显示在右侧。
4. 在文件列表中用 `j`/`k` 导航，右侧 diff 自动更新（无需回车）。
5. `+` 在左右对比与统一格式之间切换 diff 显示。
6. `←` 或 `Esc` 返回日志列表布局（Log 在左、Files 在右，焦点回到左侧）。
7. 在日志列表布局再按 `Esc` 退出日志模式回到 Changes。

## 交互状态机

```
Changes（默认）
   │ 按 L
   ▼
日志布局：左 Log 列表（焦点）＋ 右 Files 列表
   │ → / Enter / Tab            │ j/k
   ▼                            ▼
文件布局：左 Files（焦点）＋ 右 Diff    右侧文件列表刷新为新版本
   │ j/k → 右侧 Diff 自动更新
   │ +   → 切换左右对比 / 统一格式
   │ ← / Esc
   ▼
日志布局（焦点回到 Log 列表）
   │ Esc
   ▼
Changes
```

约定：

- 进入日志模式时保存进入前的右侧面板内容；退出日志模式时恢复。
- 日志布局与文件布局是同一对面板的两种排列，不额外打开窗口。
- `Esc`/`←`：文件布局 → 日志布局（焦点切回 Log 列表）；日志布局 → 退出到 Changes。
- 进入日志模式时 `logCursor=0`，右侧展示该版本的文件列表；`j`/`k` 切换版本时同步刷新（数据由 `svn log -v` 一次性获取）。
- `→` / `Enter` / `Tab` 进入文件布局时立即加载当前文件的 diff，无需再按 `j`。
- 没有文件列表时 `→` / `Enter` / `Tab` 不生效（停在本版本）。
- `q` 仍然直接退出程序。

## 数据获取

| 操作 | 命令 |
| --- | --- |
| 加载日志 | `svn log --xml -v -l 20 .` |
| 单文件 diff | `svn diff -c <rev> -x -U6 ^<repo-path>` |

- 使用 XML 输出（`--xml`）而不是文本解析，避免提交信息、作者名中出现分隔线或本地化日期导致解析错误。
- `^/path` 是相对仓库根的 caret 语法（`svn diff` 支持），无需拼接工作副本绝对路径，且对已删除文件也能显示 diff。
- 日期从 RFC3339 转换到本地时区显示为 `YYYY-MM-DD HH:MM`。

## 数据结构（main.go）

```go
type changedPath struct {
	Action string // M / A / D / R
	Kind   string // file / dir
	Path   string // 仓库路径，如 /src/util.c
}

type logEntry struct {
	Revision int64
	Author   string
	Date     time.Time
	Message  string
	Paths    []changedPath
}

type uiMode int // modeChanges / modeLog
type uiFocus int // focusLeft / focusRight
```

model 相关字段：

```go
mode         uiMode
logEntries   []logEntry
logCursor    int
logFocus     uiFocus    // focusLeft=日志布局, focusRight=文件布局
logPaths     []changedPath
logPathsRev  int64
pathCursor   int
logDiff      string     // 当前预览的文件 diff
logDiffTitle string
prevTitle    string     // 进入日志模式前的右侧内容，用于退出恢复
prevBody     string
diffSplit    bool       // true=左右对比, false=统一格式
```

`focusLeft` 表示日志布局（Log 在左、Files 在右），`focusRight` 表示文件布局（Files 在左、Diff 在右）。

## 渲染

- Changes 模式左侧标题行：有变更时居中显示暗色提示 `回车或者空格选中`，右侧对齐显示 `选中待操作文件后操作`。
- 日志布局（`logFocus == focusLeft`）：
  - 左：日志列表，标题 `SVN 日志 (n) [焦点]`，中间提示 `→ 右键切换到右侧`，右侧对齐 `Esc 退出日志模式`。
  - 右：文件列表，标题 `文件: rN`，中间提示 `→ 进入文件列表`，右侧 `回车查看文件差异`。
- 文件布局（`logFocus == focusRight`）：
  - 左：文件列表（聚焦），标题 `文件: rN [焦点]`，提示 `← 返回日志`、`↑↓ 预览`、`回车查看文件差异`。
  - 右：diff，标题 `差异: rN /path`，中间 `+ 切换差异方式`，右侧 `Esc 返回日志列表`。
- 面板内容宽度 = `contentWidth - 2`：lipgloss 的 `Width()` 包含左右 padding（各 1 列），按 `contentWidth` 截断会触发 word wrap 折行、打乱边框。
- 所有行使用 `ansi.Truncate`（按终端显示宽度，中文/emoji 计 2 列，ANSI 转义安全）。
- 焦点面板边框为黄色（lipgloss `Color("11")`），标题带 `[焦点]`。
- 文件列表与日志列表均按面板高度滚动；diff 正文按面板高度截断。

## 按键汇总

| 按键 | 日志布局（左 Log 焦点） | 文件布局（左 Files 焦点） |
| --- | --- | --- |
| `j` / `↓` | 下一条日志（右侧文件列表刷新） | 下一个文件，右侧 diff 自动更新 |
| `k` / `↑` | 上一条日志（右侧文件列表刷新） | 上一个文件，右侧 diff 自动更新 |
| `g` / `G` | 首/末条日志 | 首/末个文件 |
| `Enter` | 进入文件布局（加载当前文件 diff） | 重新加载当前文件 diff |
| `→` | 进入文件布局 | 无动作 |
| `Tab` | 进入文件布局 | 返回日志布局 |
| `←` | 无动作 | 返回日志布局 |
| `Esc` | 退出日志模式 | 返回日志布局 |
| `+` | 切换 diff 显示方式（作用于下次/当前 diff） | 切换 diff 显示方式（立即生效） |
| `q` / `Ctrl+C` | 退出程序 | 退出程序 |

## 边界条件

- 空仓库 / 无日志：左侧显示 `（无日志）`，`Enter`/`→` 无动作。
- 某版本没有 Changed paths：右侧显示 `（无变更文件）`。
- 提交信息含多行：日志列表只显示首行。
- `svn log` 失败（当前目录不是工作副本）：保持 Changes 模式，错误正文显示在右侧预览，状态栏提示失败。
- diff 输出为空（如仅属性修改）：正文显示 `（无差异输出）`。
- 从日志布局切换到文件布局时，diff 尚未返回期间右侧显示 `正在加载差异...`。
- 所有操作均为只读，不修改工作副本。

## 文档与帮助同步

- TUI 帮助面板、README Keybindings 更新 `L` 及日志模式下的 `Tab` / `←` / `→` / `Enter` / `Esc`。
- 实现与本文档如有偏差，同步更新本文档。

## 验证方式

在 `tmp/wc`（r6 + 本地修改，保留基准）中构造按键序列：

1. `L`：左 `SVN 日志 (6) [焦点]`，右 `文件: r6`（无 `[焦点]`）。
2. `j` `j`：选中 r4，右侧刷新为 `文件: r4`。
3. `Enter` 或 `→`：布局交换——左 `文件: r4 [焦点]`，右 `差异: r4 /Makefile`（首个文件）。
4. `j`：左侧选中 `/tests/`，右侧自动变为 `差异: r4 /tests`。
5. `+`：右侧 diff 切换为统一格式；再按恢复左右对比。
6. `←` 或 `Esc`：恢复日志布局——左 `SVN 日志 (6) [焦点]`，右 `文件: r4`。
7. `Esc`：退出日志模式，回到 Changes（右侧显示当前文件 diff / Help）。
8. `G` / `k`：跳到 r1、r6 时 `k` 不越界。
9. 工作区干净时按 `L` 仍可用；干净状态测试使用独立 checkout 副本（如 `/tmp/opencode/wc-clean`），**不要** revert 或删除 `tmp/wc`（见 `docs/svn-demo.md` 保留约定）。
10. 回归：`l`（选中文件日志）、`d`（diff）与 Changes 模式其它按键行为不变。

使用伪终端驱动脚本（`/tmp/opencode/drive_tui.py`，支持 `\r`、`\t`、`\e`）抓取渲染结果，用 `/tmp/opencode/render_screen.py` 重放最终画面断言标题与内容。

## 实测结果（Go 1.25 / svn 1.14.1 / tmp/wc）

- 终端 140x40，左 `SVN 日志 (6)` 每版本一行，中文消息按显示宽度截断，无折行错位。
- `Enter`/`→` 后：左 `文件: r4 [焦点]`、右 `差异: r4 /Makefile`；`j` 后右 `差异: r4 /tests`。
- `+` 后右侧 diff 变为统一格式（`@@` / `+` 行），再按恢复左右对比。
- `←` 或 `Esc` 恢复日志布局；再 `Esc` 退出到 Changes。
- 文件面板提示在左侧时显示 `← 返回日志  ↑↓ 预览  回车查看文件差异`，在右侧时显示 `→ 进入文件列表  回车查看文件差异`。
- 工作区干净副本中 `L` 仍可用；测试过程不修改 `tmp/wc`。

## 踩坑记录

### lipgloss `Width()` 包含 padding，按 contentWidth 截断会触发换行

- 现象：日志列表里中文提交信息被折到下一行，左侧边框内容错位，甚至出现半截消息行。
- 根因：`paneStyle()` 设置了 `Padding(0,1)`，lipgloss 的 `Width(contentWidth)` 把左右 padding 计入宽度，实际文本区只有 `contentWidth-2` 列。之前按 `contentWidth` 截断的行宽超出 2 列，lipgloss 用 wordwrap 折行；中文消息没有空格，整段被视为一个长 word，于是整体跳到下一行。
- 修复：新增 `paneInnerWidth(contentWidth) = max(8, contentWidth-2)`，所有面板统一使用；同时把 `truncate` 从按 rune 数改为 `ansi.Truncate`（按显示宽度、ANSI 安全）。
- 影响边界：所有左右面板（Changes / Preview / Log / Files / Diff），长路径和多字节文件名同样受益。
- 防回归：终端 140 列时，`Log` 列表每条记录必须完整显示在一行内，不出现无 `>` 前缀的折行内容。

### 标题提示过多会挤掉标题

- 现象：文件布局下 `差异: r4 /tests/test_util.c` 加上 `+ 切换差异方式`、`Esc 返回日志列表` 后，中间提示被省略。
- 修复：`renderPaneTitleMid` 在空间不足时优先截断标题（保留提示）；`Files` 左侧提示精简为 `← 返回日志`。
- 防回归：文件布局左右标题行必须同时可见两段提示。

### `svn log` 在刚 checkout 且从未 update 的工作副本根目录为空

- 见 `docs/svn-demo.md`：checkout 后根目录 BASE=0，`svn log` 以 r0 为 peg 查询不到历史，演示脚本已在提交后执行 `svn update`。
