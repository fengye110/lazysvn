# 顶部 Tab 栏（Changes / SVN 日志）

## 需求

去掉顶部标题 `LazySVN - SVN TUI`，替换为两个 tab：

- `变化(C)`：现有的变更列表 + diff 预览
- `SVN 日志(L)`：日志浏览模式（沿用 `docs/log-browser.md` 的两级布局）

交互：

1. `L` 自动切到 `SVN 日志` tab（等价于按 `L` 进入日志模式）。
2. `C` 切回 `Changes` tab。
3. 在列表顶部按 `↑` 把焦点移到 tab 栏；tab 栏获得焦点后 `←`/`→` 选择并切换 tab，`↓`/`Enter`/`Esc` 返回内容区。
4. 激活的 tab 用亮黄色；tab 栏聚焦时激活 tab 使用黄色背景，提示当前焦点在 tab 栏。

## 视图状态

- `mode` 决定当前 tab：`modeChanges` / `modeLog`。
- 新增 `tabFocus bool`：焦点是否在顶部 tab 栏。
- tab 栏为单行、无边框，替换原标题行；激活 tab 使用黄色背景 + 深色文字，非激活为灰色文字。

```
┌─────────┐ ┌─────────┐
│ 变化(C) │ │SVN 日志(L)│
└─────────┘ └─────────┘
┌ 内容区（按 mode 渲染）────────┐
└ footer                        ┘
```

## 按键

| 位置 | 按键 | 行为 |
| --- | --- | --- |
| 任意 | `L` / `Tab` | 切到 `SVN 日志`（日志内按 `Tab` 切回 `变化(C)`） |
| 任意 | `C` | 切到 `Changes`（已在该 tab 时无动作） |
| 列表顶部 | `↑` | 焦点移到 tab 栏（`k` 仍只上移光标） |
| tab 栏 | `←` / `h` | 选中 `Changes` 并切换 |
| tab 栏 | `→` / `l` | 选中 `SVN 日志` 并切换 |
| tab 栏 | `↓` / `Enter` / `Esc` | 焦点回到内容区 |
| tab 栏 | `q` / `Ctrl+C` | 退出程序 |

- `↑` 仅在光标已在列表第一项时进入 tab 栏，避免破坏列表内上下移动。
- tab 栏聚焦期间，内容面板边框不再显示黄色（焦点指示转移到 tab 栏）。

## 切换 tab 的实现

- `enterLogTabCmd()`：记录进入前的右侧内容（`prevTitle/prevBody`）→ `mode=modeLog` → 清空 diff → 焦点左侧 → 状态栏 `Loading log...` → `fetchLogCmd()`。
- `exitLogMode()`：恢复 `modeChanges` 与进入前的右侧内容（沿用现有实现）。
- `logListMsg` 成功时不再重复保存 `prevTitle/prevBody`（改由 `enterLogTabCmd` 负责），避免 tab 切换时被 Help 内容覆盖。
- 日志加载失败时回到 `modeChanges` 并恢复进入前内容。

## 渲染

- `renderTabs(width)`：每个 tab 使用 `NormalBorder` + 左右 padding，三个 tab 用 `JoinHorizontal` 并排。
  - 激活 tab：黄色边框/文字（`Color("11")`）+ 加粗；tab 栏聚焦时追加黄色背景 + 深色前景。
  - 非激活 tab：灰色边框/文字（`Color("8")`）。
  - 形如 `┌─────────┐ ┌─────────┐` / `│ 变化(C) │ │SVN 日志(L)│` / `└─────────┘ └─────────┘`。
- `View()` 用 `renderTabs` 替换原 header 行。
- 各面板聚焦判定加上 `!m.tabFocus`：
  - Changes 左列表：`!tabFocus`
  - 日志列表：`!tabFocus && logFocus == focusLeft`
  - Files（文件布局左侧）：`!tabFocus`
  - Diff / Preview：始终不聚焦

## 边界条件

- 工作区干净时 `Changes` tab 显示 `工作副本干净。`，右侧保持 Help。
- `SVN 日志` 加载中显示空 `Log (0)` 列表 + 状态栏 `Loading log...`。
- tab 栏聚焦时 `j`/`k` 不生效；`↑` 已在 tab 栏无动作。
- 日志的文件布局（Files 左 / Diff 右）内部按键不变；`↑` 在文件列表第一项同样可进入 tab 栏。
- `C` 在 `Changes` tab 无动作；`L` 在 `SVN 日志` tab 重新加载日志。
- 所有操作只读。

## 验证方式

在 `tmp/wc`（保留基准，不修改）中：

1. 启动：顶部显示 `Changes`（亮黄）/ `SVN 日志`（灰），无 `LazySVN - SVN TUI` 标题。
2. `L`：切到 `SVN 日志`，左 `SVN 日志 (6)` 列表，tab 高亮切换。
3. `C`：切回 `Changes`，右侧显示当前文件 diff。
4. Changes 列表第一项按 `↑`：tab 栏聚焦（激活 tab 黄底），`←`/`→` 切换 tab，`↓` 回到内容区。
5. 日志列表第一项按 `↑`：同样进入 tab 栏；`→` 仍在 `SVN 日志`，`←` 切到 `Changes`。
6. 在列表中第二项按 `↑`：正常上移光标，不进入 tab 栏。
7. 回归：Changes 与日志模式原有按键、布局交换、diff 切换均正常。

使用 `/tmp/opencode/drive_tui.py` 与 `/tmp/opencode/render_screen.py` 验证。

## 切换 tab 时保存位置

在两个 tab 之间切换时保留各自的上次位置，不重新加载：

- **变化(C)**：保留光标位置、标记集合、右侧 diff 内容。
- **SVN 日志(L)**：保留已加载的日志、选中的版本、布局（日志布局 / 文件布局）、选中的文件、右侧 diff、`+` 切换的 diff 方式。
- `Tab` 切换复用缓存；`L` 是显式重新加载，会把日志位置重置到最新一条（`logCursor=0`）。
- `Esc` 退出日志模式同样保留日志缓存，下次进入恢复上次位置。
- 日志加载失败时回到 Changes，并恢复进入前的右侧内容。
