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
- 在 `add55d3a40cb52b2a4ce0dbc24533f7ba7000bb1` 的媒体身份修复上，仅向 CLI command runtime 回灌视频 `content.ratio` 类型校验；完整源码固定为 `120c5e3d386d15d9a48673aaeb4a691cda5fbf39`（`codex/cli-video-ratio-runtime`）。合法的 `generation.ratio` 字符串、节点默认比例、历史坏数据的无关编辑和显式修复保持原有合同。原生 `canvas apply --file` 不在本次 ratio 校验范围。
- 源文件指纹、构建依赖来源、产物大小及摘要记录在 [build-provenance.json](build-provenance.json)。主线 SDK 修复单独合入；此兼容构建保留当前 CLI 的命令和历史行为。
- runtime SHA-256: `8e14b9a2c496491c2ee44da3ed950ac8301b67981dfaba3ab1e042cf8b937298`
- LEGAL SHA-256: `3b97217c41dfb3c298185c80b1e70c2b469f552450e53d00fc1dcf7098740ffb`
