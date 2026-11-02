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

- SDK source: `7f0ec8ad57ea62f7b29694fb53df1c9f51b355be`（`feat/novel_canvas_cli`），CLI 包版本 `1.0.50`。
- 图片和视频空节点按生成比例预留尺寸；Web 默认创建逻辑保持原样。媒体尺寸读取及面积保持计算复用 SDK 实现。
- 引用关系通过节点的 `generation.references` 声明并同步连线；命令列表移除 `create_edge`、`reconnect_edge`、`update_edge_data`、`delete_edges`。创建时可用 `references` 指定源节点，替换 generation 时须携带完整引用列表。
- 以 master `d5575fa76406678cfa75be8bbaa33a4c108864b5` 为基线修复。主干迁移 3D V2 后移除了旧 CLI `xyq.scene3d.apply/query` 适配；本产物不注册旧入口，仍支持通过业务工厂创建 V2 节点，3D 内部编辑使用 Web。公开命令为 41 项。
- 保留现有媒体身份、事务、时间轴及 CLI 专用视频 `content.ratio` 类型校验。引用内容和派生连线在同一事务更新；时间轴命令补齐对白替换的 `dialogueId` 字段，并覆盖新增的 4 项时间轴操作。原生 `canvas apply --file` 不在本次 ratio 校验范围。
- 两次独立构建的 runtime、LEGAL 和校验文件逐字节一致；源文件指纹、锁文件摘要、构建环境及产物摘要记录在 [build-provenance.json](build-provenance.json)。此更新只替换固定 CLI 产物，不发布 registry SDK 包。
- runtime SHA-256: `1c53a5f869ee7c148e9f993e318d4ea5b34c5e93e2ac0749dd3f47ecff946e4f`
- LEGAL SHA-256: `c547c923eaf2e8c155f84aed934f42ad02855d669e8c0a890e38d049bc1ef8e7`
