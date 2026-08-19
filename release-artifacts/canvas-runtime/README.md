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

- Canvas SDK source commit: `353ede76463384b174253c7953b9ac95bf1b25c5`
- `infra/pnpm-lock.yaml` blob: `c732ee2aaab43c267ae512045fa51f8f5e0722f4`
- runtime SHA-256: `b41829890fa1b85e111d5d9a347662329252d45745458350296152973895e92f`
- LEGAL SHA-256: `52f130dead7e8eba4b1a277c6ed5c8a9778590f9b7e15d3214775bbee8304e78`
