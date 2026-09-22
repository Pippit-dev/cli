# Marketing API Contract

来源：[官网 API 入口](https://xyq.jianying.com/cli?tab=api) 链接的 [正式接口文档](https://bytedance.larkoffice.com/docx/CQOYdJNLioLz6fxRzKXcCsKLnJh)。2026-09-22 核对 revision 597。以下仅整理本 Skill 使用的字段；模型可用性、积分和权限以服务端实际响应为准。

## Endpoints

Base URL：`https://xyq.jianying.com`。全部使用 POST，认证为 `Authorization: Bearer <Access Key>`，`Accept: application/json`。

| 操作 | 路径 | 请求体 |
| --- | --- | --- |
| 上传单文件 | `/api/biz/v1/skill/upload_file` | multipart/form-data，字段 `file`，boundary 由脚本生成 |
| 营销成片 | `/api/biz/v1/agent/submit_marketing_run` | JSON，字段见下表 |
| 查询结果 | `/api/biz/v1/agent/query_generate_video_result` | JSON：真实 `thread_id`、`run_id` |
| 查询积分 | `/api/biz/v1/skill/get_credit_balance` | JSON：`{}` |

## Generate Request

| 字段 | 类型 | 使用规则 |
| --- | --- | --- |
| `message` | string | 必填，非空的原始创作指令 |
| `asset_ids` | string[] | 可选，上传返回的真实 `data.pippit_asset_id` |
| `general_agent_settings` | object | 必传，且必须包含非空 `video_model` |
| `thread_id` | string | 可选，只在继续已有营销会话时使用 |

设置块必须传对象，且 `video_model` 必填。2026-09-22 实际调用及服务端 `validateMarketingGeneralAgentSettings` 均确认：`{}` 或仅有比例/时长会返回 `ret=2`、缺少 `video_model`。不再按文档中的默认策略说明发送空对象；预览和提交都先本地校验。用户未指定模型时先询问，已有明确选择授权时按授权选择，不隐式降级。

| 设置字段 | 类型 | 含义 |
| --- | --- | --- |
| `ratio` | int32 | `2=16:9`、`3=9:16`、`4=4:3`、`5=3:4`、`6=1:1`；不是比例字符串 |
| `duration_start` / `duration_end` | int32 | 正整数秒；精确时长两者相等；下限不能大于上限 |
| `show_subtitle` | bool | 是否展示字幕；显式 `false` 必须保留 |
| `video_model` | string | 必填；模型原始标识，保留大小写 |
| `video_resolution` | string | 分辨率偏好，如 `480p`、`720p`、`1080p` |

核对时文档列出 VIP 模型 `seedance2.0_fast_vision`、`seedance2.0_vision`、`Seedance_2.0_mini`，非 VIP 模型 `Seedance_2.0_mini_lite`；注明 `1080p` 目前仅支持 `seedance2.0_vision`。脚本不固化模型白名单，不自动降级或代换用户选择；若服务端拒绝，保留错误并说明参数或权限差异。

## Responses

HTTP 成功还需检查 `ret` 为 `"0"` 或 `0`；失败保留 `errmsg` 和 `log_id`。

- 上传：`data.pippit_asset_id`。
- 提交：`data.run.thread_id`、`data.run.run_id`、`data.run.state`；可有 `data.web_thread_link`。
- 查询：`data.thread_id`、`data.run_id`、`data.run_state`（兼容数字/字符串）；可有 `video_urls`、`image_urls`、`fail_reason: {code, message}`。
- 积分：`data.total_remain_amount` 为数字字符串，零余额为 `"0"`，不可丢失或转为浮点数。

服务端 `RunState` 状态枚举（2026-09-22 按生成协议代码核实）：

| 值 | 名称 | 查询处理 |
| --- | --- | --- |
| 0 | Unspecified | 未指定；保留响应并停止诊断 |
| 1 | Submitted | 已提交；继续查询 |
| 2 | Working | 处理中；继续查询 |
| 3 | Completed | 当前 Run 完成；检查真实媒体列表 |
| 4 | Failed | 失败；返回原因 |
| 5 | Canceled | 已取消 |
| 6 | InputRequired | 等待补充输入；停止轮询并处理交互 |
| 7 | Generating | 生成中；继续查询 |
| 8 | Interrupt | 程序中断，例如等待工具回调；有时限地继续查询 |
| 9 | HITL_Interrupt | 当前 Run 因人工交互中断结束；处理确认后查询同一 thread 的最新 Run |

查询接口透传 Run 状态；仅在 3 时提取媒体，4/5 时提取失败原因，其它状态返回空媒体数组。因此 9 的空 `video_urls` 不能解释为生成失败。状态 3 也不保证有视频；以真实媒体列表判断交付。未知数值保留原始响应并停止，不自动重提或无期限轮询。

一条 thread 可以有多个 Run。问卷、达人图或工具执行确认会产生后续 Run，原 `run_id` 不会自动变成最新运行。确认后从网页或可用的 `pippit-tool-cli get-thread --thread-id THREAD_ID` 读取新的真实 run_id，再传给营销查询命令；不要猜 ID 或为查询创建新 Run。
