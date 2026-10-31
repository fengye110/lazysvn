# 操作文件列表（commit / revert / delete）

## 需求

所有带文件目标的 svn 操作都要显示具体文件，而不是只显示“多少个路径”：

- footer 确认/输入提示中显示文件名；
- 右侧预览窗口显示本次操作的文件列表（含 `svn status` 状态码）。

适用于：提交（`c`）、还原（`v`）、删除（`D`）。`update` 针对整个工作副本，无文件列表。

## 交互

### 提交 `c`

footer：

```
提交 README.md：> ▮
提交 2 个文件：README.md、src/main.c：> ▮
```

右侧：

```
待提交文件（2 个）：

  M  README.md
  M  src/main.c
```

未选择具体文件时右侧显示 `未选择具体文件，将提交当前工作副本。`

### 还原 `v`

footer：

```
还原 README.md（本地修改将丢失）？按 y 确认，n 或 Esc 取消。
还原 2 个文件：README.md、src/main.c？按 y 确认，n 或 Esc 取消。
```

右侧：

```
待还原文件（2 个）：

  M  README.md
  M  src/main.c
```

### 删除 `D`

footer（已有）：

```
删除文件 README.md（本地修改将丢失）？此操作不可撤销，按 y 确认，n 或 Esc 取消。
```

右侧新增：

```
待删除文件（2 个）：

  M  README.md
  ?  notes.txt
```

## 实现

- 抽出 `statusPathList(paths)`：按 `entries` 匹配状态码，生成 `  M  path` 形式的行。
- `commitPreview` / `revertPreview` / `deletePreview` 复用该函数，标题分别为
  `待提交文件` / `待还原文件` / `待删除文件`。
- model 增加 `commitPrevTitle/Body`（已有）、`revertPrevTitle/Body`、`deletePrevTitle/Body`：
  进入确认/输入状态时保存原预览，`Esc` / `n` 取消后恢复，避免列表残留。
- 确认/输入提示统一用文件名列表，含本地修改时追加 `（本地修改将丢失）`。
- 列表与提示均按显示宽度截断（`…`），避免超出面板/footer。

## 验证

使用独立 checkout 副本（不修改 `tmp/wc`）：

1. `v`：footer 显示文件名，右侧显示 `待还原文件` 列表；`Esc` 恢复原 diff。
2. `c`：footer 显示文件名，右侧显示 `待提交文件` 列表；输入时保持。
3. 标记两个文件后 `D`：footer 显示两个名字，右侧显示 `待删除文件` 列表；`n` 恢复原预览。
4. 提交/还原执行后预览切换为命令与输出（`actionMsg`）。
5. `gofmt` / `go vet` / `go test ./...` 通过，`./build.sh` 重建。
