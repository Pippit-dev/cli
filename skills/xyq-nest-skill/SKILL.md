---
name: xyq-skill
description: 通过小云雀的 AI 能力进行综合创作，支持生成和编辑图片/视频，并在用户明确要求图片或视频模型直出、指定图片或视频模型或直接调用 CLI 时使用 pippit-tool-cli generate-image / generate-video；用户要求视频超分、提升视频清晰度、擦字幕或去字幕时，使用 video-super-resolution / erase-video-subtitle。覆盖文生图、文生视频、图生视频、首尾帧生视频、视频编辑、风格转换、视频续写、视频复刻、TVC、宣传片、音乐 MV、产品广告、分镜和教育短视频等场景。当用户提到小云雀、xyq、上传参考图/视频/mp3或wav音频、查看生成进度，或查询小云雀积分余额、剩余积分、credits 时也应触发；积分查询使用 pippit-tool-cli get-credit-balance。短剧生成、续写、改写、人物设定和分集创作应使用 xyq-short-drama-skill，不在本技能中执行。
user-invocable: true
metadata:
  {
    "openclaw":
      {
        "emoji": "💬",
        "requires":
          {
            "bins": ["python3", "pippit-tool-cli"],
            "env": ["XYQ_ACCESS_KEY"]
          },
        "primaryEnv": "XYQ_ACCESS_KEY"
      }
  }
---

# 小云雀创作、图片/视频模型直出、视频处理与积分查询

通过 小云雀的API 创建会话、发送消息（生图、生视频、编辑视频等）、上传图片/视频/mp3或wav音频文件，并查询会话消息进展；通过 CLI 查询个人有效积分余额。

小云雀是一个 AI 综合创作平台，同时为人类创作者和 Agent 设计。Agent 通过 Skill 入口理解任务、调用模型并自动编排工作流。

**平台核心能力：**
- **生成**：文生图、文生视频、图生视频、视频续写
- **编辑**：局部修改、元素替换、镜头调整、风格迁移
- **视频处理**：视频超分、提升视频清晰度、擦字幕
- **复杂创作**：复刻已有视频风格做 TVC/宣传片、用音乐生成 MV、产品展示片制作

除“图片/视频模型直出”和“视频超分/擦字幕”外，创作和编辑需求通过发送自然语言消息来完成，后端 Agent 会自主编排工作流。复杂任务耗时较长，需耐心轮询。

## 执行路由（必须先判断）

积分余额查询优先走路由 F，不进入创作、视频处理或短剧工作流。

### 路由 A：图片模型直出

满足任一条件时，必须直接使用 `pippit-tool-cli generate-image`，不要改走 `pippit-tool-cli submit-run`：

- 用户明确说“图片模型直出”、“直接调图片模型”或明确要求用 CLI 生图。
- 用户指定了具体图片模型（如 `seedream_5.0_pro`），并希望单次直接生成图片。
- 上游流程已明确将任务标记为图片 direct-model / 模型直出。

执行原则：

1. 执行前用 `command -v pippit-tool-cli` 确认 CLI 可用；不可用时报告阻塞，不要悄悄降级到会话 API。
2. 真实提交会消耗 credits；如果用户本轮尚未明确确认生成，按“用户确认与反问”规则征得明确确认后再运行。
3. 保留用户原始 prompt，不要自行扩写、润色、翻译或增加风格词。
4. `--model` 必填；用户未提供图片模型时，先询问使用哪个模型。只添加用户已经给出的 `--ratio`、`--resolution`、`--generate-image-count`、`--image` 参数，不补默认值。
5. `--resolution` 的使用说明是：仅 `seedream_5.0_pro` 支持 `1K`、`2K`、`4K`。不要在 skill 侧维护额外 allowlist 或自行改写用户值，实际合法性由服务端决定。
6. `generate-image` 返回后，保存 `thread_id`、`run_id`，并立即向用户展示 `web_thread_link`。
7. 每隔 10 秒调用 `query-result`，直到 `completed=true`。出现 `error_message` 时停止并报告；成功时展示并下载 `images[].output_path`。

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

1. 执行前用 `command -v pippit-tool-cli` 确认 CLI 可用；不可用时报告阻塞，不要悄悄降级到会话 API。
2. 真实提交会消耗 credits；如果用户本轮尚未明确确认生成，按“用户确认与反问”规则征得明确确认后再运行。
3. 保留用户原始 prompt，不要自行扩写、润色、翻译或增加风格词。
4. 只添加用户已经给出的 `--model`、`--duration`、`--ratio`、`--resolution`、`--image`、`--video`、`--audio`、`--generate-type` 参数；未给参数交给 CLI 默认值。
5. 普通用户支持模型 `Seedance_2.0_mini_lite`；VIP 专属模型包括 `seedance2.0_vision`、`seedance2.0_fast_vision`、`Seedance_2.0_mini` 和 `Seedance_2.5`。该列表仅用于指导用户选择和传入准确的 `--model` 值，不要在 skill 侧增加模型枚举校验，实际合法性由服务端决定。
6. 首尾帧请求固定传 `--generate-type 1`，并按首帧、尾帧顺序传入两次 `--image`，不得重排。用户未明确两张图片的角色或缺少任一张时，先询问用户；不要在 skill 侧维护额外的 `generate_type` 枚举 allowlist，其他值原样交给服务端处理。
7. `generate-video` 返回后，保存 `thread_id`、`run_id`，并立即向用户展示 `web_thread_link`。
8. 每隔 10 秒调用 `query-result`，直到 `completed=true`。出现 `error_message` 时停止并报告；成功时展示并下载 `videos[].output_path`。

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

1. 执行前用 `command -v pippit-tool-cli` 确认 CLI 可用；不可用时报告阻塞，不要悄悄降级到会话 API。
2. 真实提交会消耗 credits；如果用户本轮尚未明确确认处理，按“用户确认与反问”规则征得明确确认后再运行。
3. 把用户提供的本地视频路径和处理参数直接交给对应 CLI；缺少必填输入时先询问用户。
4. 命令返回后，保存 `thread_id`、`run_id`，并立即向用户展示 `web_thread_link`。
5. 每隔 10 秒调用 `query-result`，直到 `completed=true`。出现 `error_message` 时停止并报告；成功时展示并下载 `videos[].output_path`。

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

需要意图确认、脚本/分镜拆解、MV、TVC、局部编辑、复杂参考素材编排，或者用户未明确要求模型直出的创作需求，使用 `pippit-tool-cli submit-run` 提交消息，再用 `get_thread.py` 查询会话进展；明确的首尾帧请求走路由 B，明确的视频超分和擦字幕请求走路由 C；积分余额查询走路由 F。

提交前运行 `command -v pippit-tool-cli` 和 `pippit-tool-cli submit-run --help` 确认 CLI 及命令可用；不可用时报告版本或安装阻塞。

### 路由 E：短剧工作流

用户要求短剧生成、续写、改写、剧情扩展、人物设定、分集草稿或短剧会话文件处理时，停止本技能流程并转交 `xyq-short-drama-skill`，不要用 `pippit-tool-cli submit-run`、`generate-image` 或 `generate-video` 假装执行完整短剧流程。

### 路由 F：积分余额查询

用户询问小云雀“积分余额”、“还剩多少积分”、“剩余 credits”或要求查询个人有效积分时，直接使用 `pippit-tool-cli get-credit-balance`。

执行原则：

1. 执行前用 `command -v pippit-tool-cli` 确认 CLI 可用；不可用或版本不支持该命令时报告阻塞，不要改走 `pippit-tool-cli submit-run`。
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
2. 涉及 credits 消耗、真实生成、外部提交或不可逆操作时，必须等待用户明确答复；不要默认同意或超时后继续。路由 F 的只读积分余额查询不需要额外确认。
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

创建会话/发送消息、媒体上传、图片/视频模型直出、视频处理和积分余额查询（路由 A/B/C/D/F）使用原生 CLI。首次使用时运行网页登录，CLI 会自动申请或复用本机专属凭证，并保存到系统安全凭证库：

```bash
pippit-tool-cli login
```

`XYQ_ACCESS_KEY` 仅作为原生 CLI 在 CI、Agent 等非交互环境中的显式覆盖。如果该环境变量已经设置但无效，CLI 不会静默改用网页登录凭证，应先修正或取消该环境变量。

路由 D 使用 CLI 提交消息和上传素材；查询进展仍使用独立 Python 脚本 `get_thread.py`。该脚本尚未接入 CLI 的系统安全凭证库，使用前必须配置同一用户的凭证：

```bash
export XYQ_ACCESS_KEY="your-access-key"
```

原生 CLI 和保留的 Python 脚本携带用户密钥的 API 请求固定发往 `https://xyq.jianying.com`，不接受 `XYQ_OPENAPI_BASE` 或 `XYQ_BASE_URL` 覆盖。CLI 拒绝 API 跨域重定向，Python API 脚本禁止自动重定向；上传只通过 Authorization 请求头携带密钥。

所有 CLI 路由要求 `pippit-tool-cli` 在 `PATH` 中可用且支持对应命令。保留的 Python 脚本仅使用标准库。

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

先用 `pippit-tool-cli upload-file --help` 确认当前 CLI 支持此命令；缺少命令时报告版本或安装阻塞。该命令使用 CLI 登录凭证或显式设置的 `XYQ_ACCESS_KEY`，成功输出 `{"asset_id":"..."}`。

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
      → 信息展示 → 下载产物 → 结果展示
5. 自动下载：按“下载结果”的目录、前缀和编号规则，为每个产物 URL 调用 pippit-tool-cli download-result --url URL --output-path 完整文件路径
6. 汇总每次调用的下载成功、已存在跳过和失败结果，向用户展示产物链接及对应的本地文件
```

### 场景 2：用户明确要求图片模型直出

```
1. command -v pippit-tool-cli  →  确认 CLI 可用
2. 检查图片模型：用户未提供时先询问，不要自行选择
3. pippit-tool-cli generate-image --prompt "用户原始描述" --model IMAGE_MODEL [仅添加用户已给出的其他参数]
4. 拿到 thread_id、run_id 和 web_thread_link，立即展示 web_thread_link
5. 每隔 10 秒调用 query-result --thread-id THREAD_ID --run-id RUN_ID --download-dir OUTPUT_DIR
6. completed=true 后展示并下载 images[].output_path；出现 error_message 时停止并报告
```

### 场景 3：用户明确要求视频模型直出（含首尾帧）

```
1. command -v pippit-tool-cli  →  确认 CLI 可用
2. 普通视频模型直出：pippit-tool-cli generate-video --prompt "用户原始描述" [仅添加用户已给出的其他参数]
3. 首尾帧直出：确认两张图片的首帧/尾帧角色，按顺序执行 generate-video --image FIRST_FRAME_PATH --image LAST_FRAME_PATH --generate-type 1
4. 拿到 thread_id、run_id 和 web_thread_link，立即展示 web_thread_link
5. 每隔 10 秒调用 query-result --thread-id THREAD_ID --run-id RUN_ID --download-dir OUTPUT_DIR
6. completed=true 后展示并下载 videos[].output_path；出现 error_message 时停止并报告
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
1. command -v pippit-tool-cli  →  确认 CLI 可用
2. 根据用户意图调用 video-super-resolution 或 erase-video-subtitle，并传入用户提供的本地视频路径和处理参数
3. 拿到 thread_id、run_id 和 web_thread_link，立即展示 web_thread_link
4. 每隔 10 秒调用 query-result --thread-id THREAD_ID --run-id RUN_ID --download-dir OUTPUT_DIR
5. completed=true 后展示并下载 videos[].output_path；出现 error_message 时停止并报告
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

## 会话 API 路由的下载完成标准

- run 结束后，先处理意图确认或流程中断；收到产物 URL 后才进入下载交付。
- 每个待交付产物都要有对应结果：本次下载成功、已存在而跳过，或下载失败。只有所有产物均已下载或明确复用已有文件时，才能报告本地交付完成。
- 已存在跳过的文件须单独说明，不能计为本次新下载；仍有失败项时报告“生成已完成，部分产物下载失败”，列出失败项和原始产物链接，不宣称全部下载完成。

## 向用户展示内容

- 任务提交后：立即将 `web_thread_link` 展示给用户，方便用户直接打开浏览器查看任务页面
- 任务在创作中：
  - 展示过程中的创作信息等，继续轮询
- 任务完成（run 结束）：
  - 若涉及意图确认/流程中断（如"请回答以下问题"）→ 按“用户确认与反问”规则优先调用结构化提问工具 → 等待用户回复 → 使用同一 `thread_id` 重新提交任务 → 继续轮询（可能多轮）
  - 若 content 中包含产物 URL：展示来自 `get_thread` 返回的 `messages` 的产物链接，以及对应本地文件的可点击绝对路径；区分本次下载、已存在跳过和下载失败，并按上述完成标准说明交付状态。

## 核心原则：用户侧不做创作，只做传话

你（用户侧 Agent）的职责是**搬运工**，不是创作者。会话 API 路由由后端 Agent 负责理解需求、拆解分镜、编排工作流、选模型、写 prompt；图片/视频模型直出和视频处理路由把用户原始参数传给 CLI。积分余额查询按路由 F 直接查询并展示余额；以下步骤适用于创作和视频处理任务：

1. **准备素材**：会话 API 路由用 `pippit-tool-cli upload-file` 把本地文件转为 asset_id；图片/视频模型直出和视频处理路由把本地路径直接交给对应 CLI；首尾帧任务固定传 `--generate-type 1` 并保持首帧、尾帧顺序
2. **提交任务**：先按“执行路由”判断；图片模型直出调用 `pippit-tool-cli generate-image`，视频模型直出调用 `pippit-tool-cli generate-video`，视频超分和擦字幕调用对应的视频处理命令，其余通用创作任务把用户的原始描述 + asset_id 原封不动发给 `pippit-tool-cli submit-run`
3. **传话**：根据 `get_thread.py` 返回的消息列表，展示过程中的意图询问、创作信息等
4. **取件**：会话 API 路由用 `get_thread.py` 轮询，图片/视频模型直出和视频处理路由用 `query-result` 轮询 → 检查结果 → 下载产物 → 结果展示给用户

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
        └─ 无意图确认 → 信息展示 → 下载产物 → 结果展示
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
- 生成过程中将过程中的创作信息展示给用户；任务完成后给出**产物结果（图片/视频）URL链接**和下载的**本地文件列表**。
- 图片/视频模型直出和视频处理任务必须保留 CLI 返回的 `thread_id` / `run_id`，并用 `query-result` 取回最终图片或视频。
