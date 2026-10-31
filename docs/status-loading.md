# 变更列表的加载状态

## 问题

工作副本较大时 `svn status` 可能需要几十秒。此前 `renderChangesPane` 的判定是"`entries` 为空就显示 `工作副本干净。`"，而启动瞬间 `entries` 必然是空的，于是：

- `svn status` 还在执行时，左侧已经显示"工作副本干净。"，误导用户以为没有变更；
- 若 `svn status` 因需要认证而阻塞（等待输入），界面会长时间停在这个错误状态。

此外 `exec.Command` 继承 TUI 的 stdin，认证交互可能挂死。

## 修复

1. model 增加 `statusLoading bool` / `statusErr bool`：
   - 初始 `statusLoading = true`。
   - `statusMsg` 到达：`statusLoading = false`；出错时 `statusErr = true`，成功时 `statusErr = false`。
   - `r` 刷新、提交/还原/更新/添加后的自动刷新：`statusLoading = true`、`statusErr = false`。
2. `renderChangesPane` 的空列表文案：
   - 加载中：`正在加载变更...`
   - 失败：`svn status 执行失败，按 r 重试。`
   - 否则：`工作副本干净。`
3. `runSVN` 统一加 `--non-interactive`，避免 svn 在 TUI 中等待认证输入而卡住；需要认证时直接返回错误，由预览面板显示。
4. 状态栏仍显示 `正在加载 SVN 状态...`，与左侧文案一致。

## 边界

- `svn status` 耗时本身不改变（大工作副本/网络文件系统），但界面会明确显示加载中。
- `--non-interactive` 对 `status/log/diff/update/commit/add/revert` 全部生效；未缓存凭证的操作会直接失败，而不是挂起。
- 加载期间按 `r` 会再次触发 status 查询（不做并发限制）。

## 验证

使用一个延迟包装的 svn 模拟大仓库：

```sh
# /tmp/opencode/fakebin/svn
#!/bin/sh
sleep 3
exec /usr/bin/svn "$@"
```

以该 PATH 启动 lazysvn：

1. 启动后 1-2 秒内，Changes 面板显示 `正在加载变更...`，而不是 `工作副本干净。`。
2. 3 秒后 `svn status` 返回，列表显示实际条目（`M`/`?` 等）。
3. 每次操作后自动刷新期间同样显示加载中。
4. `svn status` 失败时（如非工作副本），左侧显示失败提示，右侧预览显示错误详情。

测试不影响 `tmp/wc` 的数据。
