---
name: pippit-short-drama-skill
description: 使用 pippit-cli 的短剧场景能力提交和查询短剧创作任务。覆盖短剧生成、续写、改写、剧情扩展、人物设定、分集草稿、世界观设定、会话文件获取、文件资源下载等创作场景。当用户要求创作短剧、写短剧剧本、续写故事、修改剧情、补充角色设定、查询短剧任务进展、获取短剧会话文件或下载短剧文件资源，或提到 pippit-cli short-drama / 小云雀短剧时触发。
user-invocable: true
metadata:
  {
    "openclaw":
      {
        "emoji": "📖",
        "requires":
          {
            "bins": ["pippit-cli"]
          }
      }
  }
---

# 小云雀短剧创作

通过 `pippit-cli short-drama` 命令提交短剧创作任务、上传参考文件，并查询任务进展，获取会话产物文件。

短剧场景面向剧情、人物、分集与画面化叙事创作，用户的原始需求通过 `--message` 发送给后端 Agent。后端 Agent 负责理解任务、编排流程和生成内容；用户侧 Agent 只负责提交任务、查询进展和展示结果。

## 功能

1. **提交短剧 Run 任务** - 创建新会话或向已有会话发送短剧创作需求。
2. **查询会话进展** - 根据 `thread_id`、`run_id`、`after_seq` 拉取短剧任务消息列表。
3. **上传文件** - 上传短剧相关参考文件，得到文件 ID，供后续任务引用。
4. **获取会话文件** - 根据 `thread_id` 拉取会话文件列表，得到 `file_path`、`file_name`、`download_url`。
5. **下载文件资源** - 使用文件列表中的 `download_url` 下载资源，并按 `file_path` 写入用户本地目标文件路径。

## 前置要求

需要已安装 `pippit-cli`：

```bash
npx @pippit-dev/cli@latest install
```

## 使用方法

### 1. 提交短剧任务

```bash
# 创建新会话并提交短剧创作需求
pippit-cli short-drama +submit-run --message "创作一个赛博朋克短剧开头"

# 向已有会话追加新的短剧需求
pippit-cli short-drama +submit-run --message "继续写下一集，重点描写主角的逃亡" --thread-id THREAD_ID

# 携带已上传文件 ID 提交任务
pippit-cli short-drama +submit-run --message "参考这个大纲写第一集" --asset-ids ASSET_ID
```

### 2. 查询短剧任务进展

```bash
# 查询会话消息列表
pippit-cli short-drama +get-thread --thread-id THREAD_ID --run-id RUN_ID --after-seq 0
```

> `thread_id` 和 `run_id` 由 `+submit-run` 返回。`after-seq` 用于增量拉取消息，首次查询可使用 `0`。

### 3. 上传文件

当用户提供短剧大纲、人物设定、世界观设定、已有分集或剧本等本地文件路径时，可先上传文件。

```bash
pippit-cli short-drama +upload-file --path /path/to/outline.md
```

### 4. 获取会话文件

```bash
# 获取会话文件列表
pippit-cli short-drama +list-thread-file --thread-id THREAD_ID --page-num 1 --page-size 100
```

`+list-thread-file` 返回的每个文件对象包含：

```json
{
  "file_path": "./{thread-id}/路径/文件名", // 文件完整路径，包含文件名
  "download_url": "https://..." // URL
}
```

`+list-thread-file` 只负责获取会话文件列表，不负责下载文件。

### 5. 下载文件资源

```bash
# 下载文件资源到指定文件路径
pippit-cli short-drama +download-result --url DOWNLOAD_URL --output-path FILE_PATH
```

`FILE_PATH` 必须直接使用 `+list-thread-file` 返回的完整 `file_path`，包含文件名，不要取父目录。`+download-result` 负责把会话产生的文件通过 URL 下载到该目标文件路径；如果目标文件已存在，跳过下载。

## 典型工作流

### 场景 1：用户要求生成短剧内容

```
1. pippit-cli short-drama +submit-run --message "用户的原始短剧需求"
   → 拿到 thread_id、run_id 和 web_thread_link
2. 立即将 web_thread_link 展示给用户
3. 并行发起：
   a. pippit-cli short-drama +get-thread --thread-id THREAD_ID --run-id RUN_ID --after-seq SEQUENCE
   b. pippit-cli short-drama +list-thread-file --thread-id THREAD_ID --page-num 1 --page-size 100
4. 检查 `get-thread` 返回的 messages：
   - 如果任务仍在进行中：展示过程消息，继续查询
   - 如果后端 Agent 提出问题：展示问题，等待用户回复
5. 解析 `list-thread-file` 返回的 files，只获取文件元信息：
   - 对每个文件取 file_path、file_name、download_url
   - 将 file_path 作为本地目标文件路径，包含文件名
   - 如果 file_path 已存在：跳过下载
6. 对缺失的本地文件，调用 +download-result 并行下载资源：
   - 使用第 5 步获取的 download_url 作为 --url
   - 使用第 5 步获取的完整 file_path 作为 --output-path
7. 如用户继续追加需求，使用同一 thread_id 再次 submit-run
```

### 场景 2：用户提供参考文件要求创作

```
1. pippit-cli short-drama +upload-file --path /path/to/file
   → 拿到 file_id
2. pippit-cli short-drama +submit-run --message "用户的原始短剧需求" --asset-ids file_id
   → 拿到 thread_id、run_id 和 web_thread_link
3. 后续同场景 1 的并行查询和文件下载流程
```

### 场景 3：在已有短剧会话中续写或修改

```
1. pippit-cli short-drama +submit-run --message "用户的新需求" --thread-id THREAD_ID
   → 拿到新的 run_id 和 web_thread_link
2. 继续按场景 1 展示进展、处理用户补充问题、获取新增会话文件列表，并按需下载新增文件资源
```

## 轮询策略

- **间隔**：每 10 秒查询一次。
- **增量拉取**：首次使用 `--after-seq 0`，后续根据已读消息进度调整 `after-seq`。
- **并行查询**：每次 `+submit-run` 返回 `thread_id` 后，同时发起 `+get-thread` 和 `+list-thread-file`；会话信息展示流程保持不变，`+list-thread-file` 只用于获取文件元信息。
- **文件下载**：解析 `+list-thread-file` 的结果后，只有目标本地文件不存在时才调用 `+download-result` 下载资源。
- **用户确认**：如果消息中出现需要用户确认、补充设定或回答问题的内容，先展示给用户，等待用户回复。
- **超时**：如果长时间无结果，告知用户任务仍在生成中，可稍后通过 `web_thread_link` 查看。
- **错误处理**：单次查询失败可重试；连续失败时停止轮询并向用户说明错误。

## 输出格式

**+submit-run** 返回：

```json
{
  "thread_id": "thread_...",
  "run_id": "run_...",
  "web_thread_link": "https://xyq.jianying.com/..."
}
```

**+get-thread** 返回：

```json
{
  "messages": [
    {
      "id": "message_...",
      "role": "assistant",
      "content": [
        {
          "type": "text",
          "data": {}
        }
      ]
    }
  ]
}
```

**+upload-file** 返回：

```json
{
  "scene": "short-drama",
  "file_id": "file_...",
  "status": "uploaded",
  "uploaded_at": "2026-05-19T00:00:00Z",
  "request": {
    "path": "/path/to/file",
    "file_name": "file.md"
  }
}
```

**+list-thread-file** 返回：

```json
{
  "files": [
    {
      "file_path": "./{thread-id}/{file_path}/{file_name}",
      "download_url": "https://..."
    }
  ],
  "total": 1
}
```

**+download-result** 返回：

```json
{
  "output_path": "./{thread-id}/{file_path}/{file_name}",
  "downloaded": ["./{thread-id}/{file_path}/{file_name}"],
  "total": 1
}
```

## 会话文件与资源下载

先用 `+list-thread-file` 获取会话文件列表，再按需用 `+download-result` 并行下载文件资源。

### 获取会话文件

从 `+list-thread-file` 的 `files` 中逐个读取文件元信息：`file_path`、`download_url`。

```
1. file_path在本地已存在
   → 跳过
2. file_path在本地不存在
   → 记录该file_path和URL
   → 使用 +download-result 将URL资源下载到该file_path
```

### 并行下载文件资源

只对目标路径不存在的文件调用下载工具，可并行：

1. 调用 `pippit-cli short-drama +download-result --url DOWNLOAD_URL --output-path FILE_PATH`。
2. 下载完成后，向用户展示本地文件路径；如果某个文件下载失败，只报告该文件错误，不阻塞已成功落盘的文件展示。

## 向用户展示内容

- 任务提交后：立即展示 `web_thread_link`。
- 任务进行中：展示后端 Agent 返回的过程消息。
- 需要用户补充信息时：原样展示后端 Agent 的问题，等待用户回复。
- 任务完成后：展示短剧内容、分集草稿、设定说明或其他结果信息。
- 获取会话文件后：展示或记录文件元信息，不把它当成已下载结果。
- 文件资源下载后：展示已落盘的本地文件路径；已存在而跳过下载的文件也要标明。

## 核心原则：用户侧不做创作，只做传话

你（用户侧 Agent）的职责是传递用户需求和展示后端结果，不是替后端 Agent 创作短剧。

你要做的只有三件事：

1. **上传**：如果用户给了本地参考文件，先调用 `+upload-file`。
2. **提交任务**：把用户原始短剧需求和文件 ID 通过 `+submit-run` 发给后端。
3. **传话、取文件、下载资源**：根据 `+get-thread` 返回的消息展示进展、问题和结果；根据 `+list-thread-file` 获取文件列表；再根据 `download_url` 调用 `+download-result` 把缺失资源下载到用户本地。

**不要做的事：**

- 不要替用户扩写、润色、翻译 prompt。
- 不要自行编排剧情、人物关系、世界观或分集大纲后再提交。
- 不要把用户的一个需求拆成多次 `+submit-run`，除非用户明确要求分多次处理。
- 不要将自己编写的短剧内容混入后端返回结果。

后端 Agent 会负责理解短剧任务、组织创作流程和生成内容。用户侧 Agent 越俎代庖会降低结果一致性。

## 注意事项

- `--message` 是用户的原始短剧需求，不能为空。
- 查询进展时优先使用 `+submit-run` 返回的 `thread_id` 和 `run_id`。
- `--after-seq` 用于增量拉取消息，首次查询可设置为 `0`。
- `+upload-file` 当前用于短剧场景文件上传链路，上传后将返回可传给 `+submit-run` 的文件 ID。
- `+list-thread-file` 只需要 `thread_id`；分页参数默认 `--page-num 1 --page-size 100`。
- `+list-thread-file` 和 `+download-result` 是两个不同的 CLI 指令：前者获取会话文件元信息，后者下载 URL 资源并写入到本地目标文件路径。
- `+download-result` 接收 `--url`、`--output-path`、`--workers`；`--output-path` 必须是包含文件名的目标文件路径。
