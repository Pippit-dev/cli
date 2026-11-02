# Canvas command runtime artifact

正式发版使用本目录中的固定运行时产物：

- `xyq-canvas-command-runtime.cjs`
- `xyq-canvas-command-runtime.cjs.LEGAL.txt`
- `xyq-canvas-command-runtime.cjs.sha256`

校验文件使用标准 SHA-256 格式：

```text
<runtime 的 64 位十六进制摘要>  xyq-canvas-command-runtime.cjs
<LEGAL 的 64 位十六进制摘要>  xyq-canvas-command-runtime.cjs.LEGAL.txt
```

产物与校验文件必须来自同一次受审构建，并在发布 tag 对应的提交中固定。`npm prepack`
会校验产物、必要导出和摘要，再将三份文件装配到 `dist/`。GoReleaser 的 `--clean` 只清理
`dist/`，不会删除本目录中的发布源文件。

## 当前产物来源

- SDK source: `1e5f56e1fe3561477855a8da7a26db082840454c`（`codex/revert-cli-reference-autosync`），CLI 包版本 `1.0.51`。
- 回退 `1ba2d214e51` 与 `26efe81de64` 的自动引用协议，恢复 `create_edge`、`reconnect_edge`、`update_edge_data`、`delete_edges`；公开命令共 45 项。
- `create_biz_node` 不再提供顶层 `references`。`update_node_data` 写入 `generation.references` 只修改草稿数据，不自动同步边；其他生成参数编辑无需携带引用列表。未恢复中间版本的 `xyq.generation.update_references`。
- 保留图片/视频空节点按生成比例预留尺寸、媒体身份和视频 ratio 校验、现有时间轴协议及 Node 16 兼容。Web 的节点创建和尺寸逻辑保持 master 行为。
- 沿用 master 的 3D V2 文档与能力边界：工厂可创建 V2 节点，旧 `xyq.scene3d.apply/query` 不注册。回退没有恢复旧 3D V1 适配。
- 两次独立构建的 runtime、LEGAL 与校验文件逐字节一致；源文件指纹与构建环境见 [build-provenance.json](build-provenance.json)。此补丁只准备 MR，不发布 registry SDK 包或 CLI。
- runtime SHA-256: `7e45eb4e7bb2323fd1a0f5bc56b731247076f9ae8171f46d01ba4d6774de1b0f`
- LEGAL SHA-256: `c29b5a61e7c48ec377826ec7ff6a6f2b16f0824e5a4b7f278f622524c694e047`
