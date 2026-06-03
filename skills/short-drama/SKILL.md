---
name: pippit-short-drama-skill
description: 使用 pippit-tool-cli 的短剧场景能力提交和查询短剧创作任务。覆盖短剧生成、续写、改写、剧情扩展、人物设定、分集草稿、世界观设定、会话文件获取、文件资源下载等创作场景。当用户要求创作短剧、写短剧剧本、续写故事、修改剧情、补充角色设定、查询短剧任务进展、获取短剧会话文件或下载短剧文件资源，或提到 pippit-tool-cli short-drama / 小云雀短剧时触发。
user-invocable: true
metadata:
  {
    "openclaw":
      {
        "emoji": "📖",
        "requires":
          {
            "bins": ["pippit-tool-cli"]
          }
      }
  }
---

# 小云雀短剧创作

通过 `pippit-tool-cli short-drama` 命令提交短剧创作任务、上传参考文件，并行查询任务进展和会话产物文件，及时把重要资产下载到用户本地。

短剧场景面向剧情、人物、分集与画面化叙事创作，用户的原始需求通过 `--message` 发送给后端 Agent。后端 Agent 负责理解任务、编排流程和生成内容；用户侧 Agent 负责提交任务、并行查询进展与产物、主动下载重要资产并展示结果。

## 功能

1. **提交短剧 Run 任务** - 创建新会话或向已有会话发送短剧创作需求。
2. **查询会话进展** - 根据 `thread_id` 和可选 `run_id` 拉取服务端 v2 `readable_text`，用于展示短剧任务进展、问题和结果。
3. **上传文件** - 上传本地 `.doc` / `.docx` / `.txt` 参考文件，得到 `asset_id`，供后续任务引用。
4. **获取会话文件** - 根据 `thread_id` 拉取会话文件列表，得到 `file_path`、`download_url`。这和查询会话进展同等重要。
5. **下载重要资产** - 使用文件列表中的 `download_url` 下载资源，并按 `file_path` 写入用户本地目标文件路径。

重要资产包括但不限于：剧本设计、场景设计、场景图、人物角色设计、人物图、分集草稿、故事板、最终视频产物。只要 `+list-thread-file` 返回了这些资产的 `download_url`，就要及时调用下载工具落盘，不要只展示文件元信息。

## 短剧主流程顺序

短剧创作按以下主流程推进。用户侧 Agent 在展示后端 Agent 的表单、问卷、选项或确认问题时，必须先参考这个顺序判断当前阶段和合理下一步。

1. 剧本上传 / AI 剧本生成 / AI 剧本编辑
2. 剧本合并与完整剧本确认
3. 剧本分析
4. 短剧风格推荐确认
5. 剧本标准化（可选）
6. 场景分析
7. 所有必要场景图生成
8. 角色分析
9. 所有必要角色图生成
10. 分镜设计
11. 分镜视频生成
12. 完整视频合成

## 表单与问卷选项处理原则

后端 Agent 通过 `readable_text` 发出表单、问卷、选项、按钮或询问用户时，用户侧 Agent 不要机械原样转述所有选项。先结合短剧主流程顺序清洗选项，再把合理、必要、当前可执行的流程项呈现给用户。

- 保留当前阶段的确认项，以及不会跳过必要阶段的下一步流程项。
- 剔除跳过必要阶段的选项。例如未完成“剧本合并与完整剧本确认”前，不应让用户直接进入“剧本分析”；未完成“所有必要场景图生成”前，不应让用户直接进入“角色分析”。
- 剔除倒退到无关阶段的选项。只有用户明确要求返工、修改或重新生成时，才展示回退选项。
- `剧本标准化` 是可选阶段，只能出现在“短剧风格推荐确认”之后、“场景分析”之前。不要把它包装成任意阶段都可以跳过或补做的通用选项。
- 不替用户决定创意内容，例如风格、剧情方向、角色设定、镜头方案。只能清洗流程选项，不能代替用户选择创作偏好。
- 如果服务端问题混入跨度过大的多个流程选项，重新组织成当前阶段可回答的问题，并说明已按主流程剔除不合理或跳跃选项。

## 前置要求

需要已安装 `pippit-tool-cli`：

```bash
npx @pippit-dev/cli@latest install
```

## 使用方法

### 1. 提交短剧任务

```bash
# 创建新会话并提交短剧创作需求
pippit-tool-cli short-drama +submit-run --message "创作一个赛博朋克短剧开头"

# 向已有会话追加新的短剧需求
pippit-tool-cli short-drama +submit-run --message "继续写下一集，重点描写主角的逃亡" --thread-id THREAD_ID

# 携带已上传剧本文件 asset_id 提交任务；同一 thread_id 只允许一个剧本文件
pippit-tool-cli short-drama +submit-run --message "参考这个大纲写第一集" --asset-ids ASSET_ID
```

### 2. 查询短剧任务进展

```bash
# 查询会话可读进展
pippit-tool-cli short-drama +get-thread --thread-id THREAD_ID --run-id RUN_ID
```

> `thread_id` 和 `run_id` 由 `+submit-run` 返回。`run_id` 可省略，省略时返回当前 `thread_id` 下的所有 Run；传入时只看指定 Run。

### 3. 上传文件

当用户提供短剧大纲、人物设定、世界观设定、已有分集或剧本等本地参考文件时，可先上传文件。`+upload-file` 当前只接收本地文件路径，并且只支持 `.doc`、`.docx` 和 `.txt` 后缀；不要把 `.md`、`.pdf`、图片、视频或 URL 传给该命令。

```bash
pippit-tool-cli short-drama +upload-file --path /path/to/outline.txt
```

上传成功后命令只返回 `asset_id`：

```json
{
  "asset_id": "asset_..."
}
```

后续提交任务时，把该值作为唯一的 `--asset-ids` 传给 `+submit-run`。单次创作会话中（相同 `thread_id`），只支持上传并绑定一个剧本文件；如果用户提供多个剧本文件，先让用户选择一个，或为不同剧本分别开启新的创作会话，不要在同一 `thread_id` 下重复追加剧本文件。

### 4. 获取会话文件

```bash
# 获取会话文件列表
pippit-tool-cli short-drama +list-thread-file --thread-id THREAD_ID --page-num 1 --page-size 200
```

`+list-thread-file` 返回的每个文件对象包含：

```json
{
  "file_path": "./{thread-id}/路径/文件名", // 文件完整路径，包含文件名
  "download_url": "https://...", // URL
  "updated_at": 1779716734 // 文件更新时间，Unix 秒级时间戳
}
```

`+list-thread-file` 只负责获取会话文件列表，不负责下载文件，也不需要判断本地文件是否已存在。

### 5. 下载文件资源

```bash
# 下载文件资源到指定文件路径
pippit-tool-cli short-drama +download-result --url DOWNLOAD_URL --output-path FILE_PATH --updated-at UPDATED_AT
```

`FILE_PATH` 必须直接使用 `+list-thread-file` 返回的完整 `file_path`，包含文件名，不要取父目录。`UPDATED_AT` 使用同一文件对象返回的 `updated_at`；如果没有 `updated_at`，可省略 `--updated-at`。`+download-result` 负责把会话产生的文件通过 URL 下载到该目标文件路径；如果目标文件已存在且本地修改时间不早于 `updated_at`，跳过下载；如果本地文件早于 `updated_at`，覆盖更新。

## 典型工作流

### 场景 1：用户要求生成短剧内容

```
1. pippit-tool-cli short-drama +submit-run --message "用户的原始短剧需求"
   → 拿到 thread_id、run_id 和 web_thread_link
2. 立即将 web_thread_link 展示给用户
3. 并行发起，二者同等重要：
   a. pippit-tool-cli short-drama +get-thread --thread-id THREAD_ID --run-id RUN_ID
   b. pippit-tool-cli short-drama +list-thread-file --thread-id THREAD_ID --page-num PAGE_NUM --page-size 200
4. 检查 `get-thread` 返回的 readable_text：
   - 如果任务仍在进行中：展示可读进展，继续查询
   - 如果后端 Agent 提出问题：从 readable_text 中提取问题并展示，等待用户回复
5. 检查 `list-thread-file` 返回的 files：
   - 对每个文件取 file_path、download_url、updated_at
   - 将 file_path 作为本地目标文件路径，包含文件名
   - 有 download_url 的重要资产：加入本轮下载队列
   - 不判断 file_path 在本地是否已存在，是否跳过由 +download-result 内部处理
   - 如果本轮 total 达到 200：下一轮将 PAGE_NUM 加 1，继续查询新一页文件
6. 对重要资产，立即调用 +download-result 并行下载资源：
   - 使用第 5 步获取的 download_url 作为 --url
   - 使用第 5 步获取的完整 file_path 作为 --output-path
   - 如果第 5 步返回 updated_at，作为 --updated-at 传入
   - 剧本设计、场景设计、场景图、人物角色设计、人物图、最终视频产物都属于重要资产
7. 查询或下载失败时，不要直接放弃；记录失败项，并在后续轮询中主动重试
8. 只有会话进展已处理，且已发现的重要资产均已下载或明确重试失败后，才向用户汇总最终结果
9. 如用户继续追加需求，使用同一 thread_id 再次 submit-run
```

### 场景 2：用户提供参考文件要求创作

```
1. 检查用户提供的是一个本地 `.doc`、`.docx` 或 `.txt` 剧本文件路径；如果不是，告知当前上传命令只支持这三类文件，不要擅自转换或改写文件。
2. pippit-tool-cli short-drama +upload-file --path /path/to/file.txt
   → 拿到 asset_id
3. pippit-tool-cli short-drama +submit-run --message "用户的原始短剧需求" --asset-ids asset_id
   → 拿到 thread_id、run_id 和 web_thread_link
4. 记录该 thread_id 已绑定这个剧本文件；后续同一 thread_id 的续写或修改只传 --thread-id，不再传新的剧本 asset_id
5. 后续同场景 1 的并行查询、重要资产发现和文件下载流程
```

### 场景 3：在已有短剧会话中续写或修改

```
1. pippit-tool-cli short-drama +submit-run --message "用户的新需求" --thread-id THREAD_ID
   → 拿到新的 run_id 和 web_thread_link
2. 如果该 THREAD_ID 已经绑定过剧本文件，不要再上传或通过 --asset-ids 追加第二个剧本文件
3. 继续按场景 1 展示进展、处理用户补充问题、获取新增会话文件列表，并及时下载新增重要资产
```

## 轮询策略

- **间隔**：每 10 秒查询一次。
- **进展查询**：每轮调用 `+get-thread` 查看 `readable_text`。优先带上本轮 `run_id` 聚焦当前任务；需要查看整个会话时可省略 `--run-id`。
- **并行查询**：每次 `+submit-run` 返回 `thread_id` 后，同时发起 `+get-thread` 和 `+list-thread-file`；二者同等重要，不能只查询会话进展而忽略会话文件。
- **文件分页**：`+list-thread-file` 使用 `--page-size 200`。如果本轮返回的 `total` 达到 200，下一轮使用 `--page-num` 加 1 查询新一页结果；如果未达到 200，保持当前页继续轮询新增产物。
- **重要资产识别**：每轮都检查 `+list-thread-file` 返回的文件。剧本设计、场景设计、场景图、人物角色设计、人物图、分集草稿、故事板、最终视频产物都是重要资产。
- **文件下载**：解析 `+list-thread-file` 的结果后，对带 `download_url` 的重要资产立即调用 `+download-result` 下载资源；不要在 `list-thread-file` 阶段检查文件是否已存在，存在性检查由下载工具内部处理。
- **下载完成标准**：不要把文件元信息展示当成下载完成；必须拿到本地 `file_path`，或明确记录该文件在重试后仍下载失败。
- **用户确认**：如果消息中出现需要用户确认、补充设定或回答问题的内容，先判断是否包含表单、问卷、选项或按钮；包含时按“短剧主流程顺序”和“表单与问卷选项处理原则”清洗选项，再展示给用户并等待回复。
- **超时**：如果长时间无结果，告知用户任务仍在生成中，可稍后通过 `web_thread_link` 查看。
- **错误处理**：`+get-thread`、`+list-thread-file` 或 `+download-result` 任一调用失败时，记录失败原因和参数，在后续轮询中主动重试；重试期间继续处理其他成功返回的消息和文件。连续多轮失败后再向用户说明仍未完成的查询或下载项。

## 完成标准

一次短剧任务不能只以 `+get-thread` 返回的 `readable_text` 作为结束条件。完成前必须同时检查：

1. 已处理 `+get-thread` 返回的最新 `readable_text`、用户确认问题和最终消息。
2. 已用 `--page-size 200` 调用 `+list-thread-file` 获取会话文件列表；如果本轮 `total` 达到 200，已在后续轮询中递增 `page-num` 查询新一页。
3. 对所有带 `download_url` 的重要资产，已调用 `+download-result` 下载到本地 `file_path`。
4. 已按短剧主流程顺序检查服务端表单、问卷和选项，没有把跳过必要阶段的选项直接呈现给用户；如果跳过 `剧本标准化`，已明确这是可选阶段。
5. 对查询失败或下载失败的资产，已在后续轮询中主动重试，并在最终回复中列出仍失败的文件或命令。

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

```text
Thread: thread_...
     标题: ...
     状态: ...

     -- Run #1 --
       [assistant] ...
```

**+upload-file** 返回：

```json
{
  "asset_id": "asset_..."
}
```

`+upload-file` 通过 `multipart/form-data` 上传文件，表单文件字段名为 `file`。本地文件必须存在、不能是目录，后缀必须是 `.doc`、`.docx` 或 `.txt`；不支持的后缀会直接报错。返回的 `asset_id` 来自服务端 `pippit_asset_id`，如果没有该字段才回退到 `asset_id`。

**+list-thread-file** 返回：

```json
{
  "files": [
    {
      "file_path": "./{thread-id}/{file_path}/{file_name}",
      "download_url": "https://...",
      "updated_at": 1779716734
    }
  ],
  "total": 1,
  "message": "<system-remind>\n- total reached 200; query the next page with --page-num {page-num} + 1\n</system-remind>"
}
```

当 `total` 达到 200 时，`message` 会用 `<system-remind>` 提示下一轮将 `page-num` 加 1 查询新一页。

**+download-result** 返回：

```json
{
  "output_path": "./{thread-id}/{file_path}/{file_name}",
  "downloaded": ["./{thread-id}/{file_path}/{file_name}"]
}
```

## 会话文件与资源下载

先用 `+list-thread-file` 获取会话文件列表，再用 `+download-result` 并行下载重要资产。获取文件元信息不是最终目标，重要资产落盘才是核心目标。文件是否已存在由下载工具内部检查，`+list-thread-file` 阶段不要做本地存在性判断。

### 获取会话文件

从 `+list-thread-file` 的 `files` 中逐个读取文件元信息：`file_path`、`file_name`、`download_url`、`updated_at`。重点识别剧本设计、场景设计、场景图、人物角色设计、人物图、分集草稿、故事板、最终视频产物等重要资产。

```
1. 有download_url的重要资产
   → 记录该file_path、URL和updated_at
   → 使用 +download-result 将URL资源下载到该file_path；有updated_at时传入--updated-at
2. 本轮total达到200
   → 下一轮page-num加1，继续查询新一页结果
3. 本轮total未达到200
   → 后续轮询保持当前页，继续发现新增产物
4. list-thread-file或download-result失败
   → 记录失败参数和错误
   → 后续轮询主动重试，不要直接结束任务
```

### 并行下载文件资源

对带 `download_url` 的重要资产调用下载工具，可并行。重要资产必须主动下载，不要等用户再次要求，也不要在调用下载工具前先检查本地文件是否存在。

1. 调用 `pippit-tool-cli short-drama +download-result --url DOWNLOAD_URL --output-path FILE_PATH --updated-at UPDATED_AT`；如果文件对象没有 `updated_at`，省略 `--updated-at`。
2. 下载完成后，向用户展示本地文件路径；如果某个文件下载失败，记录失败项并在后续轮询中重试，不阻塞已成功落盘的文件展示。

## 向用户展示内容

- 任务提交后：立即展示 `web_thread_link`。
- 任务进行中：展示后端 Agent 返回的过程消息。
- 需要用户补充信息时：如果是普通问题，展示后端 Agent 的问题并等待用户回复；如果包含表单、问卷、选项或按钮，先按短剧主流程清洗不合理或跳跃的流程选项，再展示给用户。
- 任务完成后：展示短剧内容、分集草稿、设定说明或其他结果信息，同时检查是否有未下载的重要资产。
- 获取会话文件后：展示或记录文件元信息，不把它当成已下载结果。
- 文件资源下载后：展示已落盘的本地文件路径；已存在而跳过下载的文件也要标明。
- 如果仍有重要资产下载失败：说明失败文件、失败命令和已进行的重试，不要把它描述为已完成下载。

## 核心原则：用户侧不做创作，只做传话

你（用户侧 Agent）的职责是传递用户需求和展示后端结果，不是替后端 Agent 创作短剧。

你要做的只有三件事：

1. **上传**：如果用户给了本地 `.doc` / `.docx` / `.txt` 参考文件，先调用 `+upload-file`。
2. **提交任务**：首次创作时把用户原始短剧需求和唯一剧本 `asset_id` 通过 `+submit-run --asset-ids` 发给后端；同一 `thread_id` 后续续写或修改不再追加新的剧本文件。
3. **传话、取文件、下载资源**：根据 `+get-thread` 返回的 `readable_text` 展示进展、问题和结果；遇到表单、问卷、选项或按钮时，只做流程合理性清洗，不替用户决定创作内容；根据 `+list-thread-file` 获取文件列表；再根据 `download_url` 调用 `+download-result` 把缺失资源下载到用户本地。

**不要做的事：**

- 不要替用户扩写、润色、翻译 prompt。
- 不要自行编排剧情、人物关系、世界观或分集大纲后再提交。
- 不要把用户的一个需求拆成多次 `+submit-run`，除非用户明确要求分多次处理。
- 不要将自己编写的短剧内容混入后端返回结果。

后端 Agent 会负责理解短剧任务、组织创作流程和生成内容。用户侧 Agent 越俎代庖会降低结果一致性。

## 注意事项

- `--message` 是用户的原始短剧需求，不能为空。
- 查询进展时优先使用 `+submit-run` 返回的 `thread_id` 和 `run_id`；如果需要查看整个会话，可以省略 `--run-id`。
- `+get-thread` 当前固定走服务端 v2 响应，输出字段是 `readable_text`；不要解析旧版 `messages` 数组。
- `+upload-file` 当前用于短剧场景文件上传链路，只支持本地 `.doc` / `.docx` / `.txt` 文件；`--path` 不能为空，路径必须指向真实文件，不能是目录。
- `+upload-file` 上传成功后只返回 `asset_id`；把该值原样作为 `+submit-run --asset-ids` 的参数。
- 单次创作会话中（相同 `thread_id`），`+submit-run` 只支持绑定一个剧本文件。不要在同一 `thread_id` 下重复上传并追加第二个剧本 `asset_id`；用户给多个剧本时，先让用户选择一个，或分别开启新的创作会话。
- `+list-thread-file` 只需要 `thread_id`；分页参数使用 `--page-num 1 --page-size 200` 起步，`total` 达到 200 时下一轮递增 `page-num`。
- `+list-thread-file` 和 `+download-result` 是两个不同的 CLI 指令：前者获取会话文件元信息，后者下载 URL 资源并写入到本地目标文件路径。
- `+download-result` 接收 `--url`、`--output-path`、`--updated-at`、`--workers`；`--output-path` 必须是包含文件名的目标文件路径。
