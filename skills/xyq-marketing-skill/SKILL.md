---
name: xyq-marketing-skill
description: 使用小云雀公开营销 API，根据商品图文生成剧情广告、品牌大片或达人带货营销视频；上传素材、提交营销成片、查询进度、下载交付结果及查询积分。用户要求小云雀营销一键成片或接入营销 API 时使用。
user-invocable: true
metadata:
  {"openclaw": {"emoji": "🛍️", "requires": {"bins": ["node", "pippit-tool-cli"]}}}
---

# 小云雀营销成片

营销一键成片使用独立的公开 API，由服务端编排创作。剧情广告、品牌大片和达人带货由用户的自然语言指令表达，不虚构 `mode`、`template` 等参数。普通模型直出、画布编辑和短剧创作应使用各自已有 Skill；不要把营销成片替换成一次普通生视频。

## Setup

需要 Node.js 16+ 和支持 `marketing` 命令的 `@pippit-dev/cli`。脚本位置为 `"{baseDir}/scripts/marketing.js"`；下面示例中的相对路径从本 Skill 目录执行。先检查 `pippit-tool-cli marketing --help`；命令不存在时安装或更新 CLI：`npm install -g @pippit-dev/cli@latest`，再检查一次；仍不支持就报告版本阻塞，不改为向用户索要密钥。

营销 API 调用复用其它 CLI 命令的登录态，脚本不读取或导出凭据。执行真实请求前运行 `pippit-tool-cli status`，读取 JSON 的 `logged_in`，不能只看退出码。未登录或凭据过期时运行 `pippit-tool-cli login`，让用户在浏览器完成授权；等待 CLI 成功返回后再次检查 status，再继续原任务。已有有效登录态直接复用，不重复登录；离线预览和帮助不要求登录。

不要让用户提供或复制 `access_token`、Access Key，也不要把凭据写入聊天、请求文件、命令参数或日志。CLI 保留原有 `XYQ_ACCESS_KEY` 显式环境覆盖规则，优先于网页登录；这仅用于已配置的自动化环境，不作为普通用户的必填项。覆盖无效时不能静默切换账号。网页登录凭据被拒绝时按 CLI 的 `login --force` 流程处理，不自动重提可能已创建的生成任务。

先按 [接口契约](references/api.md) 准备请求；需要完整执行示例时读 [商品图到营销视频](examples/product-video.md)。

## Workflow

1. 确认用户要生成的营销内容与商品素材。保留用户原始指令，不主动扩写或加入未经提供的卖点。仅查询、接入咨询或估算不提交生成；明确要求生成即可执行，不重复询问已给出的授权。必要的补充信息用宿主当前允许的 `request_user_input_async`、`request_user_input` 或 `ask_user_question` 询问，没有工具时用普通聊天。
2. 有本地商品素材时逐个上传，保留返回的 `data.pippit_asset_id`。远程素材先取得用户授权使用的本地文件；不能把 URL 或路径放进 `asset_ids`。用户已提供有效资产 ID 时直接复用。无素材的纯文字请求无需上传。
3. 写入 UTF-8 JSON 请求文件。`message` 保留用户指令；`general_agent_settings.video_model` 必填，不能传 `{}`。用户指定模型时原样使用；未指定时询问，或在用户已明确授权“你决定”等选择范围内选定并说明。其它选项按用户给定或已授权的偏好设置，不静默换模型。`thread_id` 仅在继续已有营销会话时传入真实 ID。
4. 先预览校验，已获生成授权后使用 `--execute` 提交。立即保存响应并展示真实 `data.web_thread_link`，保留 `data.run.thread_id`、`data.run.run_id`。缺少网页链接时只报告实际返回信息，不自行拼接链接。
5. 用当前 thread/run 查询到结束并下载媒体。遇到确认或问卷时沿用已有授权；缺少必要选择再问用户。确认后继续同一 thread，并取得最新 run_id 再查询：旧 Run 可永久保持等待交互状态。可通过宿主浏览器查看已返回的网页链接；也可用 `pippit-tool-cli` 的 `get-thread --thread-id THREAD_ID` 读取各 Run 的真实 ID。不要用旧 Run 重复确认或重新生成。提交成功、网页链接或进度链接都不等于交付完成。

```bash
node scripts/marketing.js upload --file /path/to/product.png
node scripts/marketing.js generate --request /path/to/marketing-request.json --dry-run
node scripts/marketing.js generate --request /path/to/marketing-request.json --execute
node scripts/marketing.js query --thread-id THREAD_ID --run-id RUN_ID --wait --max-wait 900 --output-dir /path/to/results
node scripts/marketing.js balance
```

生成默认是离线预览；预览不需要登录。上传、查询、余额是实际 API 请求。`--timeout` 设置单请求总时限（秒，默认 60）。`--wait` 由脚本每 10 秒查询，默认最多 900 秒；不再叠加其它轮询器。没有 `--wait` 时只查询一次。脚本输出逐行 JSON，查询下载前先输出服务端响应，再逐个输出已下载文件，最后输出含 `downloaded_files` 的响应。

## Completion and Recovery

- `ret` 为字符串或数字 `0` 才是 API 业务成功。查询 `data.run_state`：1/2/7 为已提交或进行中；8 为程序中断（可能等待工具回调），有时限地继续查询；3 为本轮完成；4/5 为失败/取消；6/9 为等待用户交互。不能把退出码 0 的单次进行中查询当作生成完成。
- 退出码 4：6=`InputRequired`，9=`HITL_Interrupt`（人工交互中断，当前 Run 的终态）。输出原始响应、`run_state_name`、`action_required=true` 和下一步说明，立即停止轮询。9 不表示视频失败或完成；在同一会话处理确认后查询最新 Run。
- 退出码 5：未知或未指定状态。保留原始响应和任务 ID，停止自动轮询并诊断，不重提生成。
- 退出码 2：生成失败、取消或成功却没有视频。展示 `fail_reason` 或“任务成功但未返回视频”，不编造产物、不自动重提。成功但仅有图片时，原始响应仍包含图片链接，可如实交付为部分产物。
- 退出码 3：等待预算耗尽。保留任务 ID 和最后状态；可继续用相同查询命令等待。网络错误、状态格式异常、业务错误或参数错误退出码 1，先诊断，不自动重发生成。提交超时可能已经创建任务；没有 ID 时报告结果不明确，先查官网任务或联系支持核对 `log_id`。
- 下载失败时，原始响应和已下载文件记录已输出，可用相同 ID 重新查询。脚本只新建文件，不覆盖已有结果；下载不带 API 鉴权头。生产 API 固定使用 `https://xyq.jianying.com`，不跟随鉴权请求重定向。
- 检查本地媒体可打开、内容类型和时长等与交付相符；成功下载仅证明取得了字节，不能宣称创意质量已验收。将每个最终视频/图片通过宿主的附件或媒体预览能力展示，URL/路径作为补充。若当前宿主无法展示附件，明确说明并提供可用本地文件或下载链接。

## Scope

本 Skill 仅使用正式公开营销接口；不宣称支持团队空间切换。鉴权范围沿用当前 CLI 登录身份和服务端授权，公开请求没有 `TeamID` 字段，不自行加入团队字段或跨账号复用资产/任务 ID。沉浸式短片、火山引擎服务和现有 CLI 的来源统计参数不在此脚本范围；不要把 `--source` 等未公开字段传给营销接口。
