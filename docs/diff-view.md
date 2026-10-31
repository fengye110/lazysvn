# Diff 显示方式切换（`+`）

## 需求

在 Changes 模式的 diff 预览中，按 `+` 在两种 diff 展示方式之间切换：

- **左右对比（side-by-side）**：默认方式，旧 / 新 两栏，中间 `|` 分隔
- **统一格式（unified）**：传统 `+` / `-` 前缀的整行 diff

Diff 标题行右侧显示暗色提示 `+ 切换差异方式`。

## 实现

1. model 增加 `diffSplit bool`，`initialModel()` 中置为 `true`（默认左右对比）。
2. `renderPreviewPane` 中当 `previewTitle` 以 `差异:` 开头时：
   - `diffSplit == true` → `renderSideBySideDiff(body, innerWidth)`
   - `diffSplit == false` → `renderUnifiedDiff(body, innerWidth)`
3. 新增 `renderUnifiedDiff`：
   - 与 side-by-side 一致，剥离 `$ svn diff ...` 命令行与紧随的空行
   - `\t` 展开为 4 个空格，保证截断宽度与终端显示一致
   - 逐行 `colorizeDiffLine(truncate(line, width))`，复用现有配色
4. Changes 模式按键新增：

   ```go
   case "+":
       if strings.HasPrefix(m.previewTitle, "Diff:") {
           m.diffSplit = !m.diffSplit
           // 状态栏提示当前模式
       }
   ```

   非 diff 预览（Help / Log / Update 输出等）时 `+` 无动作。
5. 标题行使用 `renderPaneTitle(previewTitle, "+ 切换差异方式", innerWidth)`，放不下时自动省略提示。
6. 切换只影响渲染，不重新执行 `svn diff`；`diffSplit` 状态在本次运行内保持，后续打开新 diff 沿用上次选择。

## 边界条件

- 日志浏览模式（`L`）左侧的 diff 是纯文本统一输出，本次不支持切换（`+` 仅在 Changes 模式的 diff 预览生效）。
- diff 内容没有 `@@` hunk 时，side-by-side 本身会回退为统一格式；此时切换视觉差异不明显，但按键行为不变。
- 切换不改变工作副本，也不重新查询 SVN。

## 验证方式

在 `tmp/wc` 中（保留基准，不修改）：

1. 选中 `README.md` 按 `d`：默认左右对比，标题行显示 `差异: README.md` + `+ 切换差异方式`。
2. 按 `+`：切换为 `+` / `-` 整行格式，标题提示不变。
3. 再按 `+`：恢复左右对比。
4. 按 `h` 显示帮助后按 `+`：无动作，不报错。
5. 打开另一个文件 diff（如 `src/main.c`）：沿用上次选择的显示方式。
6. 回归：左右对比模式下中文行、制表符行的中间分隔线仍然对齐（`padRight` 按显示宽度、tab 展开）。

使用 `/tmp/opencode/drive_tui.py` 驱动按键，`/tmp/opencode/render_screen.py` 重放最终画面。

## 光标移动自动预览

Changes 模式下用 `j`/`k`（及上下方向键、`g`/`G`、Home/End）移动光标时，右侧预览会自动切换为当前选中文件的 `svn diff`，无需先按 `d`/`Enter`。

- 启动时若 Changes 有文件，右侧直接显示第一个文件的 diff（不再显示 Help）；工作区干净时仍显示 Help
- 未版本化文件（`?`）也会执行 `svn diff`，若失败则在预览中显示错误信息
- 日志浏览模式（`L`）不受影响
- 手动 `d`/`Enter` 行为不变

验证：启动后按 `j`，右侧标题应从 `帮助` 变为 `差异: src/main.c`；再按 `j` 变为 `差异: src/pending.c`（未版本化，通常显示错误或无输出）；按 `k` 回到 `README.md`。

## 上下文行数

所有 diff（Changes 预览与日志浏览模式）默认使用 `-x -U6`，即每个 hunk 展示改动前后各 6 行原文：

- `svn diff -x -U6 <path>`
- `svn diff -c <rev> -x -U6 ^<repo-path>`

行数由 `main.go` 中的常量 `diffContextLines = 6` 控制，修改常量即可调整。文件本身不足 6 行时按实际行数显示。

## 日志浏览模式

日志浏览（`L`）中同样支持：按 `→`/`Enter` 进入文件布局后（Files 在左、Diff 在右），用 `j`/`k` 移动文件时右侧自动显示该文件在对应 revision 的 diff；按 `+` 可在左右对比与统一格式之间切换，`diffSplit` 与 Changes 模式共用。

- diff 显示在右侧，标题行显示 `+ 切换差异方式` 与 `Esc 返回日志列表`
- 空间不足时优先截断标题（如 `差异: r4 /tests/t…`），保证两段提示可见
- `←` 或 `Esc` 返回日志布局（Log 在左、Files 在右）；再次进入文件布局会重新显示 diff

## 颜色

日志浏览模式与 Changes 模式的 diff 共用同一套渲染函数（`renderSideBySideDiff` / `renderUnifiedDiff`），配色一致：

- `Index:` / `---` / `+++`：蓝色（`1;94`）
- `@@` hunk 头：紫色（`1;95`）
- `旧` / `新` 表头：青色（`1;96`）
- `+` 行：绿色（`92`），`-` 行：红色（`91`）
- 上下文行：灰色（`90`）
