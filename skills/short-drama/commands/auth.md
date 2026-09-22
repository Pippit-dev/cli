# 登录授权：status / login / logout

业务调用前先检查状态，不在每轮轮询中重复登录：

```bash
pippit-tool-cli status
```

stdout 为 JSON，读取 `logged_in`、`source` 及存在时的 `uid`、`expires_at`。未登录也可能退出 0，不能只看退出码。`logged_in=true` 只表示 CLI 找到了本地可用凭据，不保证服务端未撤销或已开放短剧权限。

未登录时运行：

```bash
pippit-tool-cli login
```

命令拉起浏览器并阻塞等待授权；仅打开网页不是成功。等待 CLI 保存凭据并成功返回，再独立执行 status 确认。默认等待 5 分钟；拒绝或关闭页面可能直到超时才结束，失败后不继续受保护操作。不要要求用户复制密钥，也不让用户回终端按回车作为授权完成信号。

豆包管理授权时使用已暴露的登录流程，避免同时启动另一个 login；授权成功并检查状态后继续原任务，不要求用户重发需求。平台未提供授权衔接能力时，按上述 CLI 流程执行；无浏览器交互且无可用凭据则报告阻塞，不臆造宿主工具。

退出或切换账号：

```bash
pippit-tool-cli logout
```

`logged_out=true` 表示清除了本机浏览器凭据；`remote_credential_preserved=true` 表示远端 Access Key 未撤销。切换账号需退出后重新登录，使用新授权页，不刷新旧页。

`XYQ_ACCESS_KEY` 是优先于网页登录的显式环境覆盖，无效时不会静默回退；logout 不清环境变量，`environment_still_active=true` 时覆盖仍生效。先修正或取消错误覆盖，不擅自切换账号。浏览器凭据被拒绝时可用 `pippit-tool-cli login --force` 轮换，不作为例行步骤。

不展示、回显或把凭据写入文档和命令参数。向用户展示或共享错误前检查并隐藏其中的凭据及敏感签名参数；这不代表 CLI 日志已有统一脱敏。
