# query-result：查询异步结果并下载

用于生成和视频处理命令返回的任务，也用于用户要求查询的已有任务。三个参数均必填：

| 参数 | 含义 |
| --- | --- |
| `--thread-id` | 原任务的 `thread_id` |
| `--run-id` | 要查询的那一次运行的 `run_id`，不得混用其他运行 |
| `--download-dir` | 本地输出目录，命令在任务成功时自动下载产物 |

用户未指定目录时使用 `./xyq_output`。已知历史任务但缺少任一 ID 时，从当前上下文获取，无法确定则询问；不要通过重新生成来补 ID。

```bash
pippit-tool-cli query-result --thread-id THREAD_ID --run-id RUN_ID --download-dir "./xyq_output"
```

## 输出契约

stdout 是 JSON；命令可能将错误编码进 JSON 并以退出码 0 返回，因此必须先检查 `error_message`。

| 字段 | 含义 |
| --- | --- |
| `status` | 目标 Run 状态：`running`、`succeeded`、`failed`、`canceled`、`input_required`、`interrupted`、`unknown`；不直接由查询接口 `ret` 决定 |
| `completed` | 是否结束；失败时也可能为 `true`，不等于成功 |
| `error_message` | 非空即错误，不能因为 `completed=false` 而忽略 |
| `thread_id` / `run_id` | 对应的查询任务 |
| `images[]` / `videos[]` | 成功后获取的媒体，每项含 `download_url`、`output_path` |
| `videos[].draft` | 服务端明确返回时才输出；`true` 为样片，`false` 为成片，未返回时省略 |
| `videos[].draft_task_id` | 原始方舟样片任务 ID，用于样片转成片；有值时才输出 |

成功示例（ID、URL 和文件名仅为示意）：

```json
{
  "status": "succeeded",
  "completed": true,
  "thread_id": "THREAD_ID",
  "run_id": "RUN_ID",
  "error_message": "",
  "images": [{"download_url": "https://example.com/image.jpeg", "output_path": "./xyq_output/asset.jpeg"}],
  "videos": []
}
```

`status` 和 `completed` 优先按返回的目标 Run 判定，适用于任意 `ret`。失败原因优先读取 Run 的 `fail_reason.message/code`，再读取兼容错误字段和接口错误；非零接口返回仍保留本地诊断日志。

没有目标 Run 时，只有已核实的接口返回可确定终态：`ret=5` 且 `errmsg` 为 `创作失败` 或以 `创作失败：` 开头时返回 `failed`、`completed=true`；`ret=5` 且 `errmsg` 为 `创作已取消` 时返回 `canceled`、`completed=true`。不要泛匹配“失败”或“不合规”。其余查询错误保持 `unknown`、`completed=false`，通过 `error_message` 报告原因及可用的 LogID。`ret=2` 是参数错误，不代表安审失败；`ret=5` 也可能只是查询会话失败。未指定或未知 Run 状态保持 `unknown`。

`input_required` 表示等待补充输入，`completed=false`；`interrupted` 表示人工交互中断结束，`completed=true`。两者均停止自动轮询并报告交互需求或中断状态。`canceled` 也直接停止，不因空产物误报下载失败。程序中断／等待工具回调仍为 `running`。

Run 已成功但产物缺失或下载失败时，保留 `succeeded`、`completed=true`，错误写入 `error_message`。此前已成功下载的文件仍保留在媒体数组中。核对这些文件后可交付已完成部分，但不能宣称全部下载或交付成功。

只有 `running` 且无错误时继续轮询；未知状态不能自动当作运行中。命令不提供完整会话消息或用户反问，不编造具体进度。轮询停止条件见 [共用流程](../workflows/async-delivery.md)。
