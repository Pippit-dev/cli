# generate-video：生视频

适用于文生视频、参考图/视频/音频生成新视频以及首尾帧生成。仅处理已有视频的清晰度或字幕时，分别使用 [超分](video-super-resolution.md)、[擦字幕](erase-video-subtitle.md)。

## 输入与参数

| 参数 | 必填 | 规则 |
| --- | --- | --- |
| `--prompt` | 是 | 用户原始描述，不能全为空白 |
| `--model` | 是 | 用户指定的准确模型枚举；未明确模型时先确认，不猜测 |
| `--image` | 否 | 本地图片路径，重复参数 |
| `--video` | 否 | 本地参考视频路径，重复参数 |
| `--audio` | 否 | 本地 `.mp3/.wav` 音频路径，重复参数 |
| `--duration` | 按模型 | 整数秒；MiniMax、Wan、HappyHorse 可省略使用下表默认值，其他模型需明确提供；用户只给时长范围时先确认具体秒数 |
| `--ratio` | 按模式 | 比例字符串，不转换为生图枚举；MiniMax、Wan、HappyHorse 可省略使用服务端配置默认值；其他模型需明确提供 |
| `--resolution` | 按模型 | MiniMax、Wan、HappyHorse 可省略使用下表默认值；其他模型需明确提供，如 `720p`、`1080p` |
| `--generate-type` | 否 | 首尾帧任务传 `1`；其他显式值交服务端处理 |

支持的模型枚举：`Seedance_2.0_mini_lite`、`seedance2.0_vision`、`seedance2.0_fast_vision`、`Seedance_2.0_mini`、`Seedance_2.5`、`MiniMax-H3`、`MiniMax-H3-Max`、`wan3.0`、`happyhorse-1.1`。该列表仅用于选择提示，不自行新增本地模型或分辨率组合校验。

本地图片支持 `.jpg/.jpeg/.png/.gif/.bmp/.webp/.svg`；视频支持 `.mp4/.avi/.mov/.wmv/.flv/.webm/.mkv/.m4v`。CLI 内部上传参考素材。

## MiniMax 模型

| 能力 | `MiniMax-H3` | `MiniMax-H3-Max` |
| --- | --- | --- |
| 分辨率 | `768p`、`2k`；默认 `768p` | `480p`、`768p`；默认 `768p` |
| 生成时长 | 整数 4–15 秒；默认 10 秒 | 整数 5–15 秒；默认 10 秒 |
| 固定比例 | `21:9`、`16:9`、`4:3`、`1:1`、`3:4`、`9:16` | `16:9`、`4:3`、`1:1`、`3:4`、`9:16`；不允许 `21:9` |
| 文生视频 | 支持；省略比例默认 `9:16`，不能用 `adaptive` | 支持；省略比例默认 `16:9`，不能用 `adaptive` |
| 普通参考图 | 支持 | 不支持 |
| 首帧／首尾帧 | 传 `--generate-type 1` | 同左；单图也必须指定此模式 |
| 视频参考 | 支持，单条 2–15 秒，总时长不超过 15 秒 | 不支持 |
| 音频参考 | 必须同时有参考图片或视频；Agent 继续执行自身限制 | 不支持 |

以上是当前服务端配置对应的能力，实际以请求时返回的配置为准。图片单张不超过 30MB。两模型首尾帧模式都不能混用参考视频或音频。H3 带视觉素材时可显式使用 `adaptive`，省略比例仍取配置默认值；Max 首尾帧模式统一使用 `adaptive`；即使传入固定比例，服务端也会自动改为自适应比例。`720p` 是兼容输入，会映射为实际输出 `768p`；新调用优先直接使用 `768p`。当前 H3 配置未提供音频独立数量和时长上限，API 不额外硬编码这些限制；CLI 不限制素材数量，Agent 校验仍生效。

```bash
pippit-tool-cli generate-video --prompt "用户原始描述" --model MiniMax-H3 --ratio 16:9
pippit-tool-cli generate-video --prompt "用户原始描述" --model MiniMax-H3-Max --generate-type 1 --image FIRST_FRAME_PATH --resolution 768p --duration 10
```

## Wan 3.0 与 HappyHorse 1.1

两模型均必须提供 `--prompt`。素材通过现有 `--image`、`--video`、`--audio` 参数上传，是否支持由服务端校验；本次不提供文件或网页链接输入。

| 参数 | `wan3.0` | `happyhorse-1.1` |
| --- | --- | --- |
| 分辨率 | `480p`、`720p`、`1080p` | `720p`、`1080p` |
| 生成时长 | 整数 4–30 秒，不支持 `-1` | 整数 3–15 秒 |
| 比例 | `adaptive`（智能）、`16:9`、`9:16`、`4:3`、`3:4`、`1:1` | `16:9`、`21:9`、`9:16`、`4:3`、`3:4`、`1:1` |
| 模式 | 全能参考；首尾帧传 `--generate-type 1` | 文生视频或图片参考；不使用首尾帧模式 |

省略分辨率、时长、比例时使用服务端配置默认值。当前配置两模型均默认 `720p`、10 秒；Wan 默认 `adaptive`，HappyHorse 默认 `16:9`。Wan、HappyHorse 的 `720p` 不转换为 `768p`。HappyHorse 当前不支持视频或音频参考。实际规则以请求时的生效配置及服务端校验为准。

```bash
pippit-tool-cli generate-video --prompt "用户原始描述" --model wan3.0 --duration 10 --resolution 720p --ratio 16:9
pippit-tool-cli generate-video --prompt "用户原始描述" --model happyhorse-1.1 --duration 10 --resolution 720p --ratio 16:9
```

## 最小调用

```bash
pippit-tool-cli generate-video --prompt "用户原始描述" --model seedance2.0_vision --duration 5 --ratio 16:9 --resolution 720p
```

首尾帧场景：明确两张图片的角色，按首帧、尾帧顺序传两次 `--image`，固定传 `--generate-type 1`。MiniMax 只给首帧时也可传一张图片，仍需 `--generate-type 1`。缺少图片、角色不清或无法确定原始描述时先询问。完整示例见 [首尾帧生视频](../examples/first-last-frame.md)。

## 返回与处理

成功返回 `thread_id`、`run_id`、`web_thread_link`，继续 [异步结果与媒体交付](../workflows/async-delivery.md)。素材、权限或参数失败时说明原因，不自动降低用户指定的模型、分辨率或删减素材。无法确认提交是否成功时，不重复生成。
