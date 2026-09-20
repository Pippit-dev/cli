# generate-audio：生成音频

将音频模型参数提交给服务端。模型和参数组合由服务端校验，CLI 不维护模型白名单，不自动切换模型、补充模型专属配置或修改用户原始描述。通用 JSON 能发送一个字段，不代表服务端已经支持它；未定义字段可能被当前服务端协议忽略。

用户想给视频增加背景音乐时，先确定要生成音频素材还是编辑现有视频；本命令仅返回音频任务，不自动合成视频。参考图的上传和提交链路已接通，但尚未通过真实音频生成验收，不能承诺稳定可用；最终以原任务查询结果为准。

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

已有引用与本地素材组合时，把完整有序 references 写入参数文件，再追加本地素材。只有用户确实要求这些参考，且目标模型/模式支持该组合时才提交。

## 返回与处理

成功提交返回 JSON 中的 `thread_id`、`run_id`、`web_thread_link`，随后执行 [异步结果与媒体交付](../workflows/async-delivery.md)。最终音频位于 `audios[]`，逐项交付 output_path 对应文件；提交成功不是生成成功。请求时间戳不保证查询命令返回独立字幕文件，实际 duration 不等于精确时长控制能力。

模型不支持、引用非法、鉴权或生成失败时说明真实错误，不改成视频请求、不自动切换模型、不重复提交未知结果的任务。查询报错与已确认的失败终态按 [查询契约](query-result.md) 区分。
