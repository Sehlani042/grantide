# 允界 · Grantide

**让 AI 自主，也让你放心。**

一个在本机运行的 AI 权限网关，通过浏览器配置规则、审批请求、限时授权和撤销访问。单个 Go 程序内置 GUI，无需云账号或前端构建环境。

[English](README.md) · [API 文档](docs/API.md) · [安全边界](SECURITY.md) · [下载](https://github.com/Sehlani042/grantide/releases)

![允界控制台](docs/screenshots/console.png)

| 模式 | 实际行为 |
| --- | --- |
| 自动放行 | 在指定 Agent、服务、方法和路径范围内直接执行。 |
| 逐次审批 | 暂停当前请求，查看目的地、Query 和 JSON Body 后再批准。 |
| 限时授权 | 批准指定范围内的一段时间和最多调用次数，可随时撤销。 |
| 明确禁止 | 命中就拒绝，其他允许规则无法覆盖。 |

没有匹配规则时默认拒绝。Agent 无法批准自己的请求，也无法修改权限。上游密钥由网关在授权后注入。

## 本机体验

从 [Releases](https://github.com/Sehlani042/grantide/releases) 下载对应系统的程序，或用 Go 1.25+ 构建：

```sh
git clone https://github.com/Sehlani042/grantide.git
cd grantide
go build -o bin/grantide ./cmd/grantide
./bin/grantide serve --demo --data-dir .local/demo
```

程序自动选择并记住本机端口。终端会显示控制台地址。数据目录中的 `operator-url` 是带登录凭证的链接；也可以打开控制台后粘贴 `operator.token` 内容。不要把这两个文件给 Agent。

演示环境预置四条规则，所有示例访问内置模拟服务。点击读取监控、生产发布、临时诊断和删除数据，可以实际观察放行、审批、授权和阻止。演示请求使用真实的权限引擎，但不会访问真实账号。

## 接入自己的 Agent

1. 不带 `--demo` 启动，数据目录选择 Agent 无法访问的位置。
2. 在「连接管理」创建 Agent，保存只显示一次的 token；添加 API 服务的 origin 和凭证。
3. 在「权限规则」指定身份、服务、HTTP 方法、路径和权限模式。
4. 给 Agent 配置 `GRANTIDE_URL`、`GRANTIDE_TOKEN`，通过 `grantide call` 或 HTTP API 调用。

```sh
export GRANTIDE_URL='http://127.0.0.1:你的端口'
export GRANTIDE_TOKEN='Agent 专属 token'
./bin/grantide call --service 服务ID --method POST --path /v1/deploy \
  --body '{"environment":"staging","version":"v1.2.0"}'
```

CLI 会等待审批并输出执行结果。权限优先级为：禁止、逐次审批、限时授权、自动放行。相同模式按更具体的身份/路径/方法选择，最后用规则 ID 保持确定性。`/v1` 不会误匹配 `/v10`。

临时授权绑定具体 Agent，调用次数包括批准时的第一个请求。修改配置或重启会使旧授权失效。审批最多等待 5 分钟，与「批准后能用多久」是两件事。可以给自动放行规则设置到期时间；到期后匹配请求会被拒绝。撤销只能阻止后续请求，无法撤回已经发出的操作。

## 当前边界

这是实验性 alpha，目前管理显式提交的 JSON HTTP 请求。Agent 若仍持有上游凭证和直连能力，就可以绕过网关；若与管理员共用完整 OS 权限，也可能读取网关文件。要约束不可信 Agent，需要另外配置账号/容器和网络隔离。本版不会自动安装这些隔离设施。

规则不检查 Body/Query 内部的业务字段。对于同一个 URL 承载多种动作的接口，应先使用逐次审批。SSH、本机命令、透明代理、MCP、流式请求及多用户云部署尚未实现。

配置与最近 500 条审计元数据使用 AES-256-GCM 加密；密钥和状态放在同一目录，目录整体可读时不能提供隔离保障。请求正文和响应仅保存在当前进程的有界内存中。尚未经过独立安全审计。详见 [SECURITY.md](SECURITY.md)。

## 开发验证

```sh
go test -race ./...
go vet ./...
node --check internal/webui/assets/app.js
```

MIT 开源。Go 标准库实现后端，原生浏览器模块实现 GUI，没有第三方运行时依赖。

## 在界面里填写凭证

Codex 可通过 `grantide credential` 发起申请，你在“凭证申请”页面填写 API Key 或 HTTP Basic 账号密码，Agent 只得到状态。填写后仍按原有权限规则执行；普通网站请使用下方的浏览器登录交接；SSH 或 sudo 密码尚未接入。接入方式见 [Codex 使用说明](docs/CODEX.md)。

## 浏览器网站登录

`grantide browser-login` 把登录交接登记到界面，显示原对话、浏览器和标签。你在原网站输入密码、扫码或完成验证码，再在允界确认；原 Agent 核验同一标签后继续。此功能依靠接入方暂停读取，不能锁住独立浏览器工具，也不会自动向其他对话发消息。登录完成不代表批准购买。详见 [接入说明](docs/BROWSER-HANDOFF.md)。

## 自动凭据登录与跨对话复用（v0.4 开发版）

`grantide web-login` 使用保存在允界的账号和指定 Agent 的授权，由受控 Chromium 填写凭据。CloudCone 真实登录已验证成功，其中一次图片验证码由外部 AI 在用户授权后操作完成；允界尚未内置验证码自动识别。真实资产提取及后续传输修复仍需实际账号验证。流程结束会关闭浏览器，其他对话不会继承会话。

登录授权支持分别选择“无限期”和“无限次数”，随时可撤销。同一账号启动间隔至少 60 秒，单次最多 10 分钟；失败或中断后暂停，由操作者解除。旧的次数已耗尽授权不会自动升级。

其他对话可阅读 [grantide-login Skill](skills/grantide-login/SKILL.md) 接入。将仓库的 `skills/grantide-login` 目录链接到本机 Codex skills 目录，即可作为复用入口；已有对话若未刷新技能目录，可直接读取该文件。[操作契约](docs/CONTROLLED-LOGIN.md)和[开发经验及验证边界](docs/LOGIN-LESSONS.md)分别记录调用方法与证据。

## Chrome 扩展（开发版）

已加入 Manifest V3 扩展，通过配对的本机 Native Messaging 组件，在 Chrome 标签页使用保存账号登录 CloudCone。Agent 使用 `web-login --grant REF --extension`；次数、冷却、撤销及失败暂停仍由允界管理。见[安装与边界](docs/CHROME-EXTENSION.md)。现有会话的账号核验与扩展实站登录尚未验证；Chrome 会话保留在 Chrome 中，与 Codex 内置浏览器分别管理。
