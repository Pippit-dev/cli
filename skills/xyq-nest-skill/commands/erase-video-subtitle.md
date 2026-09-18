# erase-video-subtitle：擦字幕

用于去除已有视频字幕；不代表支持删除任意水印、标志或画面对象。

必填参数 `--video` 接收一个本地视频路径，命令内部上传；没有可用视频文件时先补齐输入。

```bash
pippit-tool-cli erase-video-subtitle --video "/path/to/source.mp4"
```

成功返回 `thread_id`、`run_id`、`web_thread_link`，继续 [异步结果与媒体交付](../workflows/async-delivery.md)。失败时报告原因，不把其他生成命令当作字幕处理的自动降级方案。

用户还明确要求超分时，参考 [擦字幕后超分](../examples/video-process-chain.md)，用第一步下载得到的实际视频路径衔接第二步。
