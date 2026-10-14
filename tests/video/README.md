# 生视频 CLI 的 PPE 测试

测试当前 checkout 的 `generate-video`：请求明确携带 `call_source=pippit_cli`，Agent 在指定 PPE 中解析 `cli_turbo_video_generator` 的三元组，并把相同三元组用于当前 Run 的成功预扣。

`cli/main.go` 通过现有 `cmd.ExecuteWithAdapter` 复用正式命令、运行时 `config.toml`、请求头路由和本机登录态；测试 Adapter 只核验与记录，不再自行注入 PPE header。不会增加生产命令参数，不读取或打印 AK；证据只记录允许的 PPE 请求头、测试 body 和响应标识。`check_billing.py` 使用 Python 3.11+ 标准库（包含 `tomllib`）。

同一测试入口也供 `tests/marketing` 复用：营销用例显式提供 roi.json 时，仅对 submit_marketing_run 在测试 Adapter 内补 babi_param query 并记录；实现维护在 tests/marketing/roi.go。Video Part 请求、生产 marketing 命令及 PPE header 注入逻辑保持原样，详见营销测试 README。

覆盖普通文生视频及 `Seedance_2.5_draft --draft` 的 480p 预览（`reference_mode=none`）；Draft 转成片、参考视频、编辑、延长和团队隔离需要各自的输入与断言，不由本脚本推断。使用原 CLI 已认证的身份，不额外注入 UID / TeamID。

离线校验解析器可运行 `python3 -m unittest discover -s tests/video -p 'test_*.py'`。这些是合成日志用例，不构成真实计费验证，也不会触发 Go 编译。

## 1. 读取配置并预检

在 Pippit monorepo 中用只读 wrapper 获取新鲜快照，完整响应保存在任务私有证据目录：

```bash
tools/fetch-tcc-config --site cn --namespace pippit.business.agent \
  --env ppe_self_testin_5691a3 --region SINFONLINE --dir /default \
  --config available_model_list_config --output "$EVIDENCE/tcc-response.json"
```

回到本 CLI 项目根目录：

先将仓库根目录 `config.toml` 中的 `x-tt-env` 改为实际泳道（本例为 `ppe_self_testin_5691a3`），`xyq_api_url` 使用 `https://xyq.jianying.com`。提交脚本要求该值与 `--env` 一致，修改配置无需重新构建。测试结束后可将 `x-tt-env` 清空。

```bash
python3 tests/video/check_billing.py preflight \
  --env ppe_self_testin_5691a3 --tcc-response "$EVIDENCE/tcc-response.json" \
  --model Seedance_2.5 --resolution 720p
```

预检只使用 online，拒绝未发布草稿、缺失 scene / 模型、无匹配规则或 fallback。按规则顺序匹配，不改写 TCC。CLI 三元组允许暂与 Web 相同，报告单独记录 `tuple_differs_from_web`；必须由服务端日志证明实际选中了 CLI scene，不能仅凭三元组数值匹配判定切换。示例模型和规格仍须符合服务端当前准入与能力限制。

验收配置应与 Web 对齐时，preflight / submit 加 `--require-web-equal`，不一致则在提交前停止。先用测试二进制 `model list --type video --refresh`、`model describe <key>` 获取当前账号的模型及有效规格，再逐个提交。每个模型一组文生视频规格不代表全部分辨率和参考模式已做运行验证；可另外完整比较两个 scene 的 `commerce_config_v2`。Draft 预览用 `--model Seedance_2.5_draft --resolution 480p --draft`。

## 2. 显式构建本地入口

仅在已获准执行本地编译后运行：

```bash
python3 tests/video/check_billing.py build
```

只执行 `go build -o .local/tests/video/pippit-cli-ppe ./tests/video/cli`，不跑 `go test`、全仓构建或安装全局 CLI。构建记录保存 HEAD、分支、Go 源码摘要和二进制摘要；提交前重新校验，避免误测已安装版本或过期二进制。

## 3. 提交一个逻辑用例

下面命令会真实生成视频并可能扣积分。`$EVIDENCE` 应为任务 `interface-tests/` 下的私有目录，不能提交 Git；`case-001` 必须不存在。先完成 CLI 登录和 PPE 服务部署、TCC 发布、商业化策略准备。

```bash
python3 tests/video/check_billing.py submit \
  --env ppe_self_testin_5691a3 --tcc-response "$EVIDENCE/tcc-response.json" \
  --model Seedance_2.5 --resolution 720p --duration 5 --ratio 16:9 \
  --prompt "一只小猫在花园中缓慢散步，固定镜头。" \
  --source codex --output "$EVIDENCE/case-001"
```

`--source` 填写实际宿主；未知时省略。命令只执行一次生成，不自动重试。`SUBMITTED_NOT_VERIFIED` 只表示收到 Run 和 LogID，不能证明计费切换成功。

证据包括 `case.json`、`request.json`、`response.json`、`cli.stdout`、`cli.stderr`。发送前即排他创建 `request.json`，超时或响应不明时按已有 LogID / Run 查询，不能换目录盲目重交。

## 4. 获取日志并核验

使用 `response.json` 的真实 `log_id`，在 monorepo 里查询：

```bash
tools/bytedcli --json log get-logid-log "$LOG_ID" \
  --psm pippit.business.agent --vregion ChinaSinf-North --rolling \
  --idc sinfonlinea --idc sinfonline --idc sinfonlinec \
  --limit 200 --max-logs 1000 --poll-timeout 60 --output console \
  > "$EVIDENCE/case-001/agent-logs.json"
```

回到 CLI 项目执行：

```bash
python3 tests/video/check_billing.py verify \
  --output "$EVIDENCE/case-001" --logs "$EVIDENCE/case-001/agent-logs.json"
```

使用 rolling 的 `messages[{key,value}]` 结构保留 `__logid`、`_psm`、`_env` 与 `_msg`；普通文本结果可能省略环境字段，不能作为完整路由证据。只接受日志记录中可确认的同一 LogID、`pippit.business.agent` 和目标 PPE。检查：

- `[CommercialUsage][Resolve] status=success` 的 scene、模型、分辨率、参考模式及三元组匹配预期。
- `[CheckDeductBenefit]` 请求属于当前 Run / Thread / Video Part Agent，三元组与 CLI 配置相同。
- 预扣响应 `Status=1` 且存在唯一的非空 `HistoryID`。

通过后输出 `BILLING_SWITCH_VERIFIED` 并保存 `verification.json`。这只验证选配置与预扣，不宣称视频生成、核销或返还已完成。日志不足、异步链路另有 LogID、格式缺少环境字段时明确失败，继续补充同一次执行的证据，不自动重交。

报告保留日志平台的 `log_retrieval_coverage`。`partial` 表示这次拉取未穷尽全部日志；已取得的正向计费证据可以核验，但不能据此断言整条链路没有其他错误。
