# 营销 CLI PPE 用例

从仓库根目录运行。脚本调用当前分支原生 `marketing` 命令，复用 CLI 登录态、运行时 `config.toml` 和 Skill 的查询/下载实现，不自行注入 PPE、读取凭据或直接请求 API。可显式选择仅测试入口注入 ROI query。Python 3.11+、Node.js 16+；不自动编译。

## 用例矩阵

| Case | 输入 / 通过条件 |
|---|---|
| MKT-01 | generate 默认预览；message、数字 ratio、false、模型及 platform 原样保留，接口为 submit_marketing_run |
| MKT-02 | 缺 video_model、多余顶层字段、query 缺 ID 均由 CLI 拒绝，不加 --execute |
| MKT-03 | status.logged_in=true；balance ret=0，total_remain_amount 为数字字符串 |
| MKT-04 | upload 已授权本地素材，ret=0 且有 pippit_asset_id；将实际 ID 放进本次生成 asset_ids |
| MKT-05 | generate --execute 仅一次；ret=0，有 thread_id、run_id、log_id；此时尚未判生成成功 |
| MKT-06 | query 同一 thread/run，run_state=3 且 video_urls 非空；下载后进一步用 ffprobe 检查媒体 |
| MKT-07 | balance/upload/submit/query 的准确 LogID 在目标 PPE API 日志中；提交 LogID、Thread、Run 同时出现在目标 PPE Agent 日志中 |
| MKT-08 | 离线反例：错误泳道/Run、缺 API 日志/请求头、进行中/交互状态、无视频均不能通过；失败保留原因；环境不匹配、复用证据目录阻断提交 |
| MKT-09 | 显式 --roi 由测试 Adapter 编码为 query babi_param，保留正文/认证/PPE；非生成请求不注入，缺 fixture 保持原行为；无效 fixture 和重复发送在网络前拒绝 |

`fixtures/request.json` 是 Seedance_2.5 / 720p / 5s / 16:9 的猫咪宣传片用例，配套素材须由执行者提供已授权的本地小猫图片/视频。也可提供其它请求文件；不要填虚构资产 ID。无素材的纯文字用例省略 `--file`，同时调整创作指令。

## 准备与执行

1. 复用 `.local/tests/video/pippit-cli-ppe` 和 `build.json`。脚本核对当前 Go 源码及二进制摘要，拒绝过期构建。该入口包含全部正式 CLI 命令；营销提交可由 tests/marketing/roi.go 在测试 Adapter 中补 query。需要重新构建时必须已有明确编译授权，再按 `tests/video/README.md` 操作。原生 Adapter 的 5 个本地 HTTP 测试只运行已存在且摘要匹配的 binary，不触发编译；缺构建时跳过，过期则失败。
2. 将仓库 `config.toml` 配成 `xyq_api_url = "https://xyq.jianying.com"`、`x-tt-env = "ppe_self_testin_5691a3"`。运行时生效，脚本不改配置；结束后恢复原值。`VIDEO_TEST_OUTPUT` 仅用于既有测试入口的目录检查，不设置请求头。
3. 登录使用现有 CLI status/login，不提供 AK。下列 `--source codex` 仅适用于真实宿主为 Codex，其他宿主替换，未知留空。

```bash
python3 -B -m unittest discover -s tests/marketing -p 'test_*.py' -v
node scripts/marketing-skill.test.js

# OUTPUT 使用新的绝对路径，放在忽略的 .local 或任务 interface-tests 中。
python3 -B tests/marketing/check_marketing.py submit \
  --env ppe_self_testin_5691a3 --source codex \
  --request tests/marketing/fixtures/request.json \
  --file /absolute/path/to/authorized-cat.mp4 --output "$OUTPUT"
python3 -B tests/marketing/check_marketing.py query --output "$OUTPUT" --max-wait 900
```

## 仅测试入口补 ROI

普通 `generate-video` 在 `internal/generate_video/generate_video.go` 构造六个业务标签，经 `internal/common/submit_run.go` 的 `SubmitRunWithBabiParam` JSON 编码为 URL query `babi_param`；显式已有值不覆盖。正式 `marketing generate` 目前未携带此 query，也不接受 ROI 正文字段。用户选择只在测试入口补参，因此生产命令保持原样。

测试可在 submit 加 `--roi tests/marketing/fixtures/roi.json`。脚本把 fixture 复制到该用例目录；已获 API origin 校验的测试 Adapter 只对 submit_marketing_run 读取它、设置 query、在发送前排他记录 `roi-request.json`。只允许业务标签，不注入用户身份/TeamID，不改 entrance_from。没有 --roi 的对照用例保留原始请求。

示例 fixture **逐值复用现有普通视频 CLI**：`ai_agent / front_tool / instant_video`，tab_name=other、edit_type=instant_video、enter_from=skill。它用于检验补充 ROI 后的服务端行为，**不代表正式营销业务的 ROI 标签口径已确定**；不可把测试补参成功当作生产 CLI 已修复。报告同时保留实际 query、用例 fixture 和同一 Run 下游预扣结果。前一用例已终止且已上传素材时，可将其真实 asset_ids 放入新用例 request、不要再次 --file；新用例是有补参变更的独立执行，不能重发状态不明的旧请求。

查询使用 Skill 自带的 10 秒间隔和总超时，只启动一个轮询进程。退出码 3 表示超时，可继续 query 同一目录；4 表示需要交互，查看已有网页链接或 get-thread 后处理同一会话，不能重提原生成；2 为失败/取消/无视频，5 为未知状态。保存原始响应、各次查询和下载记录。下载失败可以查询恢复，不会重发生成。续轮产生新 Run 时应单独记录其真实 ID，本脚本不代替人工确认或自动续轮。

## 服务端路由证明

按保存的 LogID 先查日志，再在 CN `ChinaSinf-North` 按 PSM、准确时间窗和 `_env` 收窄。保存带 `_env`、`_psm`、`__logid` 的结构化 JSON，API 为 `pippit.business.api`，Agent 为 `pippit.business.agent`。仅请求 header、配置文件、ret=0 或缺环境元数据的文本日志不能通过 MKT-07。

```bash
python3 -B tests/marketing/check_marketing.py verify --output "$OUTPUT" \
  --logs /absolute/path/to/api-logs.json /absolute/path/to/agent-logs.json
```

全部必需断言满足才输出 `MARKETING_PPE_CHAIN_VERIFIED`。否则也保存 `verification.json` 的各项结果和失败原因，输出 `MARKETING_PPE_CHAIN_NOT_VERIFIED`、退出 1。四个 API 的入口日志还必须有实际 `x-tt-env` / `x-use-ppe`。有正确请求头但日志环境为 prod 时，记录实际环境并判失败；目标 PPE 查空时应查询同一 LogID 的其它环境，不从查空或请求头推断路由。原始日志只留私有目录，避免把凭据和完整个人信息写入仓库。媒体、服务日志和 exact commit 部署是独立证据；本例不验证所有模型、所有营销类型、团队隔离、计费三元组或核销。营销入口仍是综合 Agent，本脚本不把 Video Part 的 CLI 独立计费规则套到它上面。

已发现的兼容性限制：服务端失败响应可能返回 `video_urls: null` / `image_urls: null`，既有 Skill 的 `checkResponse` 会将其拒绝并退出 1，早于业务失败状态的退出 2。测试保留原始 `query-result.json` 和 `query-errors.txt`，verify 仍可报告实际 run_state/fail_reason；没有在测试适配层把 null 改成空数组来掩盖问题。

补齐日志后可以对同一目录再次 verify；最新结论写入 `verification.json`，历史结论追加到 `verification-history.jsonl`。这不触发生成或远端请求。
