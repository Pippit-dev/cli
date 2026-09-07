# pippit-tool-cli

面向 Pippit / 小云雀工作流的命令行工具与智能体技能集合。

## 技能列表

本仓库在 `skills/` 目录下包含两个智能体技能：

| 技能 | 说明 | 路径 |
|-------|-------------|------|
| `xyq-short-drama-skill` | 短剧工作流技能，支持提交创作任务、上传参考文件、查询进度、列出会话文件和下载产物。 | `skills/short-drama/` |
| `xyq-skill` | 通用创作技能，支持 NestAgent 图片/视频生成与编辑，并在视频模型直出时调用 `pippit-tool-cli generate-video`。 | `skills/xyq-nest-skill/` |

### 技能路由

- 通用图片/视频生成、编辑和复杂参考素材编排使用 `xyq-skill`。
- 用户明确要求视频模型直出、指定视频模型或直接调用 CLI 时，由 `xyq-skill` 调用 `pippit-tool-cli generate-video`，再用 `query-result` 查询和下载结果。
- 短剧生成、续写、改写、人物设定、分集创作和短剧会话文件处理使用 `xyq-short-drama-skill`，不要与通用创作流程混用。

两份技能需要用户补充、选择或确认时，优先调用宿主的结构化提问工具：Codex 使用 `request_user_input`，WorkBuddy 使用 `ask_user_question`，Trae 和其他宿主使用实际暴露的同类工具；没有同类工具时退回普通聊天提问。

## 通用 NestAgent 技能

`xyq-skill` 通过接入小云雀 NestAgent 的综合创作能力，实现 AI 图片/视频生成、编辑、风格转换、图片/视频/mp3或wav音频文件上传、进度查询和结果下载；视频模型直出请求直接使用 `pippit-tool-cli generate-video`。

### 功能特性

| 功能 | 说明 |
|------|------|
| 创建会话 / 发送消息 | 向小云雀发送自然语言指令，生成图片或视频。 |
| 查询会话进展 | 增量拉取会话消息，轮询创作进度和产物结果。 |
| 上传文件 | 上传图片/视频/mp3或wav音频到小云雀资产库，获取 `asset_id` 用于编辑和参考。 |
| 下载结果 | 批量下载生成的图片/视频到本地，支持并行下载。 |
| 视频模型直出 | 调用 `generate-video` 提交请求，展示 `web_thread_link`，再用 `query-result` 查询并下载视频。 |

小云雀平台能力覆盖：

- 生成：文生图、文生视频、图生视频、视频续写。
- 编辑：局部修改、元素替换、镜头调整、风格迁移。
- 复杂创作：一句话生成短剧、复刻视频/TVC/宣传片、音乐 MV 生成、产品展示片制作。

### 配置

所有 `xyq-skill` 脚本都使用 Bearer 令牌鉴权：

```bash
export XYQ_ACCESS_KEY="<access-key>"
```

可选 API 地址：

```bash
export XYQ_OPENAPI_BASE="https://xyq.jianying.com"
# 或
export XYQ_BASE_URL="https://xyq.jianying.com"
```

### 创建会话 / 发送消息

```bash
# 创建新会话
python3 skills/xyq-nest-skill/scripts/submit_run.py --message "生一个动漫视频"

# 向已有会话发送消息
python3 skills/xyq-nest-skill/scripts/submit_run.py \
  --message "再生成一个故事视频" \
  --thread-id THREAD_ID

# 携带参考文件发送
python3 skills/xyq-nest-skill/scripts/submit_run.py \
  --message "参考这个视频做修改" \
  --asset-ids asset_id1 asset_id2
```

| 参数 | 必填 | 说明 |
|------|------|------|
| `--message` | 是 | 创作指令内容。 |
| `--thread-id` | 否 | 已有会话 ID，不传则创建新会话。 |
| `--asset-ids` | 否 | 资产 ID 列表，支持多个。 |

返回示例：

```json
{
  "thread_id": "90f05e0c-...",
  "run_id": "abc123-..."
}
```

### 查询会话进展

```bash
python3 skills/xyq-nest-skill/scripts/get_thread.py \
  --thread-id THREAD_ID \
  --run-id RUN_ID \
  --after-seq 0
```

| 参数 | 必填 | 说明 |
|------|------|------|
| `--thread-id` | 是 | 会话 ID。 |
| `--run-id` | 否 | 运行 ID。 |
| `--after-seq` | 否 | 增量拉取起始序号，默认 `0`。 |

脚本会返回会话消息和产物条目。后续轮询时，根据已获取消息更新 `after_seq`。

### 上传文件

```bash
# 上传图片
python3 skills/xyq-nest-skill/scripts/upload_file.py /path/to/image.png

# 上传视频
python3 skills/xyq-nest-skill/scripts/upload_file.py /path/to/video.mp4

# 上传音频
python3 skills/xyq-nest-skill/scripts/upload_file.py /path/to/audio.mp3
```

仅支持 `image/*`、`video/*` 和 `.mp3/.wav` 音频文件，单文件大小限制 200 MB。

返回示例：

```json
{
  "asset_id": "asset_xxx"
}
```

### 下载结果

```bash
python3 skills/xyq-nest-skill/scripts/download_results.py \
  --urls URL1 URL2 URL3 \
  --output-dir ./xyq_output \
  --prefix "storyboard" \
  --workers 5
```

| 参数 | 必填 | 说明 |
|------|------|------|
| `--urls` | 是 | 要下载的 URL 列表。 |
| `--output-dir` | 否 | 输出目录，默认 `./xyq_output`。 |
| `--prefix` | 否 | 文件名前缀，例如 `storyboard_01.png`。 |
| `--workers` | 否 | 并行下载线程数，默认 `5`。 |

返回示例：

```json
{
  "output_dir": "./xyq_output",
  "downloaded": ["./xyq_output/storyboard_01.png"],
  "total": 1
}
```

### 典型示例

文生视频：

```text
1. submit_run.py --message "生成一个赛博朋克风格的城市夜景视频"
2. 每 10 秒轮询：
   get_thread.py --thread-id THREAD_ID --run-id RUN_ID --after-seq SEQUENCE
3. 拿到产物 URL 后下载：
   download_results.py --urls URL1 URL2 --output-dir ./output --prefix "cyberpunk"
```

编辑已有视频：

```text
1. upload_file.py /path/to/video.mp4
2. submit_run.py --message "把背景换成星空" --asset-ids asset_id
3. 按文生视频流程轮询和下载。
```

多参考图/视频生成：

```text
1. upload_file.py /path/to/ref1.png
2. upload_file.py /path/to/ref2.png
3. upload_file.py /path/to/ref3.mp4
4. submit_run.py --message "根据参考图和视频生成科普故事视频" --asset-ids asset_id1 asset_id2 asset_id3
5. 按文生视频流程轮询和下载。
```

在已有会话中追加需求：

```text
1. submit_run.py --message "把刚才的视频加个片头" --thread-id EXISTING_THREAD_ID
2. 使用新的 run_id 轮询和下载。
```

轮询策略：

- 间隔：每 10 秒查询一次。
- 增量拉取：首次 `--after-seq 0`，后续根据已获取消息数更新 seq。
- 意图确认：如果智能体追问用户，先展示问题，再用同一个 `thread_id` 提交用户回复。
- 超时：连续轮询 48 小时无结果则停止。
- 错误重试：单次失败可重试 1 次，连续 3 次失败则停止。

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
# 查看全部公开命令及其参数说明
pippit-tool-cli canvas command list
pippit-tool-cli canvas command describe create_biz_node

# 由 SDK 业务工厂创建角色节点；修改会通过现有 canvas apply 原子提交
pippit-tool-cli canvas command run create_biz_node \
  --canvas-id PIPPIT_CANVAS_ASSET_ID \
  --input '{"nodeKind":"role","initialData":{"nodeName":"测试角色"}}'
```

`canvas command` 由 npm 包内固定的 Canvas SDK 运行时提供，复用网页登录、`canvas get`、`canvas allocate` 和 `canvas apply`；不会读取或打印 Access Key，也不直接选择服务端地址。公开目录只包含已登记的 mutation 和业务命令，不开放任意内部 command 调用。

## 生图 CLI

`generate-image` 会上传本地参考图片，然后向综合 Nest Agent 提交生图请求：

```bash
pippit-tool-cli generate-image \
  --prompt "生成一张小猫海报" \
  --image "~/images/cat.png" \
  --model "seedream_4.5" \
  --ratio 6 \
  --generate-image-count 2
```

命令输出 `thread_id`、`run_id` 和 `web_thread_link`。提交 HTTP 请求时，`agent_name` 固定为 `pippit_nest_agent`，参考图会使用上传接口返回的 `pippit_asset_id` 写入顶层 `asset_ids`，生图模型写入 `general_agent_settings.image_model`，比例写入 `general_agent_settings.ratio`，生图数量写入 `general_agent_settings.generate_image_count`。`--model` 为必填参数，CLI 只做非空校验，具体模型值是否可用由服务端决定。

`--ratio` 可选，填写服务端 `Ratio` 枚举值。CLI 只做整数格式解析，不检查枚举值是否在下表范围内；具体值是否可用由服务端决定。常用枚举值含义如下：

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

图片支持 `.jpg`、`.jpeg`、`.png`、`.gif`、`.bmp`、`.webp`、`.svg`。CLI 会在提交前校验 prompt、model 必填、ratio 整数格式、generate-image-count 非负和文件后缀。

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

命令输出 `thread_id`、`run_id` 和 `web_thread_link`。提交生视频 HTTP 请求时，参考图、参考视频和参考音频会使用上传接口返回的 `pippit_asset_id`，并分别写入 `video_part_tool_param.images`、`video_part_tool_param.videos` 和 `video_part_tool_param.audios`。图片最多 9 张，支持 `.jpg`、`.jpeg`、`.png`、`.gif`、`.bmp`、`.webp`、`.svg`；视频最多 3 个，支持 `.mp4`、`.avi`、`.mov`、`.wmv`、`.flv`、`.webm`、`.mkv`、`.m4v`；音频最多 3 个，仅支持 `.mp3`、`.wav`。普通用户支持模型 `Seedance_2.0_mini_lite`；`seedance2.0_vision`、`seedance2.0_fast_vision`、`Seedance_2.0_mini` 和 `Seedance_2.5` 为 VIP 专属模型。CLI 会在提交前校验 prompt、素材数量和文件后缀；模型、比例、分辨率等语义校验由服务端处理。

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

CI 或 Agent 可继续显式设置 `XYQ_ACCESS_KEY`，它会覆盖本机网页登录凭证；配置错误时不会静默回退到个人登录。`skills/xyq-nest-skill/scripts` 下的独立 Python 脚本尚未接入原生 CLI 凭证库，当前仍需要该环境变量。
