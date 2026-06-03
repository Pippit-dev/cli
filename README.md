# pippit-tool-cli

面向 Pippit / 小云雀工作流的命令行工具与智能体技能集合。

## 技能列表

本仓库在 `skills/` 目录下包含两个智能体技能：

| 技能 | 说明 | 路径 |
|-------|-------------|------|
| `pippit-short-drama-skill` | 短剧工作流技能，支持提交创作任务、上传参考文件、查询进度、列出会话文件和下载产物。 | `skills/short-drama/` |
| `xyq-nest-skill` | 通用 NestAgent 技能，支持图片/视频生成、编辑、文件上传、进度查询和结果下载。 | `skills/xyq-nest-skill/` |

## 通用 NestAgent 技能

`xyq-nest-skill` 通过接入小云雀 NestAgent 的综合创作能力，实现 AI 图片/视频生成、编辑、风格转换、文件上传、进度查询和结果下载。

### 功能特性

| 功能 | 说明 |
|------|------|
| 创建会话 / 发送消息 | 向小云雀发送自然语言指令，生成图片或视频。 |
| 查询会话进展 | 增量拉取会话消息，轮询创作进度和产物结果。 |
| 上传文件 | 上传图片/视频到小云雀资产库，获取 `asset_id` 用于编辑和参考。 |
| 下载结果 | 批量下载生成的图片/视频到本地，支持并行下载。 |

小云雀平台能力覆盖：

- 生成：文生图、文生视频、图生视频、视频续写。
- 编辑：局部修改、元素替换、镜头调整、风格迁移。
- 复杂创作：一句话生成短剧、复刻视频/TVC/宣传片、音乐 MV 生成、产品展示片制作。

### 配置

所有 `xyq-nest-skill` 脚本都使用 Bearer 令牌鉴权：

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
```

仅支持 `image/*` 和 `video/*` 类型，单文件大小限制 200 MB。

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
export XYQ_ACCESS_KEY="<access-key>"
pippit-tool-cli --version
pippit-tool-cli short-drama +submit-run --message "写一个赛博朋克短剧开头"
pippit-tool-cli short-drama +upload-file --path ./reference.doc
pippit-tool-cli short-drama +get-thread --thread-id thread_123 --run-id run_456
pippit-tool-cli short-drama +download-result --output-path ./thread_123/results/result.mp4 --url URL --updated-at 1779716734
```

`+submit-run`: 输出 `thread_id`、`run_id` 和 `web_thread_link`；其中 `--message` 为必填参数。
`+get-thread`: 请求中带 `version=v2`，并输出 `readable_text`。
`+upload-file`: 输出返回的 `asset_id`。 当前仅支持 `.doc`、`.docx` 和 `.txt` 文件。
`+download-result`: 会把结果 URL 下载到 `--output-path` 指定的文件路径；传入 `--updated-at` 后，如果本地文件早于该时间戳会覆盖更新，否则跳过。

短剧命令的错误日志会追加写入本地每日日志文件：`~/.pippit_tool_cli/logs/yyyy-mm-dd.log`。日志路径会基于当前用户主目录和系统路径分隔符生成，因此可在 macOS、Linux 和 Windows 上使用。

## HTTP 客户端

命令模块通过 `common.Runner` 发起服务调用。运行时配置，例如基础地址、HTTP 超时时间和接口路径，由 `internal/config` 加载，并在运行器中与 `common.Client` 组合使用。

## 鉴权

`short-drama +submit-run`、`short-drama +get-thread`、`short-drama +upload-file` 以及 `xyq-nest-skill` Python 脚本都使用 `Authorization: Bearer <XYQ_ACCESS_KEY>` 鉴权。OAuth 命令代码仍保留在仓库中，但短剧运行时请求不使用 OAuth。
