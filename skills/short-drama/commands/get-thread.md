# get-thread：查询进展、问题与结果

```bash
pippit-tool-cli get-thread --thread-id THREAD_ID --run-id RUN_ID
```

`--thread-id` 必填；`--run-id` 可选，优先传本次提交返回的 run_id。省略时查看整个会话，注意区分旧 Run 的问题与当前问题。

CLI 固定使用服务端 v2，将 `readable_text` 的内容直接打印为 stdout **可读文本**，不是包含 readable_text 字段的 JSON，也不是旧版 messages 数组。例如：

```text
Thread: THREAD_ID
  标题: ...
  状态: ...
  -- Run #1 --
    [assistant] ...
```

格式以实际文本为准，不依赖固定缩进、标题或未声明的 JSON 字段。展示真实进展，遇到后端问题按 [创作流程](../workflows/creation.md) 提问并等待；输出没有明确终态时，不根据“本轮没有新消息”推断完成或仍在生成。

此命令读取已有 Run 的进展与回答，不会自行查询最新的角色、场景、画布或分镜内容。遇到业务内容问题，先按 [业务内容查询](../workflows/creation.md#业务内容查询) 在当前会话提交原始问题，再读取返回的新 run_id，不能把旧 Run 的总结当作本次查询结果。

此命令不代替文件发现和下载。退出码非零为调用失败，按 [轮询与文件交付](../workflows/poll-and-deliver.md) 有界恢复。保留现有 get-thread 读取业务对话，不用只面向媒体结果的查询代替它。

## 按 Run 查询积分消耗

使用已有的 Run ID 执行只读查询。

```bash
pippit-tool-cli get-run-credits --run-id RUN_ID
```

`credits.deducted` 是已结算的净实扣。确认零消耗返回 `0`，待结算或无匹配账本记录返回 `null`。可选的 `--thread-id` 用于核对会话。当前设备没有原任务的空间记录时，可传原空间的 `--team-id`。
