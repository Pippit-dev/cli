# 图片超分与视频抠像

适用于明确处理已有图片或视频；先检查 `image-super-resolution --help` 或 `video-matting --help`。旧 CLI 不含命令时按安装说明更新；API 未开放时报告原错误，不改用普通聊天猜工具。

先上传素材，保存输出的 `asset_id`；以下 `ASSET_ID`、`THREAD_ID`、`RUN_ID` 都替换为真实输出。示例使用个人空间 `0`；团队操作从上传到查询统一使用实际团队 ID。

```bash
pippit-tool-cli upload-file --path ./source.png --team-id 0
pippit-tool-cli image-super-resolution --asset-id ASSET_ID --team-id 0

pippit-tool-cli upload-file --path ./source.mp4 --team-id 0
pippit-tool-cli video-matting --asset-id ASSET_ID --team-id 0

pippit-tool-cli query-result --thread-id THREAD_ID --run-id RUN_ID --download-dir ./results --team-id 0
```

- 图片可选 `--quality LQ|MQ|HQ`；省略时服务端默认 MQ。无需指定生成模型、比例或分辨率。
- 视频可选 `--background-color black|green`；省略时服务端默认 black。产物为纯色背景 MP4，不含透明通道。
- `--asset-id` 只接收已上传资产 ID，不能传文件路径或 URL。跨空间素材由服务端拒绝；本地有记录时会提前检查空间冲突。
- 两个命令支持 `--source`，由宿主按可信运行环境填写稳定平台名。未传时沿用通用来源检测。

## 提交与恢复

每次执行只提交一次。发起网络请求前，stderr 输出 `submitting` 和 `team_id/thread_id/run_id`，请保留这些标识；stdout 返回本次提交结果。

- `submitted`：服务端返回相同任务标识；后续按共用异步交付流程查询。
- `rejected`：展示 `error_message` 与可用 `log_id`，按错误处理。
- `submission_unknown`：网络失败、响应缺失或任务标识不一致，不能认定生成失败。保存已知标识并查询原任务，不重提、不自动换工具。
- 进程退出或 stdout 丢失时，使用 stderr 的原标识、原账号、原 API origin 和原空间恢复 `query-result`。查询失败仍不代表生成失败。
- 查询的 `running` 继续轮询；`input_required` 停下来展示需要交互的状态；`failed`、`canceled`、`interrupted` 报告原结果；`unknown` 说明查询未能确认状态。

查询成功后按 [异步结果与媒体交付](../workflows/async-delivery.md) 展示下载到的真实媒体。独立结果下载不创建派生画布节点、不回填节点或连边。接口上线、个人/团队真实提交与跨空间拒绝均需单独验收，不能用本地测试替代。
