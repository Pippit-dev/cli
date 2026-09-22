# 安装与命令检查

优先使用插件平台或宿主已安装的 `pippit-tool-cli`；已可用时不重新安装、不每次获取最新版本。同一任务使用同一命令路径，若宿主给出绝对路径，用带引号的路径替换文档中的命令名；PowerShell 调用绝对路径时使用 `&`。

首次使用或安装路径、版本变化后检查以下命令。帮助不提交业务任务，不证明账号权限或后端短剧能力已开放。

```bash
pippit-tool-cli --version
pippit-tool-cli status --help
pippit-tool-cli login --help
pippit-tool-cli logout --help
pippit-tool-cli short-drama +submit-run --help
pippit-tool-cli short-drama +upload-file --help
pippit-tool-cli get-thread --help
pippit-tool-cli list-thread-file --help
pippit-tool-cli download-result --help
```

插件平台管理安装时：缺少 CLI 或必需命令，就使用平台实际提供的安装/升级流程，完成后重新检查并保留原任务上下文。没有相应能力则报告安装阻塞，不悄悄改用另一份缓存 CLI 或其它 API。

用户自行安装的本地环境可以运行：

```bash
npm install -g @pippit-dev/cli@latest
```

npm 入口声明 Node.js 16+；安装/升级还需 npm、全局目录写权限、访问 npm 和 GitHub Releases 的网络、curl，以及 macOS/Linux 的 tar 或 Windows 的 PowerShell。无需 Go 编译器或 Python。默认安装会同时安装全局 Skills，`pippit-tool-cli update` 也会更新它们；不能当成无副作用的例行检查。升级成功后只恢复原任务，不重复创作提交。

安装或升级一次后仍失败、命令仍缺失时停止，说明失败环节，不循环重装。版本和普通命令可能触发 npm 版本提示检查；宿主需禁用时可设置 `PIPPIT_CLI_DISABLE_UPDATE_CHECK=1`。

本技能没有单独的自动安装脚本，也不依赖综合创作 Skill 的脚本。独立 ZIP 应保留整个 commands、workflows、examples、scripts 目录及相对引用；`SKILL.md` 的 name 与 ZIP 文件名保持一致。
