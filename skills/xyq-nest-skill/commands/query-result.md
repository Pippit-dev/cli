# query-result：查询异步结果并下载

用于生成和视频处理命令返回的任务，也用于用户要求查询的已有任务。默认保留原有图视频查询行为；`generate-audio` 创建的任务必须加 `--audio`，包含 dubbing 返回视频的情况。这个开关选择查询契约，不限制模型。三个参数均必填：

| 参数 | 含义 |
| --- | --- |
| `--thread-id` | 原任务的 `thread_id` |
| `--run-id` | 要查询的那一次运行的 `run_id`，不得混用其他运行 |
| `--download-dir` | 本地输出目录，命令在任务成功时自动下载产物 |

用户未指定目录时使用 `./xyq_output`。已知历史任务但缺少任一 ID 时，从当前上下文获取，无法确定则询问；不要通过重新生成来补 ID。

```bash
pippit-tool-cli query-result --thread-id THREAD_ID --run-id RUN_ID --download-dir "./xyq_output"
```

音频任务：

```bash
pippit-tool-cli query-result --audio --thread-id THREAD_ID --run-id RUN_ID --download-dir "./xyq_output"
```

已有任务沿用生成命令对应的查询模式，不因返回文件的媒体类型切换。缺少来源信息时先从当前任务上下文确认；错误响应可能没有产物，不能据此猜测任务类型。

## 输出契约

stdout 是 JSON；命令可能将错误编码进 JSON 并以退出码 0 返回，因此必须先检查 `error_message`。

| 字段 | 含义 |
| --- | --- |
| `completed` | 是否结束；失败时也可能为 `true`，不等于成功 |
| `error_message` | 非空即错误，不能因为 `completed=false` 而忽略 |
| `thread_id` / `run_id` | 对应的查询任务 |
| `images[]` / `videos[]` | 成功后获取的媒体，每项含 `download_url`、`output_path` |
| `audios[]` | 仅 `--audio` 模式返回；每项含 `download_url`、`output_path`，以及可用的 `name`、`pippit_asset_id`、`duration`（秒） |

成功示例（ID、URL 和文件名仅为示意）：

```json
{
  "completed": true,
  "thread_id": "THREAD_ID",
  "run_id": "RUN_ID",
  "error_message": "",
  "images": [{"download_url": "https://example.com/image.jpeg", "output_path": "./xyq_output/asset.jpeg"}],
  "videos": []
}
```

默认模式保留既有语义：Run 失败或 API 业务错误返回 `completed=true` 和非空错误；取消及其他未成功、未失败状态可能返回 `completed=false`、空错误。命令不提供完整会话消息、用户反问或可区分的所有状态；不能仅凭默认响应断言具体进度、取消状态或等待用户输入。轮询停止条件见 [共用流程](../workflows/async-delivery.md)。

`--audio` 模式中，Run 失败或取消返回 `completed=true` 和非空错误。API 错误响应只有与请求 Run ID 匹配的结构化状态明确为失败或取消，CLI 才返回 `completed=true`、Run 失败原因及可用 LogID。错误响应缺少匹配的终态时，返回 `completed=false` 和以“查询失败：”开头的错误，保留 LogID 与任务 ID。此时未确认 Run 状态；排障后可带 `--audio` 查询同一 Run，不从错误文案推断生成结果，也不自动重新生成或无限轮询。

## 下载行为

文件名由 CLI 根据产物信息生成，以 `output_path` 为准，不自行拼接编号或推测扩展名。同目录已有同名文件可能被复用；复用不证明内容相同，也不代表本次新下载。发现同名文件属于其他产物时，选择用户认可范围内的未冲突目录再查询，不删除已有文件。

`--audio` 模式的音频扩展名只采用已知格式，依次参考元数据、URL 路径、音频名称；无法判断编码时使用 `.audio`。不要仅改后缀就声称完成转码，也不能把 `duration` 当作支持精确时长控制的证据。

找不到产物、链接缺失或下载失败时会返回错误。当前任一文件下载失败可能使整次查询只返回错误，无法据此认定其他文件都没下载或已完整交付。复查本次结果，不把输出目录里的任意旧文件当作本次产物。
