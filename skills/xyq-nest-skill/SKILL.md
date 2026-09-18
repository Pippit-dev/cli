---
name: xyq-skill
description: 通过小云雀的 AI 能力进行综合创作，支持生成和编辑图片/视频，并在用户明确要求图片或视频模型直出、指定图片或视频模型或直接调用 CLI 时使用 pippit-tool-cli generate-image / generate-video；用户要求视频超分、提升视频清晰度、擦字幕或去字幕时，使用 video-super-resolution / erase-video-subtitle。覆盖文生图、文生视频、图生视频、首尾帧生视频、视频编辑、风格转换、视频续写、视频复刻、TVC、宣传片、音乐 MV、产品广告、分镜和教育短视频等场景。当用户提到小云雀、xyq、上传参考图/视频/mp3或wav音频、查看生成进度，或查询小云雀积分余额、剩余积分、credits 时也应触发；积分查询使用 pippit-tool-cli get-credit-balance。
user-invocable: true
metadata:
  {
    "openclaw":
      {
        "emoji": "💬",
        "requires":
          {
            "bins": ["python3", "node"],
            "env": ["XYQ_ACCESS_KEY"]
          },
        "primaryEnv": "XYQ_ACCESS_KEY"
      }
  }
---

# 小云雀创作、图片/视频模型直出、视频处理与积分查询

通过 小云雀的API 创建会话、发送消息（生图、生视频、编辑视频等）、上传图片/视频/mp3或wav音频文件，并查询会话消息进展；通过 CLI 查询个人有效积分余额。

小云雀是一个 AI 综合创作平台，同时为人类创作者和 Agent 设计。Agent 通过 Skill 入口理解任务、调用模型并自动编排工作流。

每次开始执行本技能任务时，先按“前置要求”运行 `scripts/ensure-cli.js`，检查已有 CLI，不存在或缺少必需命令时获取最新版本。本文命令中的 `pippit-tool-cli` 均代表该脚本返回的 `cli_path`；实际执行时替换为带引号的绝对路径。

**平台核心能力：**
- **生成**：文生图、文生视频、图生视频、视频续写
- **编辑**：局部修改、元素替换、镜头调整、风格迁移
- **视频处理**：视频超分、提升视频清晰度、擦字幕
- **复杂创作**：复刻已有视频风格做 TVC/宣传片、用音乐生成 MV、产品展示片制作

除“图片/视频模型直出”和“视频超分/擦字幕”外，创作和编辑需求通过发送自然语言消息来完成，后端 Agent 会自主编排工作流。复杂任务耗时较长，需耐心轮询。

## 执行路由（必须先判断）

积分余额查询优先走路由 E，不进入创作或视频处理工作流。

### 路由 A：图片模型直出

满足任一条件时，必须直接使用 `pippit-tool-cli generate-image`，不要改走 `pippit-tool-cli submit-run`：

- 用户明确说“图片模型直出”、“直接调图片模型”或明确要求用 CLI 生图。
- 用户指定了具体图片模型（如 `seedream_5.0_pro`），并希望单次直接生成图片。
- 上游流程已明确将任务标记为图片 direct-model / 模型直出。

执行原则：

1. 执行前完成“前置要求”的CLI 安装检查，使用返回的 `cli_path`；失败时报告阻塞，不要悄悄降级到会话 API。
2. 真实提交会消耗 credits；如果用户本轮尚未明确确认生成，按“用户确认与反问”规则征得明确确认后再运行。
3. 保留用户原始 prompt，不要自行扩写、润色、翻译或增加风格词。
4. `--model` 必填；用户未提供图片模型时，先询问使用哪个模型。只添加用户已经给出的 `--ratio`、`--resolution`、`--generate-image-count`、`--image` 参数，不补默认值。
5. `--resolution` 的使用说明是：仅 `seedream_5.0_pro` 支持 `1K`、`2K`、`4K`。不要在 skill 侧维护额外 allowlist 或自行改写用户值，实际合法性由服务端决定。
6. `generate-image` 返回后，保存 `thread_id`、`run_id`，并立即向用户展示 `web_thread_link`。
7. 每隔 10 秒调用 `query-result`，直到 `completed=true`。出现 `error_message` 时停止并报告；成功后按“媒体交付完成标准”逐项交付 `images[].output_path` 对应的图片文件。

```bash
pippit-tool-cli generate-image \
  --prompt "用户原始描述" \
  --model IMAGE_MODEL
  --ratio RATIO
  --resolution RESOLUTION
  --generate-image-count COUNT
  --image 参考图路径

pippit-tool-cli query-result \
  --thread-id THREAD_ID \
  --run-id RUN_ID \
  --download-dir OUTPUT_DIR
```

### 路由 B：视频模型直出

满足任一条件时，必须直接使用 `pippit-tool-cli generate-video`，不要改走 `pippit-tool-cli submit-run`：

- 用户明确说“视频模型直出”、“直接调模型”或“直接调用 CLI”。
- 用户指定了具体视频模型（如 `Seedance_2.5`），并希望单次直接生成视频。
- 用户明确要求“首尾帧生视频”、指定首帧和尾帧，或要求从第一张图过渡到第二张图。
- 上游流程已明确将任务标记为 direct-model / 模型直出。

执行原则：

1. 执行前完成“前置要求”的CLI 安装检查，使用返回的 `cli_path`；失败时报告阻塞，不要悄悄降级到会话 API。
2. 真实提交会消耗 credits；如果用户本轮尚未明确确认生成，按“用户确认与反问”规则征得明确确认后再运行。
3. 保留用户原始 prompt，不要自行扩写、润色、翻译或增加风格词。
4. 只添加用户已经给出的 `--model`、`--duration`、`--ratio`、`--resolution`、`--image`、`--video`、`--audio`、`--generate-type` 参数；未给参数交给 CLI 默认值。
5. 普通用户支持模型 `Seedance_2.0_mini_lite`；VIP 专属模型包括 `seedance2.0_vision`、`seedance2.0_fast_vision`、`Seedance_2.0_mini` 和 `Seedance_2.5`。该列表仅用于指导用户选择和传入准确的 `--model` 值，不要在 skill 侧增加模型枚举校验，实际合法性由服务端决定。
6. 首尾帧请求固定传 `--generate-type 1`，并按首帧、尾帧顺序传入两次 `--image`，不得重排。用户未明确两张图片的角色或缺少任一张时，先询问用户；不要在 skill 侧维护额外的 `generate_type` 枚举 allowlist，其他值原样交给服务端处理。
7. `generate-video` 返回后，保存 `thread_id`、`run_id`，并立即向用户展示 `web_thread_link`。
8. 每隔 10 秒调用 `query-result`，直到 `completed=true`。出现 `error_message` 时停止并报告；成功后按“媒体交付完成标准”逐项交付 `videos[].output_path` 对应的视频文件。

```bash
pippit-tool-cli generate-video --prompt "用户原始描述" --model "Seedance_2.5"

pippit-tool-cli generate-video \
  --prompt "用户原始描述" \
  --image FIRST_FRAME_PATH \
  --image LAST_FRAME_PATH \
  --generate-type 1

pippit-tool-cli query-result \
  --thread-id THREAD_ID \
  --run-id RUN_ID \
  --download-dir OUTPUT_DIR
```

`--image` 最多重复 9 次，`--video` 和 `--audio` 最多各重复 3 次。

### 路由 C：视频超分和擦字幕

用户明确要求视频超分、提升视频清晰度、擦字幕或去字幕时，直接调用对应的 `pippit-tool-cli` 视频处理命令，不要改走 `pippit-tool-cli submit-run`：

- 视频超分、提升视频清晰度：`video-super-resolution`
- 擦字幕、去字幕：`erase-video-subtitle`

执行原则：

1. 执行前完成“前置要求”的CLI 安装检查，使用返回的 `cli_path`；失败时报告阻塞，不要悄悄降级到会话 API。
2. 真实提交会消耗 credits；如果用户本轮尚未明确确认处理，按“用户确认与反问”规则征得明确确认后再运行。
3. 把用户提供的本地视频路径和处理参数直接交给对应 CLI；缺少必填输入时先询问用户。
4. 命令返回后，保存 `thread_id`、`run_id`，并立即向用户展示 `web_thread_link`。
5. 每隔 10 秒调用 `query-result`，直到 `completed=true`。出现 `error_message` 时停止并报告；成功后按“媒体交付完成标准”逐项交付 `videos[].output_path` 对应的视频文件。

```bash
pippit-tool-cli video-super-resolution \
  --video VIDEO_PATH \
  --output-resolution OUTPUT_RESOLUTION

pippit-tool-cli erase-video-subtitle \
  --video VIDEO_PATH

pippit-tool-cli query-result \
  --thread-id THREAD_ID \
  --run-id RUN_ID \
  --download-dir OUTPUT_DIR
```

### 路由 D：小云雀后端 Agent 编排

需要意图确认、脚本/分镜拆解、MV、TVC、局部编辑、复杂参考素材编排，或者用户未明确要求模型直出的创作需求，使用 `pippit-tool-cli submit-run` 提交消息，再用 `get_thread.py` 查询会话进展；明确的首尾帧请求走路由 B，明确的视频超分和擦字幕请求走路由 C；积分余额查询走路由 E。

提交前完成“前置要求”的CLI 安装检查，使用返回的 `cli_path` 调用 `submit-run`；失败时报告版本或安装阻塞。

### 路由 E：积分余额查询

用户询问小云雀“积分余额”、“还剩多少积分”、“剩余 credits”或要求查询个人有效积分时，直接使用 `pippit-tool-cli get-credit-balance`。

执行原则：

1. 执行前完成“前置要求”的CLI 安装检查，使用返回的 `cli_path`；不可用或版本不支持该命令时报告阻塞，不要改走 `pippit-tool-cli submit-run`。
2. 使用当前 CLI 登录凭证或显式配置的 `XYQ_ACCESS_KEY` 查询凭证所属用户的个人有效积分余额；无需传入用户 ID、`thread_id` 或 `run_id`。鉴权要求见“前置要求”。
3. 这是只读查询，不需要积分消耗确认；不创建会话，不提交生成任务，也不调用 `get_thread.py` 或 `query-result` 轮询。
4. 成功时读取 JSON 中字符串类型的 `total_remain_amount`，向用户展示当前有效积分余额；`"0"` 是有效的零余额。查询失败或缺少余额字段时报告错误，不得当作零余额。
5. 需要排查请求时可添加 `--with-log-id`，同时保留返回的 `log_id`。该命令只返回总余额，不提供积分明细、到期时间或生成任务的费用预估。

```bash
pippit-tool-cli get-credit-balance

# 排查请求时同时返回 log_id
pippit-tool-cli get-credit-balance --with-log-id
```

默认输出示例：

```json
{"total_remain_amount":"123"}
```

## 用户确认与反问

后端返回意图确认问题、真实提交前需要 credits 确认，或缺少无法安全推断的必需信息时，暂停执行并向用户提问。

1. 优先使用当前 Agent 宿主提供的结构化用户提问或确认工具。
   - **Codex**：准确工具名是 `request_user_input`。仅在工具已暴露且当前模式允许时调用；不可用时退回普通聊天提问。不要在 Codex 中调用 `ask_user_question`。
   - **WorkBuddy**：准确工具名是 `ask_user_question`（Ask User Question）。需要用户补充、选择或确认时优先调用；工具未暴露时才退回普通聊天提问。
   - **Trae 及其他宿主**：先查看当前宿主实际暴露的工具，再使用同类结构化提问、确认或表单工具；不要臆造具体工具名。没有同类工具时退回普通聊天提问。
2. 涉及 credits 消耗、真实生成、外部提交或不可逆操作时，必须等待用户明确答复；不要默认同意或超时后继续。路由 E 的只读积分余额查询不需要额外确认。
3. 后端已经给出问题或选项时，保持原意传给用户，不要代替用户回答。
4. 当前宿主没有结构化提问工具，或当前模式不允许调用时，使用一条简洁的普通聊天问题并暂停。
5. 收到回复后，把用户答案原样发回同一 `thread_id`，获取新的 `run_id`，再继续轮询；不要新开会话。

## 功能

1. **创建会话 / 发消息** - 创建新会话或向已有会话发送一条消息（如「创作一个视频」）
2. **查询会话进展** - 根据 `thread_id` 、 `run_id`、`after_seq` 增量拉取该会话的消息列表，用于轮询创作过程的消息和最终产物结果
3. **上传文件** - 支持上传`单张图片`、`单个视频文件`或`单个mp3/wav音频文件`到小云雀资产库，得到文件对应的 `asset_id`（编辑或参考已有图片/视频/音频时需要先上传）
4. **下载结果** - 将会话中生成的图片/视频批量下载到本地，支持指定输出目录和文件名前缀。
5. **图片模型直出** - 使用 `pippit-tool-cli generate-image` 直接调用图片模型，使用 `query-result` 查询并下载图片结果。
6. **视频模型直出** - 使用 `pippit-tool-cli generate-video` 直接调用视频模型，使用 `query-result` 查询并下载视频结果。
7. **视频处理** - 使用 `pippit-tool-cli video-super-resolution` 或 `erase-video-subtitle` 处理本地视频，使用 `query-result` 查询并下载视频结果。
8. **积分余额查询** - 使用 `pippit-tool-cli get-credit-balance` 查询当前凭证所属用户的个人有效积分余额，直接展示 `total_remain_amount`。


## 前置要求

### 检查并按需安装 CLI

运行环境需要 Python 3、Node.js 16+，支持执行本地程序。首次安装或自动升级 CLI 时需要 npm、可写的用户缓存目录、访问 npm 源及 GitHub Release 的网络、`curl` 和解压工具（macOS/Linux 的 `tar`，Windows 的 PowerShell）。复用已有 CLI 不需要 npm 或下载网络；安装或升级缺少这些能力时，报告具体安装阻塞。

开始执行本技能任务时运行以下脚本。它先检查 PATH 中的 CLI，再检查自身的安装缓存；找到命令齐全的 CLI 就复用，不访问 npm 或下载二进制。两处均不存在 CLI，或已有 CLI 的必需命令帮助检查返回非零退出码时，获取 npm `latest` 并安装或升级。PATH 中的旧版本缺少命令但缓存可用时，直接复用缓存，不重复升级。同一任务内的提交、轮询、上传和下载复用返回路径。

```bash
node "{baseDir}/scripts/ensure-cli.js"
```

需要安装或升级时，脚本跳过 npm 生命周期脚本获取 `@pippit-dev/cli@latest`，再调用包内的 `scripts/install-cli.js` 只安装 CLI。新版本通过全部检查后保存在 `~/.cache/pippit-tool-cli/xyq-skill/<平台>-<架构>/current`，供后续任务复用。它不会安装、清理全局 Skill，也不要求全局 npm 写入权限。安装与检查日志写入 stderr，成功时 stdout 返回 JSON：

```json
{"cli_path":"/absolute/cache/path/current/node_modules/@pippit-dev/cli/bin/pippit-tool-cli","version":"实际安装版本"}
```

保存 `cli_path`，后续用它替换所有示例中的 `pippit-tool-cli`。不要依赖上一次 shell 调用中的临时环境变量；Windows PowerShell 用 `& "绝对路径" 参数` 调用。保留安装缓存以便后续任务复用；如果路径已被清理，重新运行安装脚本。

脚本验证 CLI 版本命令，以及 `login`、`submit-run`、`upload-file`、`download-result`、`query-result`、`generate-image`、`generate-video`、`video-super-resolution`、`erase-video-subtitle`、`get-credit-balance` 的 `--help`。检查不发送创作请求，也不需要凭据。已有 CLI 缺少必需命令时自动升级；版本命令无法运行或检查超时时报告运行错误。单次调用最多下载安装一次，最新版本仍不支持必需命令时停止并报告，不反复升级。升级成功前保留原安装；下载失败或最新包缺少只安装 CLI 的入口时，报告安装阻塞。

### 配置凭据

创建会话/发送消息、媒体上传、图片/视频模型直出、视频处理和积分余额查询（路由 A/B/C/D/E）使用原生 CLI。首次使用时运行网页登录，CLI 会自动申请或复用本机专属凭证，并保存到系统安全凭证库：

```bash
pippit-tool-cli login
```

`XYQ_ACCESS_KEY` 仅作为原生 CLI 在 CI、Agent 等非交互环境中的显式覆盖。如果该环境变量已经设置但无效，CLI 不会静默改用网页登录凭证，应先修正或取消该环境变量。

路由 D 使用 CLI 提交消息和上传素材；查询进展仍使用独立 Python 脚本 `get_thread.py`。该脚本尚未接入 CLI 的系统安全凭证库，使用前必须配置同一用户的凭证：

```bash
export XYQ_ACCESS_KEY="your-access-key"
```

原生 CLI 和保留的 Python 脚本携带用户密钥的 API 请求固定发往 `https://xyq.jianying.com`，不接受 `XYQ_OPENAPI_BASE` 或 `XYQ_BASE_URL` 覆盖。CLI 拒绝 API 跨域重定向，Python API 脚本禁止自动重定向；上传只通过 Authorization 请求头携带密钥。

所有 CLI 路由使用本次安装检查返回的 `cli_path`。保留的 Python 脚本仅使用标准库；安装检查脚本仅使用 Node.js 内置模块。

## 使用方法

### 1. 创建会话 / 发送消息

```bash
# 创建新会话并发送「生一个动漫视频」
pippit-tool-cli submit-run --message "生一个动漫视频"

# 向已有会话发送消息
pippit-tool-cli submit-run --message "再生成一个故事视频" --thread-id THREAD_ID

# 携带多个已上传的素材，每个 ID 重复一次参数
pippit-tool-cli submit-run --message "根据参考素材生成视频" --asset-ids ASSET_ID1 --asset-ids ASSET_ID2
```

`--message` 必填且不能全为空白，内容原样发送。`--thread-id` 可选；`--asset-ids` 每次接收一个 ID，多个素材必须重复该参数，不能在一次参数后以空格罗列多个 ID。

### 2. 查询会话进展

```bash
# 查询会话消息列表
python3 {baseDir}/scripts/get_thread.py --thread-id THREAD_ID --run-id RUN_ID --after-seq SEQUENCE
```

> `run_id` 由 `submit_run` 返回，用于指定查询某次具体运行的结果。

### 3. 上传文件

先完成“前置要求”的CLI 安装检查，再使用返回的 `cli_path` 调用 `upload-file`；缺少命令时报告版本或安装阻塞。该命令使用 CLI 登录凭证或显式设置的 `XYQ_ACCESS_KEY`，成功输出 `{"asset_id":"..."}`。

- 当用户提供了参考的文件地址时，先进行文件上传，仅支持图片、视频、`.mp3/.wav` 音频。
- 单次指令执行仅支持单个文件，多个文件可并行调用，单个文件必须小于 500 MB（500000000 字节，达到上限会拒绝上传）。

```bash
# 上传图片
pippit-tool-cli upload-file --path /path/to/image.png

# 上传视频
pippit-tool-cli upload-file --path /path/to/video.mp4

# 上传音频
pippit-tool-cli upload-file --path /path/to/audio.mp3
```

### 4. 下载结果

会话 API 路由从 `get_thread.py` 返回的 `messages` 中提取产物 URL，逐文件调用 `pippit-tool-cli download-result` 下载到本地。

- 输出目录沿用用户指定的目录，未指定时使用 `./xyq_output`。
- 保留原有命名规则：按产物 URL 列表顺序从 `01` 开始编号，有前缀时为 `前缀_01.ext`，无前缀时为 `01.ext`。扩展名优先取 URL 查询参数 `filename` 中的扩展名，其次取 URL 路径的扩展名，无法取得时使用 `.bin`。
- 将目录和文件名拼成完整的 `--output-path`；每个 URL 调用一次，可最多并行执行 5 个下载命令。重试时保持 URL 与目标路径的对应关系。

```bash
# 示例：输出目录 ./xyq_output，前缀 artifact，两个 URL 的扩展名分别为 .png 和 .mp4
pippit-tool-cli download-result --url "URL1" --output-path "./xyq_output/artifact_01.png"
pippit-tool-cli download-result --url "URL2" --output-path "./xyq_output/artifact_02.mp4"
```

CLI 默认跳过已存在的目标文件，返回 `already_exist`。仅在来源提供真实的文件更新时间时传入 `--updated-at`（Unix 秒），让 CLI 根据本地文件修改时间决定是否重新下载；不要用当前时间代替远端更新时间。跳过不代表已校验本地内容与远端一致，不能将已知属于其他产物的同名文件当作本次结果。

逐项收集下载结果；单项失败不阻断其他文件，只对失败项重试一次，仍失败则记录该产物、目标路径及 CLI 返回的错误。

下载成功或复用已有文件后，按“媒体交付完成标准”将真实图片/视频作为附件或可预览媒体逐项展示给用户；只下载到本地不代表已完成交付。

## 典型工作流

理解这些工作流，才能正确组合上面的 CLI 和脚本完成用户需求。

### 场景 1：用户要求生成图片或视频（非模型直出）

```
1. pippit-tool-cli submit-run --message "用户的描述"  →  拿到 thread_id、run_id 和 web_thread_link
2. **立即**将 `web_thread_link` 展示给用户（如"任务已提交，可在此查看：{web_thread_link}"）
3. 每隔 `10` 秒钟调用 get_thread.py --thread-id THREAD_ID --run-id RUN_ID --after-seq SEQUENCE 进行轮询
4. 检查 messages：
  - 当任务还在创作中：
    - 将过程创作信息展示给用户，继续轮询
  - 当任务完成（run 结束）：
    - 如果涉及意图确认/流程中断（如"请回答以下问题"）：
      → 优先调用当前宿主的结构化用户提问工具展示问题，等待用户回复
      → 使用 `thread_id` 重新提交任务（保持同一会话，产生新的 run_id）
      → 回到步骤 2 继续轮询（可能多轮，直到不再意图确认）
    - 如果 content 中包含产物 URL：
      → 信息展示 → 下载产物 → 逐项交付媒体附件
5. 自动下载：按“下载结果”的目录、前缀和编号规则，为每个产物 URL 调用 pippit-tool-cli download-result --url URL --output-path 完整文件路径
6. 按“媒体交付完成标准”逐项交付已下载或复用的图片/视频附件，并说明下载或交付失败的项目
```

### 场景 2：用户明确要求图片模型直出

```
1. 按“前置要求”检查并按需安装 CLI，后续使用返回的 cli_path
2. 检查图片模型：用户未提供时先询问，不要自行选择
3. pippit-tool-cli generate-image --prompt "用户原始描述" --model IMAGE_MODEL [仅添加用户已给出的其他参数]
4. 拿到 thread_id、run_id 和 web_thread_link，立即展示 web_thread_link
5. 每隔 10 秒调用 query-result --thread-id THREAD_ID --run-id RUN_ID --download-dir OUTPUT_DIR
6. completed=true 后按“媒体交付完成标准”逐项交付 images[].output_path 对应的图片文件；出现 error_message 时停止并报告
```

### 场景 3：用户明确要求视频模型直出（含首尾帧）

```
1. 按“前置要求”检查并按需安装 CLI，后续使用返回的 cli_path
2. 普通视频模型直出：pippit-tool-cli generate-video --prompt "用户原始描述" [仅添加用户已给出的其他参数]
3. 首尾帧直出：确认两张图片的首帧/尾帧角色，按顺序执行 generate-video --image FIRST_FRAME_PATH --image LAST_FRAME_PATH --generate-type 1
4. 拿到 thread_id、run_id 和 web_thread_link，立即展示 web_thread_link
5. 每隔 10 秒调用 query-result --thread-id THREAD_ID --run-id RUN_ID --download-dir OUTPUT_DIR
6. completed=true 后按“媒体交付完成标准”逐项交付 videos[].output_path 对应的视频文件；出现 error_message 时停止并报告
```

### 场景 4：用户提供图片/视频/音频要求编辑修改或作为参考（如"参考这个视频做一个新的"、"用这首歌做MV"）

```
1. pippit-tool-cli upload-file --path /path/to/video.mp4  →  拿到 asset_id1
2. pippit-tool-cli upload-file --path /path/to/audio.mp3  →  拿到 asset_id2
3. pippit-tool-cli submit-run --message "参考这个视频并用这首歌做一个新的" --asset-ids asset_id1 --asset-ids asset_id2  →  拿到 thread_id、run_id、web_thread_link
4. 后续同场景 1 的步骤 2-6
```

用户给了文件路径 + 编辑指令 = 先上传文件，再把编辑指令和 所有asset_id 一起发送。

### 场景 5：用户提供参考图/视频/音频要求生成新内容

```
1. pippit-tool-cli upload-file --path /path/to/ref1.png  →  拿到 asset_id1
2. pippit-tool-cli upload-file --path /path/to/ref2.mp4  →  拿到 asset_id2
3. pippit-tool-cli upload-file --path /path/to/ref3.mp3  →  拿到 asset_id3
4. 直到所有文件上传完成，拿到所有 asset_id
5. pippit-tool-cli submit-run --message "根据参考图、视频、音频生成xxx" --asset-ids asset_id1 --asset-ids asset_id2 --asset-ids asset_id3  →  拿到 thread_id、run_id、web_thread_link
6. 后续同场景 1 的步骤 2-6
```

### 场景 6：在已有会话中追加新需求

```
1. pippit-tool-cli submit-run --message "新的描述" --thread-id THREAD_ID  →  拿到 thread_id、run_id、web_thread_link
2. 后续同场景 1 的步骤 2-6
```

### 场景 7：用户要求视频超分或擦字幕

```
1. 按“前置要求”检查并按需安装 CLI，后续使用返回的 cli_path
2. 根据用户意图调用 video-super-resolution 或 erase-video-subtitle，并传入用户提供的本地视频路径和处理参数
3. 拿到 thread_id、run_id 和 web_thread_link，立即展示 web_thread_link
4. 每隔 10 秒调用 query-result --thread-id THREAD_ID --run-id RUN_ID --download-dir OUTPUT_DIR
5. completed=true 后按“媒体交付完成标准”逐项交付 videos[].output_path 对应的视频文件；出现 error_message 时停止并报告
```

### 轮询策略

- **间隔**：每 10 秒查询一次
- **增量拉取**：首次用 --after-seq 0，后续根据messages消息列表长度，计算新的 seq 值
- **完成判断**：当创作任务完成且messages的content中包含产物结果 URL（图片/视频地址）
- **超时**：连续轮询 `48 小时`仍无结果，告知用户"生成时间较长，可稍后查看"，不再继续轮询
- **错误重试**：单次查询失败可重试 1 次，连续 3 次失败则停止并告知用户

## 输出格式

**pippit-tool-cli submit-run** 返回：
```json
{
  "thread_id": "90f05e0c-...",
  "run_id": "abc123-...",
  "web_thread_link": "https://xyq.jianying.com/..."
}
```

**get_thread** 返回：
```json
{
  "messages": [
    {"id": "1", "role": "user", "content": "生一个动漫视频"},
    {"id": "2", "role": "assistant", "content": [
      {
        "type": "{type}",
        "subtype": "{sub_type}",
        "data": {...}
      }
    ]},
    {"id": "3", "role": "assistant", "content": [
      {
        "type": "{type}",
        "subtype": "{sub_type}",
        "data": {..., "url": "{url}"....}
      }
    ]}
  ]
}
```

**pippit-tool-cli upload-file** 返回：
```json
{
  "asset_id": "{asset_id}"
}
```

**pippit-tool-cli download-result** 每次下载一个文件，成功返回：
```json
{
  "output_path": "./xyq_output/artifact_01.png",
  "downloaded": ["./xyq_output/artifact_01.png"]
}
```

目标文件已存在而跳过时返回：
```json
{
  "output_path": "./xyq_output/artifact_01.png",
  "downloaded": null,
  "already_exist": ["./xyq_output/artifact_01.png"]
}
```

单文件下载失败时，命令以非零退出码返回错误，不保证输出 JSON；不能只检查 JSON 中是否有 `errors` 来判断成功。由用户侧 Agent 汇总各次调用的 `downloaded`、`already_exist` 和失败项，不再依赖批量返回的 `output_dir`、`total`。

## 媒体交付完成标准

- 本标准适用于会话 API、图片/视频模型直出及视频处理路由。会话 run 结束后，先处理意图确认或流程中断；收到最终产物后再进入下载和交付。
- 确认每个待交付产物已下载，或可明确复用对应的已有文件；检查本地文件存在且非空。已存在而跳过的文件不能计为本次新下载。
- 使用当前宿主提供的文件交付工具，将每个真实图片/视频文件作为附件或可预览媒体展示在回复中；宿主通过内置媒体渲染能力交付时，按其规定的方式引用文件。不能只发送 URL、文件路径或文件列表来代替媒体附件。
- 只有所有待交付媒体均经宿主交付成功后，才能宣称“交付完成”；生成成功、下载成功和附件交付成功须分别判断，不能仅凭下载成功就宣称用户已收到媒体。
- 下载失败、附件交付失败或宿主不支持媒体交付时，明确说明未交付的项目及原因；仍应交付其他可用媒体。原始产物链接和本地路径可作为补充信息提供，但不能据此宣称全部交付完成。

## 向用户展示内容

- 任务提交后：立即将 `web_thread_link` 展示给用户，方便用户直接打开浏览器查看任务页面
- 任务在创作中：
  - 展示过程中的创作信息等，继续轮询
- 任务完成（run 结束）：
  - 若涉及意图确认/流程中断（如"请回答以下问题"）→ 按“用户确认与反问”规则优先调用结构化提问工具 → 等待用户回复 → 使用同一 `thread_id` 重新提交任务 → 继续轮询（可能多轮）
  - 若 content 中包含产物 URL：下载对应文件，并按“媒体交付完成标准”逐项将真实图片/视频作为附件或可预览媒体展示给用户；产物链接和本地路径仅作补充，不能替代附件交付。存在未交付项时明确说明。

## 核心原则：用户侧不做创作，只做传话

你（用户侧 Agent）的职责是**搬运工**，不是创作者。会话 API 路由由后端 Agent 负责理解需求、拆解分镜、编排工作流、选模型、写 prompt；图片/视频模型直出和视频处理路由把用户原始参数传给 CLI。积分余额查询按路由 E 直接查询并展示余额；以下步骤适用于创作和视频处理任务：

1. **准备素材**：会话 API 路由用 `pippit-tool-cli upload-file` 把本地文件转为 asset_id；图片/视频模型直出和视频处理路由把本地路径直接交给对应 CLI；首尾帧任务固定传 `--generate-type 1` 并保持首帧、尾帧顺序
2. **提交任务**：先按“执行路由”判断；图片模型直出调用 `pippit-tool-cli generate-image`，视频模型直出调用 `pippit-tool-cli generate-video`，视频超分和擦字幕调用对应的视频处理命令，其余通用创作任务把用户的原始描述 + asset_id 原封不动发给 `pippit-tool-cli submit-run`
3. **传话**：根据 `get_thread.py` 返回的消息列表，展示过程中的意图询问、创作信息等
4. **取件**：会话 API 路由用 `get_thread.py` 轮询，图片/视频模型直出和视频处理路由用 `query-result` 轮询 → 检查结果 → 下载产物 → 通过宿主交付真实媒体附件

**绝对不要做的事：**
- 不要替用户扩写、润色、翻译 prompt（用户说"帮我推演分镜"，就直接传"帮我推演分镜"，不要自己先写个分镜表再逐条发）
- 不要自行编排镜头描述、剧情推演、风格分析
- 不要在消息中添加自己编的 prompt（如"超写实风格，电影级光影，8K分辨率"之类的描述词）

后端 Agent 对模型能力、参数配置、prompt 工程远比用户侧更专业。用户侧越俎代庖只会降低生成质量，换个弱模型更是灾难。

**正确示例：**
```
用户说：「根据多张参考图，做个科普故事视频」
用户给了参考图：/path/to/ref1.png, /path/to/ref2.png, /path/to/ref3.png

→ pippit-tool-cli upload-file --path /path/to/ref1.png →  拿到 asset_id1
→ pippit-tool-cli upload-file --path /path/to/ref2.png →  拿到 asset_id2
→ pippit-tool-cli upload-file --path /path/to/ref3.png →  拿到 asset_id3
→ pippit-tool-cli submit-run --message "根据参考图、视频生成xxx" --asset-ids asset_id1 --asset-ids asset_id2 --asset-ids asset_id3  →  拿到 web_thread_link，立即展示给用户
→ 轮询 ─┬─ 意图确认 → 用户确认 → 使用 thread_id 重新提交 → 继续轮询
        └─ 无意图确认 → 信息展示 → 下载产物 → 逐项交付媒体附件
```

**错误示例：**
```
❌ 用户侧自己先写了个九宫格分镜表（对峙、交锋、危机...）
❌ 然后把自己编的描述发给后端
❌ 或者拆成9次 submit_run 分别发送
```

## 注意事项

- 独立 Python 会话 API 脚本的鉴权方式为请求头 `Authorization: Bearer <XYQ_ACCESS_KEY>`
- 创建会话时 `message` 是用户的指令要求，不能为空
- 查询会话时可用 --after-seq 做增量拉取，便于轮询新消息（含 assistant 回复与生图/生视频结果）
- 上传文件仅支持图片（image/*）、视频（video/*）和 `.mp3/.wav` 音频文件，其他类型会被拒绝，文件必须小于 500 MB（500000000 字节）
- 生成过程中将创作信息展示给用户；任务完成后，必须通过宿主的文件交付或媒体渲染能力，将**真实图片/视频作为附件或可预览媒体逐项展示**。不能只回复产物 URL 或本地文件路径；交付失败时如实说明。
- 图片/视频模型直出和视频处理任务必须保留 CLI 返回的 `thread_id` / `run_id`，并用 `query-result` 取回最终图片或视频。
