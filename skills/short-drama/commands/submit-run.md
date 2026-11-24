# short-drama +submit-run：提交与续接

| 参数 | 必填 | 规则 |
| --- | --- | --- |
| `--message` | 是 | 用户原始创作需求、业务内容查询问题或对后端问题的原始回复，不能为空 |
| `--thread-id` | 否 | 续写、修改、回答问题时传原会话 ID |
| `--project-id` | 否 | 使用已有短剧项目。可与 `--thread-id` 同传，服务端会核对归属 |
| `--asset-ids` | 否 | 首次携带唯一剧本的上传 asset_id；同一会话不追加第二个剧本 |
| `--source` | 否 | 按入口的宿主来源统计规则填写，未知则省略 |

```bash
pippit-tool-cli short-drama +submit-run --message "用户的原始短剧需求"
pippit-tool-cli short-drama +submit-run --message "用户的新需求或回复" --thread-id THREAD_ID
pippit-tool-cli short-drama +submit-run --message "用户的新需求" --project-id PROJECT_ID
```

业务内容查询也使用同一 thread_id 提交，按 [业务内容查询](../workflows/creation.md#业务内容查询) 处理；提交查询 Run 不代表授权创作或修改。

以上分别是新建项目、续接会话和使用已有项目。两个 ID 都省略时会创建项目及会话。查询历史已有会话时，可沿用其原空间；只有项目 ID 时，团队项目请显式指定该项目所在的 `--team-id`。多个剧本先让用户选择一个，或在用户要求时分开建会话；即使命令帮助允许重复 asset 参数，也遵守短剧单会话一个剧本的限制。

成功 stdout 为 JSON（值仅为示意）：

```json
{"thread_id":"THREAD_ID","run_id":"RUN_ID","web_thread_link":"https://xyq.jianying.com/..."}
```

新项目和有项目的续接会额外返回 `project_id`。若输出 `submission_status: "unknown"`，先保留原 ID，用 `get-thread` 查询原任务，勿重新提交。

保存 ID、立即展示任务链接，按 [创作与会话续接](../workflows/creation.md) 处理页面与后端问题，并进入 [轮询与文件交付](../workflows/poll-and-deliver.md)。缺少链接时如实说明，不自行拼接。

提交失败按错误处理；网络超时或响应不完整时不能推断未提交，不自动重提。已有会话可先查询该会话，无法确认则报告不确定性。查询或下载失败不能触发新建任务。
