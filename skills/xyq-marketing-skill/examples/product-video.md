# 商品图到营销视频

用户示例：“用这张保温杯商品图做一个 15 秒竖屏达人带货视频，不要字幕，用 Seedance 2.0 VIP，1080p。”

1. 先检查 `pippit-tool-cli marketing --help` 与 `pippit-tool-cli status`；未登录时执行 `pippit-tool-cli login` 并等待浏览器授权成功，无需用户提供 token。随后上传用户给出的真实文件：

   ```bash
   node scripts/marketing.js upload --file /path/to/cup.png
   ```

2. 将返回的 `data.pippit_asset_id` 填入 UTF-8 请求文件。下面的 `ACTUAL_UPLOADED_ASSET_ID` 是占位说明，不能原样提交；不另补用户没选的模型或分辨率。

   ```json
   {
     "message": "用这张保温杯商品图做一个 15 秒竖屏达人带货视频，不要字幕，用 Seedance 2.0 VIP，1080p。",
     "asset_ids": ["ACTUAL_UPLOADED_ASSET_ID"],
     "general_agent_settings": {
       "ratio": 3,
       "duration_start": 15,
       "duration_end": 15,
       "show_subtitle": false,
       "video_model": "seedance2.0_vision",
       "video_resolution": "720p"
     }
   }
   ```

3. 预览后提交；用户已经要求生成，执行时不再索要相同授权。

   ```bash
   node scripts/marketing.js generate --request /path/to/request.json --dry-run
   node scripts/marketing.js generate --request /path/to/request.json --execute
   ```

4. 保存真实响应中的 thread/run ID，展示返回的网页链接，然后等待和下载：

   ```bash
   node scripts/marketing.js query --thread-id ACTUAL_THREAD_ID --run-id ACTUAL_RUN_ID --wait --output-dir /path/to/results
   ```

5. 若查询退出码为 4、`run_state=6/9`，打开提交时返回的网页查看待确认项，沿用用户已有授权；缺少必要选择时再问用户。确认后取同一 thread 的最新 run_id，重新执行查询命令，不重发生成请求，也不再查已中断的旧 Run。

6. 读取最终 `run_state` 和 `downloaded_files`，确认视频可打开并将实际本地文件作为媒体交付。用户只要“看之前那条生成好了没”时，从第 4 步开始，不能重新上传或提交。

错误、空结果和超时按 [完成与恢复规则](../SKILL.md#completion-and-recovery) 处理。
