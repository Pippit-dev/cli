# generate-audio：生成音频

使用 Seed Audio 1.0 生成音频，可以提供参考音频或参考图。用户想给视频增加背景音乐时，先确定要生成音频素材还是编辑现有视频；本命令仅返回音频任务，不自动合成视频。

## 输入与参数

| 参数 | 必填 | 规则 |
| --- | --- | --- |
| `--prompt` | 是 | 用户原始描述，不能全为空白；在这里完整描述生成内容 |
| `--model` | 否 | 默认且仅支持 `seedaudio_1.0` |
| `--audio` | 否 | 本地参考音频路径，最多重复 3 次，保持顺序；不能与 `--image` 混用 |
| `--image` | 否 | 本地参考图路径，至多 1 张；不能与 `--audio` 混用 |
| `--format` | 否 | `mp3`、`wav`、`pcm`、`ogg_opus` |
| `--sample-rate` | 否 | 采样率，单位 Hz，正整数 |
| `--speech-rate` / `--loudness-rate` / `--pitch-rate` | 否 | Seed Audio 1.0 的语速、响度、音调参数，必须为有限数值，具体范围由服务端决定 |
| `--enable-timestamp` | 否 | 请求时间戳；显式关闭可用 `--enable-timestamp=false` |

只传用户指定的可选配置；缺省交给服务端。不能套用新模型的参数范围，不提供独立 `--text`、精确时长、分轨、翻配或其他音频模型能力，也不通过填入图片、视频模型来提交音频。

音频文件后缀支持 `.mp3/.wav/.m4a/.aac/.flac/.ogg/.opus`，图片支持 `.jpg/.jpeg/.png/.gif/.bmp/.webp/.svg`。素材由 CLI 上传，实际可用性由服务端检查。远程 URL 不能替代本地路径。

## 最小调用

```bash
pippit-tool-cli generate-audio --prompt "用户原始描述"
```

如果用户提供了参考音频、输出格式与采样率：

```bash
pippit-tool-cli generate-audio --prompt "用户原始描述" --audio "./reference.wav" --format wav --sample-rate 24000
```

## 返回与处理

成功返回 JSON 中的 `thread_id`、`run_id`、`web_thread_link`，随后执行 [异步结果与媒体交付](../workflows/async-delivery.md)。最终音频位于查询结果的 `audios[]`，逐项交付 `output_path` 对应文件。请求时间戳不保证查询命令返回独立字幕文件。

模型不支持、引用非法、鉴权或生成失败时说明真实错误，不改成视频请求、不自动切换模型、不重复提交未知结果的任务。
