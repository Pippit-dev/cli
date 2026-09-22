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

在宿主允许的任务工作目录执行，保留 list-thread-file 返回的完整相对 file_path 和文件名。执行前确认解析后仍位于该任务目录内；路径越界、含可疑跳转或目标不属于当前任务时停止报告，不直接写入。此处是路径范围检查，不用自行判断同名文件是否应跳过；下载工具决定复用或更新。

成功 stdout 为 JSON。新下载结果例如：

```json
{"output_path":"./THREAD_ID/scripts/episode.txt","downloaded":["./THREAD_ID/scripts/episode.txt"]}
```

复用已有文件时检查 `already_exist`（不是 already_exists），不要把它算作本次新下载。已有文件修改时间不早于 updated_at 会跳过；缺少有效 updated_at 时也可能复用已有文件，不证明内容已更新。需要新版本却缺乏依据时报告该限制，不擅自删除旧文件。

检查退出码和响应中的 errors；失败信息可能只在 stderr，不保证失败时有 JSON。命令内部已对可重试网络错误进行重试，外层恢复上限见 [轮询与文件交付](../workflows/poll-and-deliver.md)。

下载后核对实际 output_path 存在且非空，再通过宿主交付真实文档附件或可预览媒体。部分失败不妨碍交付其他已确认产物；不能只报告路径或裸 URL 就宣称交付完成。
