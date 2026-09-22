# short-drama +upload-file：上传参考剧本

```bash
pippit-tool-cli short-drama +upload-file --path "/path/to/outline.txt"
```

`--path` 必填，必须是存在的本地文件而非目录。仅支持 `.doc`、`.docx`、`.txt`；不接受 `.md`、`.pdf`、图片、视频或 URL，不擅自转换、改写文件。

成功 stdout 为 JSON：

```json
{"asset_id":"ASSET_ID"}
```

该 ID 来自服务端 `pippit_asset_id`，缺失时回退 `asset_id`。将其原样用于一次 `short-drama +submit-run --asset-ids ASSET_ID`。记录会话与剧本的绑定关系，同一 thread_id 后续只传续接需求，不重复上传或追加剧本；多个文件先选一个，或按用户要求分别创作。

失败时先解决文件或授权问题，不编造 asset_id、不把上传成功当作创作完成。上传命令不发送来源统计参数。参考 [带剧本创作与续接](../examples/reference-and-continue.md)。
