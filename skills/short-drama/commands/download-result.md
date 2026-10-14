# download-result：下载会话产物

```bash
pippit-tool-cli download-result --url "DOWNLOAD_URL" --output-path "FILE_PATH" --updated-at UPDATED_AT
```

| 参数 | 必填 | 含义 |
| --- | --- | --- |
| `--url` | 是 | 同一文件对象的 download_url |
| `--output-path` | 是 | 完整目标文件路径，含文件名，不是父目录 |
| `--updated-at` | 否 | 同一文件对象的更新时间，Unix 秒；缺失时省略 |
| `--workers` | 否 | 正整数，默认 5；每次命令仍接收一个 URL |

在宿主允许的任务工作目录执行，保留 list-thread-file 返回的完整相对 file_path 和文件名。默认只下载 `ai_script/script_outline.json`、`ai_script/episode_<episode_id>.json`、兼容带 `asset/` 前缀的同名 AI 剧本文件，以及 `.mp4`、`.jpg`、`.jpeg` 保留产物；用户明确指定非保留文件时，先说明它不属于默认交付范围，再按本命令的路径安全规则处理。判断保留范围时沿用 list-thread-file 的路径规范化规则。执行前确认解析后仍位于该任务目录内；路径越界、含可疑跳转、目标不属于当前任务，或非保留目标未被用户明确指定时停止报告，不直接写入。此处是路径范围检查，不用自行判断同名文件是否应跳过；下载工具决定复用或更新。

“查看角色/场景/片段内容”不视为明确下载内部文件的请求，按 [业务内容查询](../workflows/creation.md#业务内容查询) 处理。即使用户明确要求下载内部文件，也只按文件请求交付，不将其作为当前项目状态的替代来源。

成功 stdout 为 JSON。新下载结果例如：

```json
{"output_path":"./THREAD_ID/ai_script/episode_1.json","downloaded":["./THREAD_ID/ai_script/episode_1.json"]}
```

为避免下载失败覆盖已取得文件，首次下载和新版下载使用任务目录内独立的下载批次子目录，并在其下保留完整相对 file_path；网络失败重试复用该目录；若校验失败且 CLI 因 `already_exist` 复用坏文件，在原重试预算内改用新的批次目录，保留问题文件，不无限换目录重试。校验通过后才将交付记录指向新路径，保留旧版本。不要删除旧文件来强迫重新下载。

复用已有文件时检查 `already_exist`（不是 already_exists），不要把它算作本次新下载。已有文件修改时间不早于 updated_at 会跳过；缺少有效 updated_at 时也可能复用已有文件，不证明内容已更新。需要新版本却缺乏依据时报告该限制，不擅自删除旧文件。

download_url 为空时记录待链接，不调用下载命令。工具仍返回运行中进程或会话 ID 时，等待同一调用完成后再解析 stdout，不将空的中间输出当作 JSON 结果或另起重复下载。

检查退出码和响应中的 errors；失败信息可能只在 stderr，不保证失败时有 JSON。命令内部已对可重试网络错误进行重试，外层恢复上限见 [轮询与文件交付](../workflows/poll-and-deliver.md)。

下载后核对实际 output_path 存在、非空且位于当前任务目录；JSON 须能解析，图片/视频通过可用的媒体预览或格式检查确认可读，不把错误页当媒体交付。校验失败按下载失败处理，不覆盖已校验版本；缺少校验能力时说明未核实项。再通过宿主交付真实文档附件或可预览媒体。部分失败不妨碍交付其他已确认产物；不能只报告路径或裸 URL 就宣称交付完成。
