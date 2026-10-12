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

宿主调用 `pippit-tool-cli install` / `update` 时，按可信运行环境静默附加可选 `--source HOST`；直接运行 npm 全局安装时，在该次命令环境中设置 `PIPPIT_CLI_SOURCE`。`HOST` 为真实宿主标识的占位符，不固定平台，未知则省略，不询问用户。安装/更新将该值写入 `host_platform`，保留原有 `source` 和操作系统 `platform`；服务端需支持新字段才能计入统计。旧版命令不支持该参数时省略，不为统计重复安装；内部仅安装 CLI 的脚本沿用原有不上报行为。

营销 API 调用复用其它 CLI 命令的登录态，脚本不读取或导出凭据。执行真实请求前运行 `pippit-tool-cli status`，读取 JSON 的 `logged_in`，不能只看退出码。未登录或凭据过期时运行 `pippit-tool-cli login`，让用户在浏览器完成授权；等待 CLI 成功返回后再次检查 status，再继续原任务。已有有效登录态直接复用，不重复登录；离线预览和帮助不要求登录。

不要让用户提供或复制 `access_token`、Access Key，也不要把凭据写入聊天、请求文件、命令参数或日志。CLI 保留原有 `XYQ_ACCESS_KEY` 显式环境覆盖规则，优先于网页登录；这仅用于已配置的自动化环境，不作为普通用户的必填项。覆盖无效时不能静默切换账号。网页登录凭据被拒绝时按 CLI 的 `login --force` 流程处理，不自动重提可能已创建的生成任务。

先按 [接口契约](references/api.md) 准备请求；需要完整执行示例时读 [商品图到营销视频](examples/product-video.md)。

## Space Selection

非画布业务支持个人和团队空间，复用同一个 CLI 登录凭据。执行前先检查 `pippit-tool-cli space --help` 和业务命令的 `--team-id`；旧版不支持时先更新 CLI，不能忽略团队选择后继续收费操作。

- `space list` 统一输出 JSON，列出个人和当前账号的团队；顶层 `current` 表示已保存的默认空间，列表项 `current: true` 标记当前选择，无法识别本地账号时顶层为 `null`。`team_id` 保留字符串，`0` 为个人。列表中的团队不代表有生成权限、有效订阅或可用积分，最终由服务端判断。
- 用户明确要求长期切换时，运行 `space use TEAM_ID`；`space use 0` 保存个人空间。`use` 必须指定 ID，成功后返回保存的空间，不等待交互输入。只为本次任务选择空间时使用 `--team-id TEAM_ID`，不修改用户默认值；不能按模糊名称或剩余积分自动选团队。
- 新创作未指定空间时，读取并沿用保存的默认空间，首次使用为个人。向用户说明本次空间与积分账户，不重复索要已有授权。一次工作流开始后保存 TeamID，分开的上传、提交、查询和余额命令均显式传相同 `--team-id`，不能中途重新读取默认值。
- 已有任务优先沿用提交结果中的 `team_id` 或 CLI 的任务记录；切换默认团队后可以直接按原 thread/run 查询，CLI 自动恢复原空间。不要把当前默认 TeamID 强加给旧任务。跨设备或旧任务没有记录时，使用已知原空间；未知且用户也未指定时先核实，不能轮流试团队。
- 保存上传结果的资产 ID 和 `team_id`，提交结果的 thread/run ID 和 `team_id`。跨空间资产或任务冲突时停止；无权限、会员失效或余额不足不回退个人或其它团队。提交成功但本地记录写入失败时，保留返回 ID，用原 `--team-id` 查询，不重新生成。
- CLI 向同源 API 统一透传当前团队的 TeamID；模型接口内部继续按个人身份查询；模型列表成功不代表团队有生成权限。Canvas 本轮仅支持个人空间；默认团队时须明确个人意图后用 `--team-id 0`，不能用团队参数执行画布操作。

营销脚本的 `upload / generate / query / balance` 都支持 `--team-id TEAM_ID`。预览也读取同一空间；`query --wait` 固定 TeamID，本地账号可识别时同时固定账号和 API origin，切换账号则停止。无本地账号记录时，显式传入原 `--team-id` 仍可轮询，由 API 按当前凭据鉴权；不能读取默认值代替原任务空间。不要手工构造内部 `space_context`；使用原任务查询命令恢复。不同脚本调用之间仍需由宿主保留任务原 TeamID。

```bash
node scripts/marketing.js upload --file /path/to/product.png --team-id TEAM_ID
node scripts/marketing.js generate --request /path/to/request.json --execute --team-id TEAM_ID
node scripts/marketing.js query --thread-id THREAD_ID --run-id RUN_ID --wait --team-id TEAM_ID
```

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

## Source Attribution

仅营销生成命令（`marketing generate` 或 `marketing.js generate`）静默附加可选 `--source`，由 CLI 映射为请求体 `platform`，不写入创作文本或请求文件。豆包办公填 `doubao_office`，WorkBuddy 填 `workbuddy`，Codex 填 `codex`；其它宿主使用可信环境中的真实稳定标识，未知则省略，不询问用户。不附带版本、会话 ID、用户信息或 prompt。首尾空白去除，未传或空值时先读取 `PIPPIT_CLI_SOURCE`、再尝试已核实的宿主运行标记，仍无法识别时省略；首次生成和同一会话继续提交均适用。上传、查询、下载、余额不附加。旧 CLI 不支持时省略，不为来源统计重提任务。

优先从宿主的系统身份、当前工具运行环境和明确的平台元数据确认真实宿主，并在每次提交主动填写 `--source`；不要仅因字段可选就跳过。未传 `--source`、显式空值或纯空白时，CLI 依次尝试 `PIPPIT_CLI_SOURCE` 和已核实的宿主运行标记。只上报稳定的平台名，不上报标记中的会话 ID；来源冲突或仍无法确认则省略，不询问用户、不扫描登录凭据或安装目录。

## Completion and Recovery

- `ret` 为字符串或数字 `0` 才是 API 业务成功。查询 `data.run_state`：1/2/7 为已提交或进行中；8 为程序中断（可能等待工具回调），有时限地继续查询；3 为本轮完成；4/5 为失败/取消；6/9 为等待用户交互。不能把退出码 0 的单次进行中查询当作生成完成。
- 退出码 4：6=`InputRequired`，9=`HITL_Interrupt`（人工交互中断，当前 Run 的终态）。输出原始响应、`run_state_name`、`action_required=true` 和下一步说明，立即停止轮询。9 不表示视频失败或完成；在同一会话处理确认后查询最新 Run。
- 退出码 5：未知或未指定状态。保留原始响应和任务 ID，停止自动轮询并诊断，不重提生成。
- 退出码 2：生成失败、取消或成功却没有视频。展示 `fail_reason` 或“任务成功但未返回视频”，不编造产物、不自动重提。成功但仅有图片时，原始响应仍包含图片链接，可如实交付为部分产物。
- 退出码 3：等待预算耗尽。保留任务 ID 和最后状态；可继续用相同查询命令等待。网络错误、状态格式异常、业务错误或参数错误退出码 1，先诊断，不自动重发生成。提交超时可能已经创建任务；没有 ID 时报告结果不明确，先查官网任务或联系支持核对 `log_id`。
- 下载失败时，原始响应和已下载文件记录已输出，可用相同 ID 重新查询。脚本只新建文件，不覆盖已有结果；下载不带 API 鉴权头。生产 API 固定使用 `https://xyq.jianying.com`，不跟随鉴权请求重定向。
- 检查本地媒体可打开、内容类型和时长等与交付相符；成功下载仅证明取得了字节，不能宣称创意质量已验收。将每个最终视频/图片通过宿主的附件或媒体预览能力展示，URL/路径作为补充。若当前宿主无法展示附件，明确说明并提供可用本地文件或下载链接。

## Scope

本 Skill 使用 CLI 登录和服务端空间授权，通过 `--team-id` 选择个人或团队，CLI 将其写入 HTTP Header；不向营销请求 JSON 添加 `TeamID` / `team_id`，不跨账号复用资产或任务。沉浸式短片、火山引擎服务不在此脚本范围。
