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

新 Run 从第一页核对会话文件，下载更新或新增的重要资产，避免重复交付未变更版本。已绑定剧本的会话不能再追加第二个剧本；用户只要求查进度或取件时，只查原会话，不运行上述提交命令。
