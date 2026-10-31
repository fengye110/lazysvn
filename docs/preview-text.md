# 预览面板文本换行

## 背景

预览面板（右侧）普通文本从 `truncate`（截断加 `…`）改为 `ansi.WrapWc` 软换行，以便完整显示 svn 错误信息中的长路径。

## 问题：分隔线被折成两行

`svn log` 输出的每条记录之间是 72 个 `-` 组成的分隔线：

```
------------------------------------------------------------------------
r1 | alice | 2026-10-30 18:03:46 +0800 (五, 30 10月 2026) | 1 line
```

右侧面板内宽通常小于 72 列，软换行会把分隔线拆成两行（第二行只剩几个 `-`），视觉上多出空行。

## 修复

在 `renderPreviewPane` 的普通文本分支中识别“分隔线行”，对它只做截断、不换行：

```go
if isRuleLine(line) {
    if len(line) > innerWidth {
        line = line[:innerWidth]
    }
    lines = append(lines, line)
    continue
}
lines = append(lines, strings.Split(ansi.WrapWc(line, innerWidth, "/"), "\n")...)
```

`isRuleLine`：

- 去除首尾空白后长度 ≥ 3；
- 所有字符都是 `-` 或 `=`。

这类行只有分隔语义，截断到面板宽度即可；分隔线是 ASCII，直接按字节截断与显示宽度一致。

其它文本仍走 `ansi.WrapWc`（按空格与 `/` 断行、超长词硬切、保留 ANSI）。

## 验证

1. 任意文件按 `l`，右侧 `SVN 日志:` 预览中每条记录的分隔线只占一行，不再出现第二行残线。
2. 长错误消息（如 `is not under version control ...`）仍能完整换行显示。
3. 帮助、diff 视图不受影响。
