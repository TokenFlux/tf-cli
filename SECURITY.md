# 安全

## 报告漏洞

请发邮件到 security@tokenflux.dev，不要开公开 issue。

## tkr 如何处理你的凭据

- API Key 存在 `credentials.json`，权限 0600；文件权限过宽时启动会自动收紧并告知
- Key **绝不接受**从命令行参数传入 —— 那会进 shell 历史，也能被同机其它进程从 `ps` 看到
- 展示时一律掩码（`sk-d61…5b1c`），掩码是唯一出口
- Key 通过环境变量传给子进程；tkr 自身不代理、不转发、不记录任何请求内容
- 不上报遥测

## 边界

tkr 会把你的 Key 交给你指定的 harness 子进程。那个进程能做什么，取决于它自己 ——
tkr 不做沙箱，也不检查 harness 的行为。

`--host` 可以指向任意地址，请只指向你信任的 TokenRouter 实例。
