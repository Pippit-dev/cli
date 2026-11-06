# 画布查询、编辑与验证

适用于 [Canvas](../commands/canvas.md) 任务。该流程以画布状态为交付对象，创建、编辑不会自动产生可交付媒体文件。

1. **准备入口**：执行 Canvas 安装检查，保存 `cli_path` 和 `canvas_entry`，按授权文档确认身份，并按 Space Selection 确定 TeamID。后续独立命令均显式使用同一 `--team-id TEAM_ID`，个人为 `0`；已有画布沿用原空间。只查命令目录和离线指南无需登录；查询真实画布、执行编辑需要授权。
2. **确定目标**：已有画布使用真实 `canvas_asset_id` 作为语义命令的 `--canvas-id`。用户提供的信息无法唯一定位时先补齐，不创建替代画布。新画布仅在用户要求创建时使用原生 `canvas create`，检查其返回状态。
3. **发现与查询**：`list → describe/schema` 定位操作。用 `get_snapshot` 查节点，用 `get_asset` 查具体资产；3D/多轨再查询其独立文档。`run` 是统一执行入口，其中 query/get 类操作是读取，不等于所有 run 都是写入。
4. **准备输入**：使用查询所得的节点、子对象和资产 ID。外链素材通过 `canvas upload --url` 导入并保存 `pippit_asset_id`、`state` 与可能的 `warning`；也可复用 `upload-file --url` 返回的稳定 `asset_id`，再按 [URL 导入与资产复用](../commands/canvas-assets.md#url-import) 创建所需节点；已有资产直接复用，不重复导入。多轨 `expectedRevision` 来自最新查询。复杂输入写入 JSON 文件后传 `--file`；不要同时传 `--file` 和 `--input`。只操作用户指定范围。
5. **预演与写入**：当前 schema 支持 `dryRun` 时可先预演，再在已有授权范围内执行真实修改。不能给所有命令强加此字段：领域命令的 dryRun 验证具体编辑，通用 `apply_mutations.dryRun` 不能替代领域预演。删除、恢复等操作仅按用户明确要求执行。
6. **验证结果**：检查退出码与 JSON 的 `ok`、错误信息，`ok=false` 即失败。`dryRun=true` 成功不代表落盘；实际写入成功后，用对应查询回读目标字段及关联关系，确认没有误改其他对象。素材绑定还须按[图片绑定契约](../commands/canvas-assets.md#低层事务的使用边界)核对节点类型、字段位置和素材身份：普通 `biz/image` 的 `content.pippitAssetId` 必须是所选图片素材，`content.assetId` 必须匹配该素材查询的 `Image.asset_id`。保存 ACK 只证明事务被接受，不证明参考素材可用。
7. **反馈交付**：说明已验证的画布变化，保留已有/返回的画布链接及目标 ID。只完成文档编辑时不要声称图片/视频已生成；确有媒体文件待交付时遵循 [媒体交付标准](async-delivery.md)。

语义执行骨架（占位符替换为当前目录中的命令、真实 ID 和已准备的输入文件）：

```bash
node "CANVAS_ENTRY" canvas command run COMMAND_NAME --canvas-id CANVAS_ASSET_ID --team-id TEAM_ID --file "/path/to/input.json"
```

写请求失败或返回“未确认事务已隔离”时保留错误上下文，先回读状态，不自动重跑、不删除本地恢复记录。多轨版本冲突需重新查询并基于新状态准备操作；检查点恢复会改变画布，不能作为所有失败的自动回滚方式。

后续任务要把节点用于生视频参考时，先在原空间回读并完成上述核对。绑定缺失、错层或素材未就绪时停止提交，明确指出节点和缺失字段；保留原始错误与 LogID，不反复重试或手改上传状态。`node asset resource not ready` 表示素材预处理未就绪；即使外层显示“服务器繁忙”，也不能据此把重复生成当作恢复方式。
