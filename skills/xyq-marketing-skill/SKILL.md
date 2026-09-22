---
name: xyq-marketing-skill
description: 使用小云雀公开营销 API，根据商品图文生成剧情广告、品牌大片或达人带货营销视频；上传素材、提交营销成片、查询进度、下载交付结果及查询积分。用户要求小云雀营销一键成片或接入营销 API 时使用。
user-invocable: true
metadata:
  {"openclaw": {"emoji": "🛍️", "requires": {"bins": ["node"], "env": ["XYQ_ACCESS_KEY"]}}}
---

# 小云雀营销成片

营销一键成片使用独立的公开 API，由服务端编排创作。剧情广告、品牌大片和达人带货由用户的自然语言指令表达，不虚构 `mode`、`template` 等参数。普通模型直出、画布编辑和短剧创作应使用各自已有 Skill；不要把营销成片替换成一次普通生视频。

## Setup

本 Skill 自包含，只需要 Node.js 16+，不依赖其它 Skill 或 npm 安装。脚本位置为 `"{baseDir}/scripts/marketing.js"`；下面示例中的相对路径从本 Skill 目录执行。

API 和 CLI 可以使用同一个 Access Key，但脚本只读取当前进程的 `XYQ_ACCESS_KEY`。CLI 浏览器登录保存的凭据不会自动变成环境变量，不读取系统钥匙串或 CLI 凭据文件。缺少变量时，引导用户在 [官网 API 页](https://xyq.jianying.com/cli?tab=api) 管理 Access Key 并在本机安全配置，不能让用户在聊天中发送密钥，也不要将密钥写入请求文件、命令参数或日志。

先按 [接口契约](references/api.md) 准备请求；需要完整执行示例时读 [商品图到营销视频](examples/product-video.md)。

## Workflow

1. 确认用户要生成的营销内容与商品素材。保留用户原始指令，不主动扩写或加入未经提供的卖点。仅查询、接入咨询或估算不提交生成；明确要求生成即可执行，不重复询问已给出的授权。必要的补充信息用宿主当前允许的 `request_user_input_async`、`request_user_input` 或 `ask_user_question` 询问，没有工具时用普通聊天。
2. 有本地商品素材时逐个上传，保留返回的 `data.pippit_asset_id`。远程素材先取得用户授权使用的本地文件；不能把 URL 或路径放进 `asset_ids`。用户已提供有效资产 ID 时直接复用。无素材的纯文字请求无需上传。
3. 写入 UTF-8 JSON 请求文件。`message` 是用户指令，`general_agent_settings` 只填用户给定的选项，无偏好时传 `{}`。`thread_id` 仅在用户要求继续已有营销会话时传入真实 ID。
4. 先预览校验，已获生成授权后使用 `--execute` 提交。立即保存响应并展示真实 `data.web_thread_link`，保留 `data.run.thread_id`、`data.run.run_id`。缺少网页链接时只报告实际返回信息，不自行拼接链接。
5. 用相同 thread/run 查询到结束并下载媒体。提交成功、网页链接或进度链接都不等于交付完成。已有 ID 的查询/下载请求只取原任务，不重新生成。

```bash
node scripts/marketing.js upload --file /path/to/product.png
node scripts/marketing.js generate --request /path/to/marketing-request.json --dry-run
node scripts/marketing.js generate --request /path/to/marketing-request.json --execute
node scripts/marketing.js query --thread-id THREAD_ID --run-id RUN_ID --wait --max-wait 900 --output-dir /path/to/results
node scripts/marketing.js balance
```

生成默认是离线预览；预览不需要密钥。上传、查询、余额是实际 API 请求。`--timeout` 设置单请求总时限（秒，默认 60）。`--wait` 由脚本每 10 秒查询，默认最多 900 秒；不再叠加其它轮询器。没有 `--wait` 时只查询一次。脚本输出逐行 JSON，查询下载前先输出服务端响应，再逐个输出已下载文件，最后输出含 `downloaded_files` 的响应。

## Completion and Recovery

- `ret` 为字符串或数字 `0` 才是 API 业务成功。查询 `data.run_state`：1/2 为进行中；3 为生成成功；4/5 为失败/取消。不能把退出码 0 的单次进行中查询当作生成完成。
- 退出码 2：生成失败、取消或成功却没有视频。展示 `fail_reason` 或“任务成功但未返回视频”，不编造产物、不自动重提。成功但仅有图片时，原始响应仍包含图片链接，可如实交付为部分产物。
- 退出码 3：等待预算耗尽。保留任务 ID 和最后状态；可继续用相同查询命令等待。网络错误、未知状态、业务错误或参数错误退出码 1，先诊断，不自动重发生成。提交超时可能已经创建任务；没有 ID 时报告结果不明确，先查官网任务或联系支持核对 `log_id`。
- 下载失败时，原始响应和已下载文件记录已输出，可用相同 ID 重新查询。脚本只新建文件，不覆盖已有结果；下载不带 API 鉴权头。生产 API 固定使用 `https://xyq.jianying.com`，不跟随鉴权请求重定向。
- 检查本地媒体可打开、内容类型和时长等与交付相符；成功下载仅证明取得了字节，不能宣称创意质量已验收。将每个最终视频/图片通过宿主的附件或媒体预览能力展示，URL/路径作为补充。若当前宿主无法展示附件，明确说明并提供可用本地文件或下载链接。

## Scope

本 Skill 仅使用正式公开营销接口；不宣称支持团队空间切换。鉴权范围由用户配置的 Access Key 和服务端决定，公开请求没有 `TeamID` 字段，不自行加入团队字段或跨账号复用资产/任务 ID。沉浸式短片、火山引擎服务和现有 CLI 的来源统计参数不在此脚本范围；不要把 `--source` 等未公开字段传给营销接口。
