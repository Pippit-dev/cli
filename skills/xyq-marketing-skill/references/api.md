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
| `general_agent_settings` | object | 本 Skill 必传，无偏好时 `{}` |
| `thread_id` | string | 可选，只在继续已有营销会话时使用 |

正式文档将设置块标为必填，同时注明省略时按默认策略执行。本 Skill 统一传对象来兼容该描述，不为用户补选项。

| 设置字段 | 类型 | 含义 |
| --- | --- | --- |
| `ratio` | int32 | `2=16:9`、`3=9:16`、`4=4:3`、`5=3:4`、`6=1:1`；不是比例字符串 |
| `duration_start` / `duration_end` | int32 | 正整数秒；精确时长两者相等；下限不能大于上限 |
| `show_subtitle` | bool | 是否展示字幕；显式 `false` 必须保留 |
| `video_model` | string | 模型原始标识，保留大小写 |
| `video_resolution` | string | 分辨率偏好，如 `480p`、`720p`、`1080p` |

核对时文档列出 VIP 模型 `seedance2.0_fast_vision`、`seedance2.0_vision`、`Seedance_2.0_mini`，非 VIP 模型 `Seedance_2.0_mini_lite`；注明 `1080p` 目前仅支持 `seedance2.0_vision`。脚本不固化模型白名单，不自动降级或代换用户选择；若服务端拒绝，保留错误并说明参数或权限差异。

## Responses

HTTP 成功还需检查 `ret` 为 `"0"` 或 `0`；失败保留 `errmsg` 和 `log_id`。

- 上传：`data.pippit_asset_id`。
- 提交：`data.run.thread_id`、`data.run.run_id`、`data.run.state`；可有 `data.web_thread_link`。
- 查询：`data.thread_id`、`data.run_id`、`data.run_state`（兼容数字/字符串）；可有 `video_urls`、`image_urls`、`fail_reason: {code, message}`。
- 积分：`data.total_remain_amount` 为数字字符串，零余额为 `"0"`，不可丢失或转为浮点数。

生成状态 1=已创建、2=处理中、3=成功、4=失败、5=已取消。状态 3 不保证产物非空；以返回的真实媒体列表判断交付。
