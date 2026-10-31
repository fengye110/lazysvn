# 提交输入框：占位提示、命令展示与错误换行

## 问题 1：输入框莫名出现“提”字

按 `c` 进入提交状态、还没输入任何内容时，footer 只显示一个反色的“提”字：

```
提交（1 个路径）：> 提
```

### 根因

`github.com/charmbracelet/bubbles@v0.21.0/textinput` 的 `placeholderView()`：

```go
p := make([]rune, m.Width+1)
copy(p, []rune(m.Placeholder))
...
if m.Width < 1 && len(p) <= 1 {
    return m.PromptStyle.Render(m.Prompt) + v
}
```

`Width` 未设置时默认为 0，`make([]rune, 1)` 只分配 1 个 rune，`copy` 只拷贝了 Placeholder 的第一个字符（“提”），随后 `len(p) <= 1` 直接返回。因此占位文案只显示第一个 rune。

此前的英文占位 `Commit message` 同样只显示 `C`，只是中文字符少、更容易被察觉。

### 修复

不使用 `textinput.Placeholder`：

- `initialModel()` 中移除 `ti.Placeholder = "提交信息"`，只保留 `Prompt = "> "` 与 `CharLimit = 200`。
- `renderFooter()` 在 `commitInput.Value() == ""` 时自行追加暗色提示：
  `提交信息（输入后回车提交，Esc 取消）`。

这样既不依赖 bubbles 的占位实现，也不会限制输入框宽度。

## 问题 2：提交信息没有引号

提交失败时预览面板显示：

```
$ svn commit -m test design rw permission design/test.txt
```

消息参数被空格拆开，看起来像多个参数，容易误导。

实际执行使用 `exec.Command` 的数组参数，不存在 shell 分词问题；问题只出在展示。修复：

- 新增 `shellQuoteArg()`：参数含空格或 shell 特殊字符时用 `strconv.Quote` 加双引号，普通路径保持原样。
- `formatSVNResult()` 拼接 `$ svn ...` 时逐参数调用 `shellQuoteArg()`。

展示效果：

```
$ svn commit -m "test design rw permission" design/test.txt
```

## 问题 3：svn 错误消息被截断

预览面板的普通文本分支此前对每行调用 `truncate()`，长错误（例如 `is not under version control ...`）会被裁成 `…`，看不到完整原因。

### 修复

`renderPreviewPane()` 的普通文本分支改为软换行：

```go
lines = append(lines, strings.Split(ansi.WrapWc(line, innerWidth, "/"), "\n")...)
```

- `ansi.WrapWc` 按显示宽度（中文/emoji 计 2 列）换行，保留 ANSI 转义。
- 断点包含空格与 `/`，长路径优先在目录分隔处换行，超长单词硬切。
- diff 视图仍使用 `truncate`（左右对比需要固定列宽），不受影响。

换行后行数可能超过面板高度，超出部分仍被裁剪；这是面板高度的固有限制。

## 验证

```sh
# 用包装的 svn 拦截 commit，输出长错误，不真正提交
cat > /tmp/opencode/fakebin/svn <<'EOF'
#!/bin/sh
case "$*" in
  *commit*)
    printf "%s\n" \
      "svn: E200009: Commit failed (details follow):" \
      "svn: E200009: '/proj/N30/wa/yaoj/n30-svn/design/test.txt' is not under version control ..." >&2
    exit 1
    ;;
esac
exec /usr/bin/svn "$@"
EOF
```

`PATH=/tmp/opencode/fakebin:$PATH python3 drive_tui.py ./lazysvn 'cfix bug\r'`

预期：

```
提交
$ svn commit -m "fix bug" README.md

svn: E200009: Commit failed (details follow):
svn: E200009: '/proj/N30/wa/yaoj/n30-svn/design/test.txt' is not under
version control and is not part of the commit, yet its child '/proj/
N30/wa/yaoj/n30-svn/design/test.txt/child' is part of the commit

error: exit status 1
```

- 不再出现孤立的“提”字；空输入时显示完整占位提示。
- 单元测试 `TestShellQuoteArg`、`TestFormatSVNResultQuotesMessage` 覆盖引号逻辑。
- 验证提交使用包装 svn，不修改 `tmp/wc`（见 `docs/svn-demo.md` 保留约定）。

## 待提交文件列表（右侧预览）

按 `c` 进入提交状态时，右侧预览面板显示本次将提交的文件，避免只看到 footer 的路径数量：

```
待提交文件（2 个）：

  M  README.md
  M  src/main.c
```

- 文件条目来自 `commitTargets()`（有标记用标记集合，否则当前文件）。
- 每行前缀为该文件在 `svn status` 中的状态码（`M`/`A`/`D`/`?` 等），便于发现未 `add` 的条目。
- 未选择具体文件（`commitTargets()` 返回 `.`）时显示
  `未选择具体文件，将提交当前工作副本。`
- 输入提交信息期间列表保持显示；`Esc` 取消后恢复进入提交前的预览内容
  （`commitPrevTitle` / `commitPrevBody` 快照）。
- 提交执行后预览被 `actionMsg`（命令与输出）覆盖。

### 验证

1. 标记两个文件按 `c`：右侧标题 `待提交文件`，列表显示两个路径及状态码。
2. 输入文字：列表不变，footer 显示输入内容。
3. `Esc`：恢复到按 `c` 之前的预览（如当前文件 diff）。
4. 回车提交：预览切换为 `$ svn commit -m "..."` 与执行结果。
