# pippit-tool-cli

面向 Pippit / 小云雀工作流的命令行工具与智能体技能集合。

## 技能列表

本仓库在 `skills/` 目录下包含三个智能体技能：

| 技能 | 说明 | 路径 |
|-------|-------------|------|
| `xyq-short-drama-skill` | 短剧工作流技能，支持提交创作任务、上传参考文件、查询进度、列出会话文件和下载产物。 | `skills/short-drama/` |
| `xyq-skill` | 图片生成与参考图编辑、视频生成、视频超分与擦字幕、异步结果交付、个人 Canvas 编辑、积分查询及登录授权。 | `skills/xyq-nest-skill/` |
| `xyq-marketing-skill` | 商品图文营销一键成片：剧情广告、品牌大片、达人带货，含素材上传、进度查询与媒体下载。 | `skills/xyq-marketing-skill/` |

### 技能路由

- 图片生成与参考图编辑、视频生成（含首尾帧和参考素材）、视频超分、擦字幕、结果查询、个人 Canvas 编辑、积分和授权由 `xyq-skill` 处理。
- 短剧生成、续写、改写、人物设定、分集创作和短剧会话文件处理使用 `xyq-short-drama-skill`。
- 商品图文营销成片、剧情广告、品牌大片和达人带货视频使用 `xyq-marketing-skill`；由公开营销 API 完成创作编排。

需要补充、选择或确认时，使用宿主实际暴露且当前模式允许的工具：Codex 的 `request_user_input` / `request_user_input_async`、WorkBuddy 的 `ask_user_question`；不可用时用普通聊天。

## 小云雀营销成片技能

入口：[skills/xyq-marketing-skill/SKILL.md](skills/xyq-marketing-skill/SKILL.md)。使用 [官网](https://xyq.jianying.com/cli?tab=api) 已开放的营销 API，支持素材上传、营销视频提交、结果查询与下载、积分查询。安装器会从 `skills/` 自动安装该 Skill，也可以单独安装：

```bash
npx skills add Pippit-dev/cli --skill xyq-marketing-skill
```

需要 Node.js 16+，使用 Skill 内自包含脚本，无额外 npm 依赖。脚本从当前进程读取 `XYQ_ACCESS_KEY`；API 与 CLI 可使用同一 Access Key，但 CLI 浏览器登录不会设置此环境变量。密钥在本机安全配置，不写入请求 JSON 或命令参数。

```bash
# 从仓库根目录执行；营销请求字段见接口契约，默认只预览
node skills/xyq-marketing-skill/scripts/marketing.js generate --request request.json --dry-run
# 用户已要求真实生成时提交，然后用返回的真实 ID 取回媒体
node skills/xyq-marketing-skill/scripts/marketing.js generate --request request.json --execute
node skills/xyq-marketing-skill/scripts/marketing.js query --thread-id THREAD_ID --run-id RUN_ID --wait --output-dir ./results
```

参数与错误处理见 [接口契约](skills/xyq-marketing-skill/references/api.md)，完整示例见 [商品图到营销视频](skills/xyq-marketing-skill/examples/product-video.md)。这是 Skill 脚本入口，不是新增的 Go CLI 子命令。营销 API 未公开团队切换字段，不宣称支持团队空间切换。

维护后运行 `node scripts/marketing-skill.test.js` 和 `node scripts/skills.test.js`，验证请求、上传、任务状态、下载与 Skill 引用。测试使用本地模拟服务，不会创建真实付费任务。

## 小云雀图片、视频与媒体处理技能

入口：[skills/xyq-nest-skill/SKILL.md](skills/xyq-nest-skill/SKILL.md)。普通生成请求也直接使用对应 CLI；素材路径交给命令内部上传，异步查询自动下载，最后通过宿主交付真实媒体附件。

| 操作 | CLI | 文档 |
| --- | --- | --- |
| 登录授权 | `status` / `login` / `logout` | [授权](skills/xyq-nest-skill/commands/auth.md) |
| 个人 Canvas 画布与节点编辑 | `canvas` | [画布](skills/xyq-nest-skill/commands/canvas.md) |
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

`get-credit-balance`: 使用当前登录凭证查询个人有效积分余额，并输出 `{"total_remain_amount":"123"}`；零余额会显式输出为 `"0"`。加 `--with-log-id` 可在输出中同时保留本次请求的 `log_id`。

`+submit-run`: 输出 `thread_id`、`run_id` 和 `web_thread_link`；其中 `--message` 为必填参数。
`get-thread`: 请求中带 `version=v2`，并输出 `readable_text`。
`list-thread-file`: 输出会话文件列表、分页提示和可直接传给下载命令的 `file_path`。
`+upload-file`: 输出返回的 `asset_id`。 当前仅支持 `.doc`、`.docx` 和 `.txt` 文件。
`download-result`: 会把结果 URL 下载到 `--output-path` 指定的文件路径；传入 `--updated-at` 后，如果本地文件早于该时间戳会覆盖更新，否则跳过。

短剧命令的错误日志会追加写入本地每日日志文件：`~/.pippit_tool_cli/logs/yyyy-mm-dd.log`。日志路径会基于当前用户主目录和系统路径分隔符生成，因此可在 macOS、Linux 和 Windows 上使用。

## Canvas 原子命令

CLI 提供个人漫剧画布的通用原子命令，不包含特定来源的导入或转换逻辑：

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

使用当前个人登录凭证查询服务端 Skill 模型接口；图片 scene 为 `web_image_agent`，视频为 `web_turbo_video_generator`，不传 TeamID。成功结果按账号、环境和场景隔离缓存 5 分钟，`--refresh` 强制刷新。失败时提示重试，不回退静态列表或过期缓存。`describe` 提供可直接传给生成命令的参数：图片比例保留数字枚举并附比例说明，视频比例转换为字符串，同时整理分辨率、图片推理强度、视频时长和素材限制；不展示内部 `config_key`，未知比例枚举跳过。图片列表和详情仅用 `name` 标识模型，不展示底层模型枚举；搜索、详情查询和 `generate-image --model` 均使用名称，包含空格时加引号。图片详情保留原始 `parameter_config`，包括条件、必选标记、选项说明和参数组合约束。生成仍由服务端校验；服务端尚未开放图片场景或提交参数时会返回错误，不回退静态模型清单。详见 [模型发现](skills/xyq-nest-skill/commands/model.md)。

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

命令输出 `thread_id`、`run_id` 和 `web_thread_link`。提交生视频 HTTP 请求时，参考图、参考视频和参考音频会使用上传接口返回的 `pippit_asset_id`，并分别写入 `video_part_tool_param.images`、`video_part_tool_param.videos` 和 `video_part_tool_param.audios`。图片支持 `.jpg`、`.jpeg`、`.png`、`.gif`、`.bmp`、`.webp`、`.svg`；视频支持 `.mp4`、`.avi`、`.mov`、`.wmv`、`.flv`、`.webm`、`.mkv`、`.m4v`；音频仅支持 `.mp3`、`.wav`。当前可用模型通过 `model list` 查询，参数详情通过 `model describe MODEL_KEY` 查询。CLI 会在提交前校验 prompt 和文件后缀；模型、比例、分辨率等语义校验由服务端处理。

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

## HTTP 客户端

命令模块通过 `common.Runner` 发起服务调用。运行时配置，例如基础地址、HTTP 超时时间和接口路径，由 `internal/config` 加载，并在运行器中与 `common.Client` 组合使用。

## 鉴权

原生 CLI 命令通过 `pippit-tool-cli login` 打开小云雀网页授权，并把本机设备专属凭证保存到系统安全凭证库；Access Key 不会显示在终端。可用 `pippit-tool-cli status` 查看状态、`pippit-tool-cli logout` 清除本机登录。

CI 或 Agent 可继续显式设置 `XYQ_ACCESS_KEY`，它会覆盖本机网页登录凭证；配置错误时不会静默回退到个人登录。会话提交和查询共享上述凭据。

### 宿主来源统计

调用 Skill 提交接口的命令均支持可选 `--source`：`submit-run`、`generate-image`、`generate-video`、`video-super-resolution`、`erase-video-subtitle`、`short-drama +submit-run`。

由宿主 Agent 根据实际环境静默填写稳定标识，例如豆包办公 `doubao_office`、WorkBuddy `workbuddy`、Codex `codex`。其它来源可使用其真实产品标识；来源未知时省略，不询问用户，也不从 prompt 猜测。该值去掉首尾空白后写入请求顶层 `platform`，仅供统计，不参与创作、模型选择或鉴权；未提供/空值时不发送该字段。不会自动读取环境变量、持久化来源或影响查询、上传、下载、Canvas 命令。

```bash
pippit-tool-cli generate-video --prompt "小猫在花园散步" --model Seedance_2.0_mini --source workbuddy
```
