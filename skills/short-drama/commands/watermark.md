# 账号 AIGC 水印设置

先用 `pippit-tool-cli watermark --help` 检查命令。缺失时按[安装说明](../scripts/install.md)通过宿主升级，独立 CLI 可执行 `update`；改用升级后的绝对路径重查。

```bash
pippit-tool-cli watermark       # 查询
pippit-tool-cli watermark off   # 关闭
pippit-tool-cli watermark on    # 保留
```

返回 `{"enabled":true}` 或 `{"enabled":false}`，`true` 表示保留。设置后返回服务端回读状态。

设置与 Web 同步，按当前账号持续生效，与团队选择无关。用户明确要求修改账号设置时才执行 `on/off`；只要求本次视频无水印时，先确认是否接受修改账号默认值。

此开关控制 AIGC 水印，品牌水印与最终下载权益由服务端决定。修改报错或结果未知时先查询确认。
