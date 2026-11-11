# Canvas 原生资产命令

本页命令通过 `cli_path` 执行。支持个人和团队画布，复用默认空间或 `--team-id TEAM_ID`；创建、上传、查询和修改须使用同一空间。所有 ID 保持原始字符串，不能转换成 JavaScript Number；`project_id`、`canvas_asset_id`、节点 ID 和媒体 `pippit_asset_id` 不可混用。

| 命令 | 输入 | 返回与完成判断 |
| --- | --- | --- |
| `canvas create` | `--title` 最多 50 字符；`--request-id` 可选；`--wait` 等初始化；可调 `--poll-interval`、`--timeout` | 保存 `project_id`、`canvas_asset_id`、`web_url`、`request_id`；检查 `state`、`warning` 和退出码 |
| `canvas get` | `--asset-id` 必填，可重复 | 返回 `requested_asset_ids`、`assets` 及可用的 `log_id`；核对实际返回资产 |
| `canvas upload` | `--path` 本地文件或 `--url` HTTPS 媒体直链，必须且只能提供一个；可调 `--poll-interval`、`--timeout` | 返回 `pippit_asset_id`、可用的 `media_asset_id`、`source_id`、`locator`、`state`、`warning`；素材可查询不代表已插入画布 |
| `canvas allocate` | `--count` 分配数量 | 返回 `asset_ids[]`；只预留 ID，不创建节点或资产 |

```bash
pippit-tool-cli canvas create --title "产品方案" --wait
pippit-tool-cli canvas get --asset-id CANVAS_ASSET_ID
pippit-tool-cli canvas upload --path "/path/to/reference.png"
pippit-tool-cli canvas upload --url 'https://example.com/reference?signature=...' --team-id TEAM_ID
```

示例代表不同操作，不应因为读到示例就全部执行。创建、上传可能以退出码 0 返回 `creating` / `processing` 和 `warning`：保留已返回的 ID，稍后用 `canvas get` 回查资产可见性；不要重复创建或上传。画布初始化概览尚未完成时，回查根资产仅证明资产可见，不足以宣称所有初始化完成，应保留 `web_url` 和原始状态说明限制。`request_id` 用于追踪，不能假定跨故障重试严格幂等。

## URL Import

画布流程可使用 `canvas upload --url` 导入外链；只需要稳定资产 ID 时也可按 [上传说明](upload-file.md) 使用 `upload-file --url`，同一素材选择一个入口，不重复导入。导入和目标画布使用相同 `--team-id`。Canvas 上传保存 `pippit_asset_id` 和 `state`，通用上传保存 `team_id` 与 `asset_id`。

URL 导入前检查实际入口的 `--help` 含 `--url`；旧版缺少时按 [安装说明](../scripts/install.md) 更新并重新检查，仍不支持则说明缺口。服务端也须部署 URL 能力。仅支持服务端可访问且不重定向的 HTTPS 图片、视频、音频直链；当前服务端上限为 200 MiB，仍需格式校验和审核。不要求素材类型或文件名；保留原签名和查询参数，不把 URL 放进 `--path`。CLI 不读取、下载或缓存源素材。

`--poll-interval` 与 `--timeout` 只控制上传返回后的素材可查询等待，不是服务端导入的超时参数。等待失败或超时仍输出已有稳定 ID、`processing` 和 `warning`，后续用 `canvas get` 查询，不重放上传。`ready` 不表示已创建节点；批量需控制导入和查询并发。

这里的 `canvas upload.pippit_asset_id` 与 `upload-file.asset_id` 都是稳定的 Pippit 素材 ID。先用 `pippit-tool-cli canvas get --asset-id PIPPIT_ASSET_ID --team-id TEAM_ID` 查询真实素材，再在用户要求建节点时，将该 ID 填入语义 `create_biz_node.initialData.pippitAssetId`，其余输入按当前 schema 和目标画布确定。节点中的真实云媒体身份由现有 CLI 查询并填写，不把源 URL 写成节点 ID 或媒体 ID。写后按 [画布编辑流程](../workflows/canvas-edit.md) 回读；素材导入成功不代表节点已创建。

## 语义编辑的媒体身份

媒体节点优先通过语义 `create_biz_node` 创建，`initialData` 只传 `pippitAssetId`：CLI 查询素材并填写真实云媒体 `assetId`。用 `update_asset.contentPatch` 或 `update_node_data.patch` 替换素材时也可只传新的 `pippitAssetId`。显式填写的 `assetId` 必须与素材查询结果匹配，否则返回 `INVALID_MEDIA_IDENTITY` 并拒绝写入。

普通 `biz/image` 的素材绑定在同层 `content.pippitAssetId` 与 `content.assetId`。`content.appearanceInfo`、`content.sourceInfos` 属于角色/场景结构，不能套用于普通图片；角色/场景继续按各自领域命令及 schema 操作。

以下仅用于核对保存后的节点结构，ID 占位符须替换为同空间真实查询结果，不作为完整写入请求直接执行：

```json
{"type":"biz/image","content":{"pippitAssetId":"PIPPIT_MATERIAL_ID","assetId":"CLOUD_IMAGE_ASSET_ID"}}
```

错误反例：同层绑定缺失，即使保存 ACK，也不是可用参考图：

```json
{"type":"biz/image","content":{"appearanceInfo":{"pippitAssetId":"PIPPIT_MATERIAL_ID","assetId":"CLOUD_IMAGE_ASSET_ID"}}}
```

可先创建不含绑定的图片空草稿；不能用 `generation`、`sourceInfos` 或手改“已上传”状态把空草稿视为素材已就绪。已有错绑数据先回读、核对真实素材，再在用户授权的编辑范围内修正，不自动迁移用户画布。

`canvas upload` 顶层旧字段 `asset_id` 是保留兼容的 workspace/source ID，不能当节点媒体 ID；它与 `upload-file.asset_id` 的含义不同。`media_asset_id` 来自查询结果对应的 `Image/Video/Audio.asset_id`；缺失时不能用 `source_id`、`pippit_asset_id` 或画布节点 ID 补位。`node_asset_refs[].pippit_asset_id` 使用画布节点资产 ID。

画布修改使用 [语义命令](canvas.md)，先查询当前目录和 schema，再由 SDK 分配 ID、构造和提交事务。`apply_mutations` 由 CLI 固定使用原子执行；检查结果中的每项状态，写入结果不明确、超时或版本冲突时，先查询受影响资产，不能盲目重跑整批。

写入传输保留媒体身份、事务 ACK 和资产版本校验，包括部分字段 patch；按整笔事务最终结果核对 Pippit ID、source ID、节点 ID 与真实媒体 ID。语义命令负责查询和填写媒体绑定，原始事务传输不作为公开命令提供。
