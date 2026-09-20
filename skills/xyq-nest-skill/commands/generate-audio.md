# generate-audio：生成音频

将音频模型参数提交给服务端。模型和参数组合由服务端校验，CLI 不维护模型白名单，不自动切换模型、补充模型专属配置或修改用户原始描述。通用 JSON 能发送一个字段，不代表服务端已经支持它；未定义字段可能被当前服务端协议忽略。

用户想给视频增加背景音乐时，先确定要生成音频素材还是编辑现有视频。结果按模式可包含音频和视频，例如 dubbing 可返回翻配视频；普通音频生成不会自动把产物配回任意源视频。参考图的上传和提交链路已接通，但尚未通过真实音频生成验收，不能承诺稳定可用；最终以原任务查询结果为准。

## 已验证范围

以下为已执行真实用例的结果，不是模型白名单，也不代表全部输入和参数组合均可用。模型到生成服务的映射由服务端配置决定，CLI 原样发送用户的 model。

| 用例 | 实际结果 |
| --- | --- |
| `seedaudio_1.0`，JSON 参数 | 成功生成并下载 24 kHz WAV；三个 rate 的显式 0 和 enable_timestamp=false 在下游保留 |
| `seedaudio_1.5`，`task_type=reference` | 成功生成并下载 24 kHz、3 秒 WAV |
| `seedaudio_1.5`，`task_type=separate` | 成功生成并下载两份 3 秒 WAV；返回的单轨 duration 均为 6 秒，与文件实际时长不一致 |
| 未知模型；separate 缺少 prompt | 服务端明确拒绝，CLI 不改模型、不补造 prompt |
| `seedaudio_1.5`，`task_type=dubbing` | 使用符合模式要求的 6 秒、864×480、24 fps MP4，翻配英语并请求 video_url；成功返回并下载 1 个音频和 1 个视频 |
| `seedaudio_1.0`，参考图 | 用例失败，尚未取得成功生成结果；不能据此断言所有参考图必然失败 |

reference 和 separate 的文件时长来自实际下载文件，不是精确时长控制承诺。separate 返回的单轨 duration 当前可能使用请求总时长，不能作为可靠的单轨时长；这个已知问题尚未修复，应以实际媒体文件为准。dubbing 早期失败用例的素材不符合目标模式要求；后续合规素材用例成功，不代表任意视频均可翻配。无 prompt 的参数对象能被 CLI 发送，也不等于目标模式允许省略 prompt。

## 输入与参数

| 参数 | 规则 |
| --- | --- |
| `--input` | 单个 `audio_part_tool_param` JSON 对象，与 `--file` 互斥 |
| `--file` | JSON 文件路径，或 `-` 从 stdin 读取；与 `--input` 互斥 |
| `--prompt` | 用户原始描述，原样写入 prompt；与 JSON prompt 冲突 |
| `--model` | 原样写入 model，由服务端判断支持情况；与 JSON model 冲突 |
| `--audio` / `--image` / `--video` | 本地可读文件路径，可重复；按命令行顺序追加到 JSON references 后面 |
| `--format` | 旧 `audio_config.format`，不转换大小写或校验格式枚举 |
| `--sample-rate` | 旧 `audio_config.sample_rate`，接受 int32 |
| `--speech-rate` / `--loudness-rate` / `--pitch-rate` | 旧 audio_config 对应字段，只检查有限数值，不复制模型数值范围 |
| `--enable-timestamp` | 旧 `audio_config.enable_timestamp`；显式 false 会保留 |

只传用户指定的可选配置。不用 JSON 且未显式提供 model 时，历史兼容默认值为 `seedaudio_1.0`，不是白名单；显式 model 原样发送，不修剪或替换空字符串。使用 `--input` 或 `--file` 时不补默认模型，不补 prompt/text；缺省语义由服务端决定。

旧配置 flags 不会按模型自动转换成 `audio_config_v2` 或 `output_format`。V2 配置、任务模式、翻配参数、输出选择等通过 JSON 的 `audio_config_v2`、`output_format`、`task_type`、`dubbing_config`、`include` 等字段表达。取值来自用户或已核实的服务端契约，不能从其他模型照搬；不要把透传能力描述成所有网页端模式均已支持或所有配置已验证。

JSON 必须是单个对象，最多 64 MiB，不能有重复键或尾随内容。对象只放音频参数，不含外层 agent_name/message/audio_part_tool_param 包装、鉴权、团队或请求头。CLI 固定外层 agent 身份，用非空 prompt、text 或 task_type 生成 message；没有文本时使用中性任务描述，不写回 JSON 参数。

## 合并与参考素材

- 同一个字段同时由 JSON 和显式 flag 提供时直接报错，不比较值、不覆盖。旧配置 flags 可补充 audio_config 中不存在的字段；audio_config 不是对象时不能合并。
- JSON references 保留原始顺序和字段；已有 `pippit_asset_id`、`speaker` 直接通过 JSON 提供，不重新上传。speaker 是引用项字段，不是本地文件路径。
- `--audio`、`--image`、`--video` 上传后生成 type/pippit_asset_id 引用，按命令行出现顺序追加。CLI 不去重、不删引用；需要追加时，JSON references 必须是数组。
- 所有本地文件均在首次上传前检查可读性。远程 URL 不能代替本地路径。素材类型、数量、混用和 speaker 组合是否合法由服务端判断，CLI 不复制前端常量。

## 最小调用

传统调用保留默认模型，只发送用户给出的配置：

```bash
pippit-tool-cli generate-audio --prompt "用户原始描述" --audio "./reference.wav" --format wav --sample-rate 24000
```

JSON 直接表示音频参数对象，可与不冲突的便捷 flags 组合：

```bash
pippit-tool-cli generate-audio --input '{"model":"seedaudio_1.0","audio_config":{"format":"wav"}}' --prompt "用户原始描述"
pippit-tool-cli generate-audio --file "./audio-params.json"
pippit-tool-cli generate-audio --file - < "./audio-params.json"
```

`seedaudio_1.5` 的 reference 模式使用 V2 参数，例如：

```bash
pippit-tool-cli generate-audio --input '{"model":"seedaudio_1.5","task_type":"reference","prompt":"生成约3秒轻柔鸟鸣。","output_format":"wav","audio_config_v2":{"sample_rate":24000,"speech_rate":0,"loudness_rate":0,"pitch_rate":0},"watermark":false}'
```

英语翻配并请求视频产物，可将 JSON 参数与本地视频组合：

```bash
pippit-tool-cli generate-audio --input '{"model":"seedaudio_1.5","task_type":"dubbing","dubbing_config":{"target_language":"en"},"include":["video_url"]}' --video "./source.mp4"
```

当前 `seedaudio_1.5` 视频输入要求：时长 4–360 秒，宽和高均为 300–6000 像素，宽×高为 407696–2086876 像素，宽高比 0.4–2.5，帧率 12–60 fps，格式为 MP4 或 MOV。这些是当前服务模式的素材要求，不是 CLI 硬编码的白名单；CLI 不复制这些限制，也不会修改视频来绕过服务端校验。

已有引用与本地素材组合时，把完整有序 references 写入参数文件，再追加本地素材。只有用户确实要求这些参考，且目标模型/模式支持该组合时才提交。

## 返回与处理

成功提交返回 JSON 中的 `thread_id`、`run_id`、`web_thread_link`，随后执行 [异步结果与媒体交付](../workflows/async-delivery.md)。音频位于 `audios[]`，视频位于 `videos[]`，逐项交付 output_path 对应文件；提交成功不是生成成功。请求时间戳不保证查询命令返回独立字幕文件，实际 duration 不等于精确时长控制能力。

模型不支持、引用非法、鉴权或生成失败时说明真实错误，不改成视频请求、不自动切换模型、不重复提交未知结果的任务。查询报错与已确认的失败终态按 [查询契约](query-result.md) 区分。
