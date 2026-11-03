# 账号 AIGC 水印设置

```bash
pippit-tool-cli watermark       # 查询当前账号
pippit-tool-cli watermark off   # 关闭 AIGC 水印
pippit-tool-cli watermark on    # 恢复保留 AIGC 水印
```

输出 `{"enabled":true}` 或 `{"enabled":false}`，`true` 表示保留 AIGC 水印。修改接口完成写入并回读后返回当前状态；不提交短剧 Run，不要求先解析剧本，不轮询生成进度。

设置作用于当前认证账号并与 Web 同步，不因个人/团队空间选择变成团队开关。仅在用户明确要求修改账号持久设置时执行；已有授权不重复确认。用户只要求本次成片无水印时，应先确认是否接受修改账号默认设置。

品牌水印及最终下载策略继续由服务端决定，不能推断所有视频都无任何水印，也不替换下载地址来绕过策略。修改报错或结果未知时先用无参数查询确认，不自动重写；无法查询则如实反馈。
