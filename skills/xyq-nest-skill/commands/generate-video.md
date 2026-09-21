# generate-video：生视频

适用于文生视频、参考图/视频/音频生成新视频以及首尾帧生成。仅处理已有视频的清晰度或字幕时，分别使用 [超分](video-super-resolution.md)、[擦字幕](erase-video-subtitle.md)。

## 输入与参数

| 参数 | 必填 | 规则 |
| --- | --- | --- |
| `--prompt` | 是 | 用户原始描述，不能全为空白 |
| `--model` | 否 | 用户指定的模型；未提供时省略，由服务端处理默认配置 |
| `--image` | 否 | 本地图片路径，重复参数 |
| `--video` | 否 | 本地参考视频路径，重复参数 |
| `--audio` | 否 | 本地 `.mp3/.wav` 音频路径，重复参数 |
| `--duration` | 否 | 整数秒；用户只给时长范围时先确认具体秒数 |
| `--ratio` | 否 | 比例字符串，如 `9:16`、`16:9`、`3:4`、`4:3`；不转换为生图枚举 |
| `--resolution` | 否 | 用户指定值，如 `720p`、`1080p` |
| `--generate-type` | 否 | 首尾帧任务传 `1`；其他显式值交服务端处理 |

普通用户模型为 `Seedance_2.0_mini_lite`；VIP 模型包括 `seedance2.0_vision`、`seedance2.0_fast_vision`、`Seedance_2.0_mini`、`Seedance_2.5`。该列表仅用于选择提示，以当前帮助和服务端为准，不自行新增模型或分辨率组合校验。

本地图片支持 `.jpg/.jpeg/.png/.gif/.bmp/.webp/.svg`；视频支持 `.mp4/.avi/.mov/.wmv/.flv/.webm/.mkv/.m4v`。CLI 内部上传参考素材。

## 最小调用

```bash
pippit-tool-cli generate-video --prompt "用户原始描述"
```

首尾帧场景：明确两张图片的角色，按首帧、尾帧顺序传两次 `--image`，固定传 `--generate-type 1`。缺少图片、角色不清或无法确定原始描述时先询问。完整示例见 [首尾帧生视频](../examples/first-last-frame.md)。

## 返回与处理

成功返回 `thread_id`、`run_id`、`web_thread_link`，继续 [异步结果与媒体交付](../workflows/async-delivery.md)。素材、权限或参数失败时说明原因，不自动降低用户指定的模型、分辨率或删减素材。无法确认提交是否成功时，不重复生成。
