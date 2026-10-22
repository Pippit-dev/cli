# pippit-tool-cli

面向 Pippit / 小云雀工作流的命令行工具与智能体技能集合。

## 本地运行配置

CLI 每次启动时读取**当前工作目录**中的 `config.toml`。在仓库根目录执行 CLI 时使用仓库配置，修改后下一次执行直接生效，无需重新编译：

```toml
xyq_api_url = "https://xyq.jianying.com"
x-tt-env = ""
```

`x-tt-env` 填写本次实际 PPE 环境名，格式为 `ppe_[A-Za-z0-9_]+`。非空时，发往 `xyq_api_url` 的提交、查询、上传等 API 请求自动携带 `x-tt-env: <环境名>` 和 `x-use-ppe: 1`；外部媒体下载不携带这两个 header。留空时不添加 PPE header。仓库默认留空，避免默认路由到某个个人泳道。

文件不存在时继续使用线上默认配置；不会搜索父目录、用户目录或二进制目录。文件格式、字段名或值错误会终止执行。旧的 `PIPPIT_CLI_PPE_ENV` 不再生效；`XYQ_ACCESS_KEY` 等认证设置保持原有行为。

`tests/` 中的验证脚本随源码仓库维护，不包含在 npm 发布包中，也不接入正式 CLI 二进制入口。

## 技能列表

本仓库在 `skills/` 目录下包含三个智能体技能：

| 技能 | 说明 | 路径 |
|-------|-------------|------|
| `xyq-short-drama-skill` | 短剧工作流技能，支持提交创作任务、上传参考文件、查询进度、列出会话文件和下载产物。 | `skills/short-drama/` |
| `xyq-skill` | 图片生成与参考图编辑、视频生成、视频超分与擦字幕、异步结果交付、个人或团队 Canvas 编辑、积分查询及登录授权。 | `skills/xyq-nest-skill/` |
| `xyq-marketing-skill` | 商品图文营销一键成片：剧情广告、品牌大片、达人带货，含素材上传、进度查询与媒体下载。 | `skills/xyq-marketing-skill/` |

### 技能路由

- 图片生成与参考图编辑、视频生成（含首尾帧和参考素材）、视频超分、擦字幕、结果查询、个人或团队 Canvas 编辑、积分和授权由 `xyq-skill` 处理。
- 短剧生成、续写、改写、人物设定、分集创作和短剧会话文件处理使用 `xyq-short-drama-skill`。
- 商品图文营销成片、剧情广告、品牌大片和达人带货视频使用 `xyq-marketing-skill`；由公开营销 API 完成创作编排。

需要补充、选择或确认时，使用宿主实际暴露且当前模式允许的工具：Codex 的 `request_user_input` / `request_user_input_async`、WorkBuddy 的 `ask_user_question`；不可用时用普通聊天。

## 个人与团队空间

同一个 AK 可以切换个人与所属团队，无需重新登录。首次使用默认个人；当前账号与 API origin 的选择独立保存。

```bash
pippit-tool-cli space list           # 查看个人和所属团队，并标记当前默认空间
pippit-tool-cli space use 123456      # 保存团队为默认空间
pippit-tool-cli space use 0           # 保存个人为默认空间
pippit-tool-cli get-credit-balance --team-id 123456 # 仅本次使用团队
pippit-tool-cli get-credit-balance --team-id 0      # 仅本次使用个人
```

`space` 仅提供 `list` 和 `use <TEAM_ID>`，统一输出 JSON。`list` 返回完整分页后的 `spaces` 和已保存的默认空间 `current`，列表项用 `current: true` 标记当前选择；`use` 必须指定 ID，不等待交互输入，成功后返回保存的空间。团队 ID 按 int64 的十进制字符串处理，不接受负数、小数或溢出值。切换到团队前核实账号所属团队列表，失败保留旧选择；切回个人不依赖列表接口。

新任务按「单次 `--team-id` → 已保存默认 → 个人」确定空间；上传、提交、轮询固定同一空间。生成、视频处理、短剧、上传、会话查询、文件列表、积分、营销与 Canvas 命令均适用。同源 API 请求统一携带当前团队的 `X-Team-Id`，由 API 决定是否使用；模型接口内部仍按个人身份查询；Canvas 复用相同空间选择，`--team-id 0` 可单次切回个人。帮助、安装和版本命令无需读取空间或登录。

显式 `--team-id` 不要求 CLI 能读取本地账号标识或空间记录，认证继续使用当前 AK/adapter，由 API 校验团队权限。本地记录可用时照常保存；不可用时本次仍固定使用指定空间，宿主应保存返回的任务 ID 和 `team_id`，后续显式传入原 TeamID。`space use` 保存默认空间仍需要可识别的账号；`space list` 在账号不可识别时返回 `current: null`。

提交成功会按当前账号、API origin 保存 thread/run 的空间记录，上传成功保存资产归属。之后即使切到另一团队，直接 `get-thread --thread-id ...`、`query-result`、`list-thread-file` 或 `marketing query` 仍查询原任务；带原 thread 的续写也使用原空间。显式指定与记录冲突的团队会在请求前停止，已知跨空间素材同样会拦截。跨设备或旧任务无记录时应显式传原 `--team-id`；失败不会遍历其它团队。查询成功后可补齐记录。

空间记录位于系统用户配置目录的 `pippit-cli/spaces/`，按账号与 API origin 摘要隔离，每条记录原子保存，不存 AK。空间账号标识使用本地登录凭证的 UID 摘要，同 UID 换 AK 保留选择。损坏记录报错，不当作首次使用；按错误提示用原 `--team-id` 查询恢复。保存失败不把已经成功的提交改成失败：输出保留 ID 与 `team_id`，警告写 stderr，后续沿用原空间查询，不能重提。

业务 stdout 保留既有结构；上传和提交增加字符串 `team_id`，`status` 增加 `default_space`，空间提示写 stderr。`marketing.js query --wait` 自动向各次 CLI 子进程传递固定空间；有账号记录时同时传递账号/API origin 约束，切换账号会停止。无本地账号记录时可显式传入 `--team-id` 轮询，各次请求仍由 API 鉴权；默认空间切换不会改变已固定的 TeamID。团队出现在列表中不等于有 WRITE 权限、有效会员或可用积分；最终由服务端授权和扣费，CLI 不自动回退。

`space list/use TEAM_ID` 依赖 API 部署 `/api/biz/v1/skill/get_lite_team_list` 的 AK 适配入口，业务团队使用还依赖对应 API 团队权限实现；不直连 commerce Web 接口。宿主 Agent 的选择、任务恢复与不回退规则已写入三个 Skill 的 `Space Selection` 章节。

## 小云雀营销成片技能

入口：[skills/xyq-marketing-skill/SKILL.md](skills/xyq-marketing-skill/SKILL.md)。使用 [官网](https://xyq.jianying.com/cli?tab=api) 已开放的营销 API，支持素材上传、营销视频提交、结果查询与下载、积分查询。安装器会从 `skills/` 自动安装该 Skill，也可以单独安装：

```bash
npx skills add Pippit-dev/cli --skill xyq-marketing-skill
```

需要 Node.js 16+ 和支持 `marketing` 命令的 CLI。Skill 脚本通过原生 `marketing` 命令复用 CLI 登录态；先运行 `pippit-tool-cli status`，未登录时执行 `pippit-tool-cli login` 完成浏览器授权，无需用户提供 access_token 或 Access Key。生成请求通过 stdin 传入原生 CLI，凭据仅在 CLI 内用于鉴权。

```bash
# 从仓库根目录执行；营销请求字段见接口契约，默认只预览
node skills/xyq-marketing-skill/scripts/marketing.js generate --request request.json --dry-run
# 用户已要求真实生成时提交，然后用返回的真实 ID 取回媒体
node skills/xyq-marketing-skill/scripts/marketing.js generate --request request.json --execute
node skills/xyq-marketing-skill/scripts/marketing.js query --thread-id THREAD_ID --run-id RUN_ID --wait --output-dir ./results
```

参数与错误处理见 [接口契约](skills/xyq-marketing-skill/references/api.md)，完整示例见 [商品图到营销视频](skills/xyq-marketing-skill/examples/product-video.md)。Skill 脚本通过原生 `marketing` 命令执行；两者均支持 `--team-id`，空间经 HTTP Header 传递，营销请求 JSON 无需增加字段。

维护后运行 `node scripts/marketing-skill.test.js` 和 `node scripts/skills.test.js`，验证请求、上传、任务状态、下载与 Skill 引用。测试使用本地模拟服务，不会创建真实付费任务。

## 小云雀图片、视频与媒体处理技能

入口：[skills/xyq-nest-skill/SKILL.md](skills/xyq-nest-skill/SKILL.md)。普通生成请求也直接使用对应 CLI；素材路径交给命令内部上传，异步查询自动下载，最后通过宿主交付真实媒体附件。

| 操作 | CLI | 文档 |
| --- | --- | --- |
| 登录授权 | `status` / `login` / `logout` | [授权](skills/xyq-nest-skill/commands/auth.md) |
| 个人或团队 Canvas 画布与节点编辑 | `canvas` | [画布](skills/xyq-nest-skill/commands/canvas.md) |
| 生图、参考图编辑 | `generate-image` | [图片](skills/xyq-nest-skill/commands/generate-image.md) |
| 查看可用图片/视频模型、参数配置 | `model list` / `model describe` | [模型发现](skills/xyq-nest-skill/commands/model.md) |
| 生视频、首尾帧 | `generate-video` | [视频](skills/xyq-nest-skill/commands/generate-video.md) |
| 视频超分 | `video-super-resolution` | [超分](skills/xyq-nest-skill/commands/video-super-resolution.md) |
| 擦字幕 | `erase-video-subtitle` | [擦字幕](skills/xyq-nest-skill/commands/erase-video-subtitle.md) |
| 查询并下载结果 | `query-result` | [查询](skills/xyq-nest-skill/commands/query-result.md) |
| 查积分 | `get-credit-balance` | [积分](skills/xyq-nest-skill/commands/get-credit-balance.md) |

### 安装与执行

```bash
node /path/to/xyq-skill/scripts/ensure-cli.js
```

保存返回的 `cli_path`，后续用带引号的绝对路径替换示例中的命令名。同一任务复用路径；已有命令齐全的 CLI 不下载，缺少必需命令时自动升级。ZIP 应包含整个 Skill 目录，具体环境条件与故障处理见 [安装说明](skills/xyq-nest-skill/scripts/install.md)。`node scripts/install-cli.js` 是 npm 包内仅安装 CLI 的入口，不安装或清理全局 Skill。

Canvas 任务使用 `ensure-cli.js --canvas`，额外返回 `canvas_entry`；原生资产命令使用 `cli_path`，语义命令通过 `node "CANVAS_ENTRY" canvas command ...` 执行。检查会真实加载 npm 内的离线命令目录，避免把原生帮助误当作运行时已就绪。画布编辑使用独立的 [查询、编辑与回读流程](skills/xyq-nest-skill/workflows/canvas-edit.md)，不套用媒体轮询。

登录后选择生成或处理命令，统一接入 [异步结果与媒体交付](skills/xyq-nest-skill/workflows/async-delivery.md)。完整基础案例见 [生成一张图并交付](skills/xyq-nest-skill/examples/generate-and-deliver.md)，组合案例由入口按需引导。

### 模块维护

- `SKILL.md` 维护能力边界、意图到命令的路由及必要执行规则。
- `commands/` 每个模块维护适用场景、必填与可选参数、最小调用、真实返回契约及失败处理；授权相关命令合并在同一文档。
- `workflows/` 维护共用轮询与媒体交付规则；`examples/` 展示基础完整流程及易混淆的组合场景，引用规则，不复制参数手册。
- 新增 CLI 时补命令文档、入口路由、`ensure-cli.js` 必需命令集合和安装测试；声明是同步结果还是异步任务，是否需要附加运行时及其检查方式，按需接入交付流程，补正常、缺输入和易混淆场景用例。
- 文档使用 Skill 内相对链接，打包时保留结构。规范副本位于 `skills/xyq-nest-skill/`，项目发现入口 `.agents/skills/xyq-skill` 指向该目录。
- 修改后运行 `node scripts/skills.test.js` 与 `node scripts/install-cli.test.js`，检查引用完整、保留命令与安装检查一致及缺命令升级/缓存复用；Agent 行为用例见 [测试场景](skills/xyq-nest-skill/tests/agent_test_cases.md)。这些检查不代表真实生成已验证。

## CLI 更新渠道与发布

普通用户使用稳定渠道（npm `latest`）；试用用户主动选择 `canary`：

```bash
pippit-tool-cli update                  # 默认安装 latest
pippit-tool-cli update --channel canary # 本次安装 canary，并更新配套 Skills
pippit-tool-cli update --channel stable # 本次安装 latest，并更新配套 Skills
```

`--channel` 只影响本次更新，不保存渠道偏好。下一次不带参数的 `update` 仍走 `latest`，因此可能从试用版退回当前稳定版。显式 `--channel` 优先于 `PIPPIT_CLI_INSTALL_PACKAGE`；不传渠道时保留该环境变量指定包版本的既有行为。新版本提示仍检查稳定渠道。

首次安装试用版，或旧 CLI 尚不支持 `--channel` 时，使用现有安装入口：

```bash
npx --prefer-online @pippit-dev/cli@canary install
pippit-tool-cli --version
```

该命令会替换全局 CLI 和配套 Skills。`canary` 是公开 npm 分发渠道，适合主动参与的小范围试用，不限制谁可以下载。Skill 的 `ensure-cli.js` 在需要下载时仍使用 `latest`；希望试用的用户应先安装 canary。

维护者发布步骤：

1. 保持 `package.json`、`package-lock.json` 与 `vX.Y.Z` tag 的版本一致；使用普通版本号，不使用 `-beta` / `-rc` 后缀（现有安装器按普通版本号定位二进制）。
2. 推送 tag 后，`Release` workflow 构建 GitHub Release 产物并执行 `npm publish --tag canary`，保留原 `latest`。GitHub Release 产物仍公开可见，本流程控制的是 npm 分发渠道。
3. 试用用户安装 canary，核对版本并验证实际业务流程。
4. 验证通过后，在 GitHub Actions 的 `Promote canary to latest` workflow 中点击 `Run workflow`，填写已验证的 `X.Y.Z`。该 workflow 须先合入默认分支才会显示手动入口，并复用仓库已有的 `NPM_TOKEN`。
5. 提升流程要求输入版本与当前 `canary` 一致，只移动 npm `latest` 指针并回读确认，不重新构建或发布 npm 包。如果 canary 已更新，需要验证新的候选版本后再提升。

GitHub Release 已成功、npm 发布失败时的补发步骤：

1. 在 GitHub Actions 选择 `Republish npm from Release`，点击 `Run workflow`，填写已有的 `vX.Y.Z` tag。该 workflow 须先合入默认分支，复用已有 `NPM_TOKEN`。
2. 流程检出指定 tag，校验源码版本、已发布的六个平台产物及 `checksums.txt`，复用这些二进制，只准备并发布 npm canary，不运行 GoReleaser。
3. 目标必须高于当前 latest 和 canary，且 npm 上尚无该版本；发布前查询异常会停止。npm 接收发布后可能暂时返回 404，流程会有上限地重试读取版本与渠道，确认 canary 指向目标版本、latest 保持原值。回读超时只表示结果尚未确认，先核实 npm 实际处理状态，不连续重复发布，也不删除已有 Release 或重打 tag。

维护者回滚步骤：

1. 在 [GitHub Actions](https://github.com/Pippit-dev/cli/actions) 选择 `Rollback CLI`，点击 `Run workflow`。该 workflow 须先合入默认分支；复用 `NPM_TOKEN` 修改 npm 标签，使用只读 `GITHUB_TOKEN` 检查 Release。
2. 填写 `channel`（`stable` 对应 `latest`，或 `canary`）、`expected_version`（预期渠道当前版本）和 `target_version`（已验证的目标旧版本）。两个版本均须为普通 `X.Y.Z`，目标必须更低；例如 `stable / 1.0.39 / 1.0.38`，仅在确认 `1.0.38` 可用后操作。
3. 流水线核对渠道当前值、目标 npm 版本及 GitHub Release 元数据：版本 tag 匹配、已公开发布、六种平台/架构的压缩包及 `checksums.txt` 均已上传且非空。该检查不运行旧版程序，不替代业务验证；回滚依赖这些历史产物继续保留。
4. 写入前再次核对当前渠道，随后只移动所选 npm 标签并回读确认。不会重新构建、发包、修改 Git tag 或改变另一渠道。Actions 日志和运行摘要会记录目标及确认结果；写入或回读失败时，先用 `npm view @pippit-dev/cli dist-tags --prefer-online` 核实实际状态，再决定是否重试。
5. 已安装问题版本的用户需要主动执行 `pippit-tool-cli update --channel stable`（回滚 canary 时使用 `--channel canary`）并核对 `pippit-tool-cli --version`。旧 CLI 可执行默认 `update` 获取 `latest`，但须确保未设置 `PIPPIT_CLI_INSTALL_PACKAGE` 覆盖变量。已运行的进程及 Skill 独立缓存中的 CLI 不会被渠道回滚自动替换。

发布、补发、提升和回滚共用并发组，避免这些 workflow 同时修改渠道；不取消正在运行的任务。GitHub 默认只保留一个等待中的同组任务，连续触发时请检查是否有等待任务被替换。并发组不能锁住维护者在终端执行的 npm 写入，操作期间应避免其它渠道修改。此阶段不提供百分比放量或自动回滚，普通客户端也不会因指针变化而自动安装；显式指定问题版本的安装仍然可用。

## 短剧工作流技能

包发布后可以通过 npm 安装。安装器会按当前系统下载匹配的预构建二进制文件，支持 macOS、Linux 和 Windows：

```bash
npx @pippit-dev/cli@latest install
pippit-tool-cli login
pippit-tool-cli --version
pippit-tool-cli get-credit-balance
pippit-tool-cli short-drama +submit-run --message "写一个赛博朋克短剧开头"
pippit-tool-cli short-drama +upload-file --path ./reference.doc
pippit-tool-cli get-thread --thread-id thread_123 --run-id run_456
pippit-tool-cli list-thread-file --thread-id thread_123 --page-num 1 --page-size 200
pippit-tool-cli download-result --output-path ./thread_123/results/result.mp4 --url URL --updated-at 1779716734
```

`get-credit-balance`: 使用当前登录凭证查询所选个人或团队空间的有效积分余额，并输出 `{"total_remain_amount":"123"}`；零余额会显式输出为 `"0"`。加 `--with-log-id` 可在输出中同时保留本次请求的 `log_id`。

`+submit-run`: 输出 `thread_id`、`run_id` 和 `web_thread_link`；其中 `--message` 为必填参数。
`get-thread`: 请求中带 `version=v2`，并输出 `readable_text`。
`list-thread-file`: 输出会话文件列表、分页提示和可直接传给下载命令的 `file_path`。
`+upload-file`: 输出返回的 `asset_id`。 当前仅支持 `.doc`、`.docx` 和 `.txt` 文件。
`download-result`: 会把结果 URL 下载到 `--output-path` 指定的文件路径；传入 `--updated-at` 后，如果本地文件早于该时间戳会覆盖更新，否则跳过。

短剧命令的错误日志会追加写入本地每日日志文件：`~/.pippit_tool_cli/logs/yyyy-mm-dd.log`。日志路径会基于当前用户主目录和系统路径分隔符生成，因此可在 macOS、Linux 和 Windows 上使用。

## Canvas 原子命令

CLI 提供个人和团队漫剧画布的通用原子命令，不包含特定来源的导入或转换逻辑：

```bash
# 首次使用时打开小云雀网页授权
pippit-tool-cli login
pippit-tool-cli status

# 创建、分配资产 ID、查询、上传与提交单个画布 transaction
pippit-tool-cli canvas create --title "CLI Canvas" --wait
pippit-tool-cli canvas allocate --count 3
pippit-tool-cli canvas get --asset-id PIPPIT_ASSET_ID
pippit-tool-cli canvas upload --path ./reference.png
pippit-tool-cli canvas apply --project-id PROJECT_ID --file ./patch.json
```

五个命令复用 `space use TEAM_ID` 保存的默认空间，也支持单次 `--team-id TEAM_ID`，个人空间使用 `0`。操作已有画布时显式使用其原 `--team-id`，CLI 不根据画布 ID 自动选择团队。`canvas get` 返回实际 `team_id`，本地账号可识别时还返回 `space_context`；`canvas command run` 据此固定后续子进程的空间与账号/API origin，期间切换默认空间不影响当前操作。本地恢复记录和检查点按账号、团队与画布隔离，原有个人记录保持可读。

五个命令均输出单行 JSON，资源 ID 保持字符串。`allocate` 只预留 ID，实际资产仍由后续 `apply` transaction 创建。`create` 的 `request_id` 用于追踪，不是跨服务崩溃窗口的严格幂等键；写请求结果不明确时不要盲目重放，应先使用 `canvas get` 回读确认。`apply` 当前只接受一个 transaction，但该 transaction 可以包含多个 patches；CLI 会严格检查 transaction ACK 和每个目标资产的新版本。

通过 npm 安装的 CLI 还提供基于同一 Canvas SDK 的语义命令目录：

```bash
# 先看精简目录，再按类别或参数定位
pippit-tool-cli canvas command list
pippit-tool-cli canvas command list --category timeline
pippit-tool-cli canvas command describe create_biz_node
pippit-tool-cli canvas command describe create_biz_node --node-kind role
pippit-tool-cli canvas command describe xyq.timeline.apply --operation set_output_size
pippit-tool-cli canvas command describe xyq.generation.update_prompt --path properties.prompt

# 完整 schema 按需导出；不指定命令时导出全部
pippit-tool-cli canvas command schema xyq.timeline.apply
pippit-tool-cli canvas command schema

# 离线指南：先取主题索引，再读正文
pippit-tool-cli canvas command guide
pippit-tool-cli canvas command guide storyboard

# 由 SDK 业务工厂创建角色节点；修改会通过现有 canvas apply 原子提交
pippit-tool-cli canvas command run create_biz_node \
  --canvas-id PIPPIT_CANVAS_ASSET_ID \
  --input '{"nodeKind":"role","initialData":{"nodeName":"测试角色"}}'
```

`canvas command` 由 npm 包内固定的 Canvas SDK 运行时提供，复用网页登录、`canvas get`、`canvas allocate` 和 `canvas apply`；不会读取或打印 Access Key，也不直接选择服务端地址。公开目录只包含已登记的 mutation 和业务命令，不开放任意内部 command 调用。

`list [--category <category>]` 返回精简命令目录；`describe <command>` 说明入口参数，按 `--operation <name>` 查看一种领域操作、按 `--node-kind <kind>` 查看业务节点初始字段、按 `--path <schema.path>` 查看 schema 子路径。需要完整嵌套结构时使用 `schema [command]` 显式导出。`create_biz_node.nodeKind` 包含 `scene3d` 与 `timeline-composition`；字段枚举、必填项、默认值与动态来源以当前安装版本的 schema 为准。

发现输出使用 `schema_version: 2`：`list` 只包含名称、分类和摘要；`describe` 的 `schema_view: "summary"` 表示展示视图，嵌套内容通过 `schema_path` 继续展开，不能直接当作完整校验 schema。原来从 `list` 或 `describe` 读取完整 `input_schema` 的脚本应改用 `schema [command]`。默认索引预算为 16 KiB，单次字段说明预算为 32 KiB；完整导出需要显式调用，执行命令的输入与返回值不受这一发现协议调整影响。

`guide [topic]` 提供无需登录的离线帮助。无主题时仅返回索引，可选 `storyboard`、`prompt-references`、`time`、`timeline`、`scene3d`；正文包含单位、ID 来源、前置条件和最小示例。指南不启用新能力，先用 `list` 确认本机运行时支持哪些命令。故事板指南说明原生 `<duration-ms>` 标签累加与引用格式；目前没有公开的故事板脚本编辑、镜头排序或指定镜头生成领域命令，通用视频生成与资产补丁不能替代其业务流程。

3D 导演台和多轨道都通过外层画布节点定位，其编辑内容保存在节点引用的独立文档或草稿资产中。先查询取得内部对象、轨道、片段 ID 和版本，再执行编辑：

```bash
pippit-tool-cli canvas command describe xyq.scene3d.apply
pippit-tool-cli canvas command run xyq.scene3d.query \
  --canvas-id CANVAS_ID --input '{"nodeId":"DIRECTOR_NODE_ID"}'
pippit-tool-cli canvas command run xyq.scene3d.apply \
  --canvas-id CANVAS_ID \
  --input '{"nodeId":"DIRECTOR_NODE_ID","operations":[{"command":"create_node","args":{"kind":"camera","id":"camera-2","name":"Close-up"}}]}'

pippit-tool-cli canvas command describe xyq.timeline.apply
pippit-tool-cli canvas command run xyq.timeline.query \
  --canvas-id CANVAS_ID --input '{"nodeId":"TIMELINE_NODE_ID"}'
# expectedRevision 使用上一步返回的 draft.revision
pippit-tool-cli canvas command run xyq.timeline.apply \
  --canvas-id CANVAS_ID \
  --input '{"nodeId":"TIMELINE_NODE_ID","expectedRevision":0,"commands":[{"type":"set_output_size","payload":{"width":1920,"height":1080}}]}'
```

领域命令的 `dryRun:true` 会完整预演编辑并保留原文档。多轨时间以整数微秒表示；3D 关键帧以帧表示，动作片段 `trimStart/trimEnd` 以源动画秒数表示。3D 对象旋转以度表示，几何体的 `theta/phi/arc` 参数以弧度表示，具体以字段 schema 为准。新增多轨素材须复用已有来源，或关联真实画布素材节点。截图、渲染导出、上传和生成仍需各自的运行环境。

运行结构为 `npm 的 JS 入口 → Canvas SDK CJS → Go 二进制的资产命令`。Go 二进制可独立执行其原生命令，无需安装 Go；`canvas command` 需要 npm 包中的 Node.js 入口与 CJS 运行时。

图片或视频节点通过 `xyq.generation.update_prompt` 更新提示词，`prompt` 直接使用前端已有的标签文本。CLI 与前端粘贴调用同一份 SDK 标签解析、引用匹配和连边逻辑：

```bash
pippit-tool-cli canvas command describe xyq.generation.update_prompt
pippit-tool-cli canvas command run xyq.generation.update_prompt \
  --canvas-id CANVAS_ID \
  --input '{"nodeId":"TARGET_IMAGE_NODE_ID","prompt":"参考 <node-asset label=\"人物\">REFERENCE_IMAGE_NODE_ID</node-asset> 的人物，改为雨夜街景"}'
```

示例 ID 应替换为查询到的真实节点或资产 ID。`get_asset` 可查看当前节点与草稿；`describe` 按需查看参数，`schema` 导出完整输入结构。角色连边沿用前端既有默认选择与草稿处理，标签属性原样保留给编辑器和提交解析器。无需另传 `text/reference` 数组、`asset` 包装或 `referenceSource`；引用类型和独立素材前置条件可查看 `guide prompt-references`。

直接上传或从素材库选出的素材可以没有节点。对于已在目标 `generation.references` 草稿中的素材，直接使用其 `pippitAssetId`：

```bash
pippit-tool-cli canvas command run xyq.generation.update_prompt \
  --canvas-id CANVAS_ID \
  --input '{"nodeId":"TARGET_IMAGE_NODE_ID","prompt":"参考 <pippit-asset-id label=\"参考图\">PIPPIT_ASSET_ID_IN_DRAFT</pippit-asset-id> 的人物"}'
```

当前版本只解析已有画布候选与目标草稿中的引用；找不到的普通标签返回 `UNRESOLVED_PROMPT_REFERENCE`，不写入文档。仅拿到 `canvas upload` 返回的 ID，还不会自动查询并加入草稿。新独立素材 ID 的自动解析属于后续能力。已有独立素材草稿可由前端上传或素材库流程产生，视频生成可使用其中的图片、视频和音频。同一媒体 ID 若同时匹配到画布源节点，则遵循前端现有的节点优先规则建立关联。`canvas get --asset-id PIPPIT_ASSET_ID` 可查询外部素材，但查询本身不添加引用。

该命令先在隔离文档上按前端原有顺序执行 SDK commands，再把实际补丁一次性提交，保留模型参数、已有引用及 caption/title/name 等展示字段。`dryRun:true` 只执行隔离预演，原文档和撤销历史不变；清空 prompt 不移除引用。节点引用由现有生成流程转换成 `node_asset_refs`，独立图片进入 `pippit_asset_ids`，视频生成的独立素材按类型进入 `images`、`videos`、`audios`。本命令不触发生成、上传或远端素材查询。不要用浅合并的 `update_asset.contentPatch.generation` 更新提示词，否则可能覆盖其他生成参数。

## 生图 CLI

`generate-image` 使用图片模型的展示名称；先查询当前列表并选择模型，再上传本地参考图片并提交生图请求。以下假设列表包含该名称：

```bash
pippit-tool-cli generate-image \
  --prompt "生成一张小猫海报" \
  --image "~/images/cat.png" \
  --model "智能图片V2.5 Fast" \
  --ratio 6 \
  --generate-image-count 2
```

命令输出 `thread_id`、`run_id` 和 `web_thread_link`。提交 HTTP 请求时，`agent_name` 固定为 `pippit_nest_agent`，参考图会使用上传接口返回的 `pippit_asset_id` 写入顶层 `asset_ids`，生图模型写入 `general_agent_settings.image_model`，比例写入 `general_agent_settings.ratio`，生图数量写入 `general_agent_settings.generate_image_count`。`--model` 为必填参数，填写 `model list --type image` 返回的完整名称，例如 `"智能图片V2.5 Fast"`。CLI 在上传素材前查询或复用有效缓存，解析名称后仅在提交请求中填写服务端模型标识；名称缺失、重复或未找到时停止，不猜测模型。

图片 `model list/search/describe` 返回给宿主的是 API 下发的展示名称 `name`，例如 `美学模型 8.2`。生成时填写 `--model "美学模型 8.2"`，CLI 仅在提交请求中使用该条目对应的 `key`。名称、可用模型和参数均由接口动态提供，不维护静态名称映射。

`--ratio` 可选，只接受服务端 `Ratio` 数字枚举，例如 `--ratio 3` 表示 `9:16`。通过 `model describe "模型名称" --type image` 查看当前可用的数字 `options/default`，`option_labels` 说明每个数字对应的比例。CLI 校验整数格式，模型是否支持该枚举由服务端决定。常用枚举值含义如下：

| ratio 参数 | IDL 枚举 | 含义 |
| ---: | --- | --- |
| `0` | `CanvasRatioOriginal` | 原始比例（自动） |
| `2` | `CanvasRatio16To9` | 16:9（横屏） |
| `13` | `CanvasRatio21To9` | 21:9（电影） |
| `3` | `CanvasRatio9To16` | 9:16（竖屏） |
| `4` | `CanvasRatio4To3` | 4:3 |
| `5` | `CanvasRatio3To4` | 3:4 |
| `6` | `CanvasRatio1To1` | 1:1 |

`--generate-image-count` 可选，填写生图数量，对应 IDL 字段 `GeneralSettingsPart.GenerateImageCount` / JSON 字段 `generate_image_count`。CLI 只校验不能为负数；具体数量范围由服务端决定。

`--resolution` 写入 `general_agent_settings.resolution`（转大写），新增 `--effort` 写入 `general_agent_settings.image_effort`（转小写）。分辨率和推理强度选项都来自该模型的动态配置；没有 `effort` 维度的模型不展示推理强度选择。只传用户指定的参数，未指定时省略，不自动补查询默认值。

图片支持 `.jpg`、`.jpeg`、`.png`、`.gif`、`.bmp`、`.webp`、`.svg`。CLI 会在提交前校验 prompt、model 必填、ratio 格式、generate-image-count 非负和文件后缀；不新增模型或参数组合白名单。

## 图片与视频模型发现

```bash
# 图片模型及可选参数
pippit-tool-cli model list --type image
pippit-tool-cli model search "智能图片" --type image
pippit-tool-cli model describe "智能图片V2.5 Fast" --type image
pippit-tool-cli model list --type image --refresh

# 省略 --type 保持查询视频
pippit-tool-cli model list
pippit-tool-cli model search MiniMax
pippit-tool-cli model describe MiniMax-H3
pippit-tool-cli model list --refresh
```

使用当前登录凭证查询服务端 Skill 模型接口；图片 scene 为 `web_image_agent`，视频为 `web_turbo_video_generator`。团队空间的请求携带 `X-Team-Id`，API 内部仍按个人身份查询模型。成功结果按账号、环境和场景隔离缓存 5 分钟，`--refresh` 强制刷新。失败时提示重试，不回退静态列表或过期缓存。`describe` 提供可直接传给生成命令的参数：图片比例保留数字枚举并附比例说明，视频比例转换为字符串，同时整理分辨率、图片推理强度、视频时长和素材限制；不展示内部 `config_key`，未知比例枚举跳过。图片列表和详情仅用 `name` 标识模型，不展示底层模型枚举；搜索、详情查询和 `generate-image --model` 均使用名称，包含空格时加引号。图片详情保留原始 `parameter_config`，包括条件、必选标记、选项说明和参数组合约束。生成仍由服务端校验；服务端尚未开放图片场景或提交参数时会返回错误，不回退静态模型清单。详见 [模型发现](skills/xyq-nest-skill/commands/model.md)。

## 生视频 CLI

`generate-video` 会上传本地参考图片、视频和音频，然后向视频片段 Agent 提交生视频请求：

```bash
pippit-tool-cli generate-video \
  --prompt "做个小猫视频" \
  --image "~/images/cat1.jpg" \
  --image "~/images/cat2.jpg" \
  --video "~/images/video1.mp4" \
  --video "~/images/video2.mp4" \
  --audio "~/audio/bgm.mp3" \
  --duration 5 \
  --ratio "9:16" \
  --model "Seedance_2.0_mini_lite" \
  --resolution "720p"
```

命令输出 `thread_id`、`run_id` 和 `web_thread_link`。提交生视频 HTTP 请求时，参考图、参考视频和参考音频会使用上传接口返回的 `pippit_asset_id`，并分别写入 `video_part_tool_param.images`、`video_part_tool_param.videos` 和 `video_part_tool_param.audios`。图片支持 `.jpg`、`.jpeg`、`.png`、`.gif`、`.bmp`、`.webp`、`.svg`；视频支持 `.mp4`、`.avi`、`.mov`、`.wmv`、`.flv`、`.webm`、`.mkv`、`.m4v`；音频仅支持 `.mp3`、`.wav`。当前可用模型通过 `model list` 查询，参数详情通过 `model describe MODEL_KEY` 查询。CLI 会在提交前校验 prompt 和文件后缀，仅传入非空 `--draft-task-id` 时允许省略 prompt；模型、比例、分辨率等语义校验由服务端处理。

首尾帧生视频时，按首帧、尾帧的顺序传入两次 `--image`，并设置 `--generate-type 1`：

```bash
pippit-tool-cli generate-video \
  --prompt "让镜头从首帧平滑过渡到尾帧" \
  --image "~/images/first.jpg" \
  --image "~/images/last.jpg" \
  --duration 5 \
  --ratio "16:9" \
  --model "Seedance_2.0_mini" \
  --resolution "720p" \
  --generate-type 1
```

`--generate-type` 可选，填写后原样写入 `video_part_tool_param.generate_type`；值 `1` 表示首尾帧生成。CLI 保持图片上传和请求中的输入顺序，不在本地校验该参数的枚举值，具体能力与约束由服务端决定。

### Seedance 2.5 Draft

复用 `generate-video` 分两次提交。需要目标服务端支持 Draft 协议和无 prompt 的成片请求。

```bash
# 样片
pippit-tool-cli generate-video --model Seedance_2.5_draft --draft \
  --prompt "小猫钓鱼视频" --task-type reference --duration 10 --ratio 16:9
pippit-tool-cli query-result --thread-id DRAFT_THREAD_ID --run-id DRAFT_RUN_ID --download-dir ./draft

# 用户预览后要求生成成片：使用 videos[].draft_task_id 原值
pippit-tool-cli generate-video --model Seedance_2.5_draft --draft-task-id DRAFT_TASK_ID
pippit-tool-cli query-result --thread-id FINAL_THREAD_ID --run-id FINAL_RUN_ID --download-dir ./final
```

`query-result` 在视频结果中保留可选的 `draft`、`draft_task_id`，继续返回 `download_url` 和 `output_path`。两阶段分别计费，下游固定生成 480p 样片和 1080p 成片；样片创建后 7 天内可转成片。CLI 不自动续跑，不要求重复提示词和素材。

新增 `--task-type`、`--seed` 透传生成模式与 seed。完整参数与两阶段示例见 [生视频命令](skills/xyq-nest-skill/commands/generate-video.md)。

## 视频处理工具 CLI

`video-super-resolution` 会上传一个本地视频并提交视频超分任务：

```bash
pippit-tool-cli video-super-resolution \
  --video "~/videos/source.mp4" \
  --output-resolution "1080p" \
  --tool-version "standard"
```

`--output-resolution` 必填，当前服务端支持 `720p`、`1080p`、`2k`、`4k`。`--tool-version` 可选，当前服务端支持 `standard`、`professional_v1`、`professional_v2`；省略时由服务端使用 `standard`。CLI 不重复校验这些枚举值，具体能力与约束由服务端决定。

`erase-video-subtitle` 会上传一个本地视频并提交擦字幕任务：

```bash
pippit-tool-cli erase-video-subtitle \
  --video "~/videos/with-subtitle.mp4"
```

两个命令都会把 `agent_name` 固定为 `pippit_video_part_agent`。上传接口返回的 `pippit_asset_id` 不会写入顶层 `asset_ids` 或普通参考视频列表，而是分别写入以下服务端专属参数：

- 超分：`video_part_tool_param.mini_tool_param.tool_param.video_super_resolution_tool_param.video.pippit_asset_id`
- 擦字幕：`video_part_tool_param.mini_tool_param.tool_param.erase_video_subtitle_tool_param.video.pippit_asset_id`

两个命令都输出 `thread_id`、`run_id` 和 `web_thread_link`。拿到任务 ID 后，可继续使用 `query-result` 查询并下载结果。

查询并下载生图/生视频结果：

```bash
pippit-tool-cli query-result \
  --thread-id "skill_xxx" \
  --run-id "skill_xxx" \
  --download-dir "./output"
```

`query-result` 会查询指定 Run 并输出 JSON。Run 成功完成后下载视频和图片产物，`completed=true`，`videos` 和 `images` 中各包含 `download_url` 和 `output_path`；图片扩展名取自产物 `metadata.format`，缺省时兜底 `.png`。Run 失败也视为终态，`completed=true` 且填充 `error_message`；Run 未到终态时 `completed=false`。

## Adapter 接入边界

公共接口位于 `internal/adapter`，通过实例注入适用于当前运行环境的命令和 HTTP 请求策略；公共命令与 HTTP 层只依赖接口，不依赖具体实现。接口提供 `CLIOptions`、`PrepareAPIRequest`、`WrapTransport`、`StripSensitiveHeaders` 四个方法，`adapter.Default{}` 提供默认行为。

- 根入口 `main.go` 调用 `cmd.Execute()`，使用完整命令、本机认证和原始 API 路由。
- 同进程集成可使用 `cmd.NewRootCommandWithAdapter(stdout, stderr, policy)` 或 `common.NewHTTPClientWithAdapter(baseURL, timeout, authorizer, policy)`；传 nil 使用默认 adapter，不存在全局 adapter 注册。
- 同源判断由公共 HTTP client 按 scheme、hostname、有效端口执行；只有同源 API 请求才调用 `PrepareAPIRequest`。非同源请求清除默认认证和 adapter 管理的敏感字段；API 跨域重定向会被拒绝。
- `AuthProvided` 只表示 adapter 已附加认证材料，不代表服务端验证通过；失败立即返回，不回退到本机认证。`UseDefaultAuth` 才调用默认认证器，未知决策会被拒绝。
- CLI 策略与 Cobra 解耦。`DefaultAllowRootCommands` 明确指定是否默认放行一级业务命令；为 false 时只开放 `AllowedRootCommands` 白名单，nil 和空列表都表示没有白名单命令。默认 adapter 将该开关设为 true，本机认证和自更新仍受各自开关控制；标准帮助和版本行为仍由 Cobra 提供。可通过 `auth.WithCLIOptions` 将本机认证能力配置应用于独立 Manager。
- `space` 按普通业务命令白名单开放，不随 `LocalCredentialsEnabled` 隐藏。`space list` 复用请求认证；没有可识别账号时仍可查询列表，JSON 的 `current` 为 `null`。`space use` 需要稳定账号标识，缺失时明确报错；不读取或写入匿名空间配置。显式 `--team-id` 的业务请求仍可使用 adapter 认证。关闭本地凭据后，未用于实际认证的环境 AK 不作为宿主账号标识。
- 业务 client 与更新遥测 client 都在构造时包装 Transport；遥测 client 属于命令实例，避免使用不同 adapter 的实例共享可变客户端。

例如，自定义 adapter 可以复用默认实现，仅调整命令能力，并在自己的程序入口注入：

```go
type customAdapter struct {
    adapter.Default
}

func (customAdapter) CLIOptions() adapter.CLIOptions {
    options := adapter.Default{}.CLIOptions()
    options.SelfUpdateEnabled = false
    return options
}

// 程序入口：cmd.ExecuteWithAdapter(customAdapter{})
```

新增业务请求应复用 `Runner.Client`；需要独立 `http.Client` 时，通过所注入策略的 `WrapTransport` 包装。直接创建未包装的客户端不会自动应用策略。

## HTTP 客户端

命令模块通过 `common.Runner` 发起服务调用。运行时配置，例如基础地址、HTTP 超时时间和接口路径，由 `internal/config` 加载，并在运行器中与 `common.Client` 组合使用。

## 鉴权

原生 CLI 命令通过 `pippit-tool-cli login` 打开小云雀网页授权，并把本机设备专属凭证保存到系统安全凭证库；Access Key 不会显示在终端。可用 `pippit-tool-cli status` 查看状态、`pippit-tool-cli logout` 清除本机登录。

CI 或 Agent 可继续显式设置 `XYQ_ACCESS_KEY`，它会覆盖本机网页登录凭证；配置错误时不会静默回退到个人登录。会话提交和查询共享上述凭据。

### 安装与更新来源统计

宿主执行安装/更新时，按可信运行环境静默传入真实稳定标识，未知时省略；不固定为某个平台，不询问用户。以下 `HOST` 是占位符：

```bash
pippit-tool-cli install --source HOST
npx @pippit-dev/cli install --source HOST
pippit-tool-cli update --source HOST
```

直接使用 `npm install -g @pippit-dev/cli` 时，可由宿主在该次命令的环境中设置 `PIPPIT_CLI_SOURCE`。安装和更新均按非空显式 `--source`（命令支持时）、`PIPPIT_CLI_SOURCE`、已核实宿主运行标记的顺序识别来源；去掉首尾空白后，未传或空值继续尝试下一来源。该值不持久化；生成命令运行环境中若也存在此变量，同样会读取它作为宿主来源，具体兜底规则见下文。

`report_telemetry` 新增可选请求字段 `host_platform` 上报宿主标识；原有 `source` 保留 `npm_install`、`npx_install`、`cli_update`，`event` 区分 install/update，`platform` 仍表示操作系统。服务端通过独立的 `host_platform` 指标标签统计宿主来源。帮助不安装也不上报，`PIPPIT_CLI_DISABLE_TELEMETRY=1` 仍可关闭上报。内部 `install-cli.js` 仅安装二进制，沿用不安装 Skill、不上报的原有行为。

### 宿主来源统计

生成提交命令均支持可选 `--source`：`generate-image`、`generate-video`、`video-super-resolution`、`erase-video-subtitle`、`short-drama +submit-run`、`marketing generate`。

由宿主 Agent 根据实际环境静默填写稳定标识，例如豆包办公 `doubao_office`、WorkBuddy `workbuddy`、Codex `codex`。其它来源可使用其真实产品标识；来源未知时省略，不询问用户，也不从 prompt 猜测。该值去掉首尾空白后写入请求顶层 `platform`，仅供统计，不参与创作、模型选择或鉴权；无法解析来源时不发送该字段。省略 `--source`、显式空值或纯空白时，CLI 按以下顺序尽力补齐来源：

1. 宿主显式设置的 `PIPPIT_CLI_SOURCE`。
2. 运行标记：`CODEX_THREAD_ID` / `CODEX_SESSION_ID` → `codex`，`CLAUDECODE` → `claude_code`，`CURSOR_AGENT` → `cursor`，`GEMINI_CLI` → `gemini_cli`，`OPENCODE` → `opencode`，`QWEN_CODE` → `qwen_code`，`CLINE_ACTIVE` → `cline`，`COPILOT_CLI` / `COPILOT_AGENT_SESSION_ID` → `github_copilot_cli`。
3. 标记缺失、为 0/false 或不同宿主标记冲突时不猜测，省略来源。

非空显式 `--source` 始终优先，空值继续尝试环境识别。仅消费环境标记是否存在，不上传会话标识，不根据 API Key、已安装软件、普通终端名或用户 prompt 猜来源，也不持久化。查询、上传、下载、Canvas 不附加来源。营销脚本的预览与提交采用相同规则；原生 update 也复用该解析，仍写入 `host_platform`，保留旧 source。豆包办公、WorkBuddy 等尚无已核实运行标记的宿主，应优先主动传参或设置上述环境变量。

Cursor 标记依据：[官方终端文档](https://docs.cursor.com/en/agent/terminal)；Gemini 标记依据：[官方命令文档](https://geminicli.com/docs/reference/commands/)。Codex 标记已在本机运行环境核实；Claude Code 标记已在本机安装产物核实。

新增标记依据：[OpenCode 启动入口](https://github.com/anomalyco/opencode/blob/388406238bd5ca15564a762840a2362c3a45bd9c/packages/opencode/src/index.ts#L75-L77)、[Qwen Code shell 环境](https://github.com/QwenLM/qwen-code/blob/e6e2c9efd1dd48bf93143510f7ed1c2413bcea6b/packages/core/src/services/shellExecutionService.ts#L988-L994)、[Cline VS Code 终端环境](https://github.com/cline/cline/blob/e6ad24c43aa2d7551cecdbb97a15269566d3650f/apps/vscode/src/hosts/vscode/terminal/VscodeTerminalRegistry.ts#L28-L34)、[GitHub Copilot CLI 变更记录](https://github.com/github/copilot-cli/blob/main/changelog.md)（0.0.421 增加 `COPILOT_CLI`，1.0.29 增加 `COPILOT_AGENT_SESSION_ID`）。只在子进程实际继承标记时识别，不保证所有宿主版本或启动方式都提供这些标记。

```bash
pippit-tool-cli generate-video --prompt "小猫在花园散步" --model Seedance_2.0_mini --source workbuddy
```
