# 示例：参考剧本与会话续接

用户提供一个本地 outline.txt，要求：“参考这个大纲写第一集。”

先按 [上传说明](../commands/upload-file.md) 检查格式，只上传一个文件：

```bash
pippit-tool-cli short-drama +upload-file --path "/path/to/outline.txt"
pippit-tool-cli short-drama +submit-run --message "参考这个大纲写第一集。" --asset-ids ASSET_ID
```

第二条中的 ASSET_ID 来自第一条真实返回。保存会话、Run 和剧本绑定关系，按 [基础示例](create-and-deliver.md) 查询、下载和交付。

后端询问人物动机或风格时，按 [创作流程](../workflows/creation.md) 展示问题并等待。用户回答后，原样发送到同一会话：

```bash
pippit-tool-cli short-drama +submit-run --message "用户的原始回答" --thread-id THREAD_ID
```

记录新的 run_id，继续查询该 Run；不新建会话，也不再次传剧本 asset_id。

用户后续要求：“继续写下一集，重点描写主角的逃亡。”同样续接：

```bash
pippit-tool-cli short-drama +submit-run --message "继续写下一集，重点描写主角的逃亡。" --thread-id THREAD_ID
```

新 Run 从第一页核对会话文件，只下载更新或新增的保留产物，避免重复交付未变更版本。已绑定剧本的会话不能再追加第二个剧本；用户只要求查进度或取件时，只查原会话，不运行上述提交命令。

## 查询业务内容

用户问“画布有哪些场景”，在原会话原样提交：

```bash
pippit-tool-cli short-drama +submit-run --message "画布有哪些场景" --thread-id THREAD_ID
pippit-tool-cli get-thread --thread-id THREAD_ID --run-id NEW_RUN_ID
```

NEW_RUN_ID 来自本次提交。按轮询流程取得服务端回答，保留“画布”口径；不枚举 `/scene/` 目录、不从网页统计节点、不从剧本抽取地点来代答。若服务端只返回名称，没有返回总数或图片状态，就只展示名称。

用户问“第一集片段1内容是什么”，同样原样提交到当前会话，由服务端读取片段内容后回答；不下载 `storyboard.json` 或用第一集正文拼出片段。若服务端读取失败，报告无法确认，不重新生成故事版。

用户问“现在生成到哪了”，只查询原 Run 的进度；用户要求“下载第一集正文”，只定位并下载对应保留文件。这两种请求均不新建 Run。

服务端先返回“正在读取场景”或部分名单、Run 仍进行中时，继续等待最终回答。用户说“看看有哪些场景，把缺的图补齐”时保留完整原话，并按已授权范围执行，不因包含查询而再次索要相同授权。

文件交付时，若旧版正文已交付而同路径出现新 updated_at，将新版下载到独立批次目录，校验后交付；若图片 download_url 为空，记录待链接并做有界收尾核对。恢复任务时继续处理未交付记录，不将缺链接、下载失败或本地文件已丢失的条目当作完成。
