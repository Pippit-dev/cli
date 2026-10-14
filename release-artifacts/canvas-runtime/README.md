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

- SDK source base: `d1a1915fee83c8f54bcfc43ed0abfb15e85771d1`。
- 保留现有 CLI runtime 的事务与时间轴修复：`0fadc755ad4e5dd75dcadb090c89fe892519223d`、`2dfe29a254b2f559188dfdbe2cf2479c26024cc9`。
- 在该基线上回移媒体身份解析、写入校验与字段说明；完整源码固定为 `56b4714ec8dd2b370f2d0170e49c042090c43abb`（`codex/vg20-cli-compatible`）。该提交已推送至 SDK 源码仓库，尚未发布 registry package。
- 源文件指纹、构建依赖来源、产物大小及摘要记录在 [build-provenance.json](build-provenance.json)。主线 SDK 修复单独合入；此兼容构建保留当前 CLI 的命令和历史行为。
- runtime SHA-256: `c112319809da8374e0abfbe495cac10cfdb4bf7b90d72493061c47c2b9cef586`
- LEGAL SHA-256: `e915ba38d7f96e9fc85f584bacaafaaad0b7a0d02b5746d9eea57ec626ef12da`
