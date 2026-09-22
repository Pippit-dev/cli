# 示例：新建短剧到交付

用户：“帮我写一个都市悬疑短剧开头。”

按 [入口](../SKILL.md) 检查安装和登录，并读取 [创作流程](../workflows/creation.md)、[轮询交付流程](../workflows/poll-and-deliver.md)。原样提交：

```bash
pippit-tool-cli short-drama +submit-run --message "帮我写一个都市悬疑短剧开头。"
```

保存真实 thread_id、run_id、web_thread_link，展示并按宿主能力打开链接。把下列占位符替换为真实值，并行查询：

```bash
pippit-tool-cli get-thread --thread-id THREAD_ID --run-id RUN_ID
pippit-tool-cli list-thread-file --thread-id THREAD_ID --page-num 1 --page-size 200
```

读取会话可读文本；有问题就等待用户回复，不自行写一个答案。发现剧本等文件时用同一文件对象的真实字段下载：

```bash
pippit-tool-cli download-result --url "DOWNLOAD_URL" --output-path "FILE_PATH" --updated-at UPDATED_AT
```

没有 updated_at 时省略该参数。确认实际 output_path 后通过宿主发送剧本附件；产生图片/视频时也逐项展示。后端仅返回文本且没有文件时，展示真实文本，不编造附件。查询时限、错误恢复和最终完成判断按共用流程执行。
