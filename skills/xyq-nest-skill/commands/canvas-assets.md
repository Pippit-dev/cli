# Canvas 原生资产命令

本页命令通过 `cli_path` 执行。仅面向个人画布，所有 ID 保持原始字符串，不能转换成 JavaScript Number；`project_id`、`canvas_asset_id`、节点 ID 和媒体 `pippit_asset_id` 不可混用。

| 命令 | 输入 | 返回与完成判断 |
| --- | --- | --- |
| `canvas create` | `--title` 最多 50 字符；`--request-id` 可选；`--wait` 等初始化；可调 `--poll-interval`、`--timeout` | 保存 `project_id`、`canvas_asset_id`、`web_url`、`request_id`；检查 `state`、`warning` 和退出码 |
| `canvas get` | `--asset-id` 必填，可重复 | 返回 `requested_asset_ids`、`assets` 及可用的 `log_id`；核对实际返回资产 |
| `canvas upload` | `--path` 本地文件；可调 `--poll-interval`、`--timeout` | 返回 `pippit_asset_id`、可用的 `media_asset_id`、`source_id`、`locator`、`state`、`warning`；素材可查询不代表已插入画布 |
| `canvas allocate` | `--count` 分配数量 | 返回 `asset_ids[]`；只预留 ID，不创建节点或资产 |
| `canvas apply` | `--project-id` 项目 ID；`--file` JSON 文件，默认 `-` 读 stdin | 一个 transaction，可含多条 patch；检查事务 ACK 和目标资产版本 |

```bash
pippit-tool-cli canvas create --title "产品方案" --wait
pippit-tool-cli canvas get --asset-id CANVAS_ASSET_ID
pippit-tool-cli canvas upload --path "/path/to/reference.png"
```

示例代表不同操作，不应因为读到示例就全部执行。创建、上传可能以退出码 0 返回 `creating` / `processing` 和 `warning`：保留已返回的 ID，稍后用 `canvas get` 回查资产可见性；不要重复创建或上传。画布初始化概览尚未完成时，回查根资产仅证明资产可见，不足以宣称所有初始化完成，应保留 `web_url` 和原始状态说明限制。`request_id` 用于追踪，不能假定跨故障重试严格幂等。

## 低层事务的使用边界

媒体节点优先通过语义 `create_biz_node` 创建，`initialData` 只传 `pippitAssetId`：CLI 查询素材并填写真实云媒体 `assetId`。用 `update_asset.contentPatch` 或 `update_node_data.patch` 替换素材时也可只传新的 `pippitAssetId`。显式填写的 `assetId` 必须与素材查询结果匹配，否则返回 `INVALID_MEDIA_IDENTITY` 并拒绝写入。

上传顶层旧字段 `asset_id` 是保留兼容的 workspace/source ID，不能当节点媒体 ID。`media_asset_id` 来自查询结果对应的 `Image/Video/Audio.asset_id`；缺失时不能用 `source_id`、`pippit_asset_id` 或画布节点 ID 补位。`node_asset_refs[].pippit_asset_id` 使用画布节点资产 ID。

批量 `apply_mutations` 默认非原子执行：必须检查 `data.results` 中每一项的 `committed` / `aborted` 状态，保留已成功项，不重跑整批。需要任一失败时整批回滚，可显式传 `atomic: true`。

普通节点和领域编辑优先使用 [语义命令](canvas.md)，由 SDK 分配 ID 并构造事务。仅在用户确实提供或需要底层资产事务且契约已核实时执行：

```bash
pippit-tool-cli canvas allocate --count 2
pippit-tool-cli canvas apply --project-id PROJECT_ID --file "/path/to/validated-patch.json"
```

JSON 根包含 `batch_id`、`client_id`、`transactions`，可含 `root_pippit_asset_id` 和 `Base`；单一 transaction 含 `transaction_id`、`patches`。每个 patch 使用实际 `asset_id`、`op`、`path`、所需 `value` 与适用的 `base_asset_version`，具体版本及内容必须来自真实查询或已核实调用契约。保留调用方原有 batch/transaction ID，不擅自改前缀；不手工绕过语义命令的业务校验。

写入结果不明确、超时或版本冲突时，先查询受影响资产，再决定如何恢复；不能照原请求盲目重放。不要使用隐藏传输参数绕过 ACK 校验。

`canvas apply` 在发送写入前验证本次修改涉及的媒体身份，包括部分字段 patch；按整笔事务最终结果校验，拒绝 Pippit ID、source ID、节点 ID 与真实媒体 ID 的错配。低层写入不会自动改写调用方的 patch。
