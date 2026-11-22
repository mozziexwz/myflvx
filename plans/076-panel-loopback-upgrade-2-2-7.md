# 076 面板本机端口绑定与 2.2.7 发布

更新预检目前把 `BACKEND_PORT` 和 `FRONTEND_PORT` 当成纯数字校验，导致有效的 `127.0.0.1:6365` 和 `127.0.0.1:6366` 被拒绝。预检应检查发布端口号的范围，并交由 Compose 校验完整的地址和端口映射。升级时保留 `.env` 中的绑定地址，按原有流程备份、替换 Compose 文件并重建服务。[Docker 端口发布](https://docs.docker.com/engine/network/port-publishing/#publishing-ports) · [Compose 校验命令](https://docs.docker.com/reference/cli/docker/compose/config/)

`2.2.7` 标签触发现有镜像构建和 Release 发布流程。仓库主分支与 Release 安装脚本均使用修复后的校验逻辑。

## 任务清单

- [x] 复现本机绑定在升级预检被拒绝的问题，核对 Compose 模板和发布标签。
- [x] 更新预检，支持纯端口号、IPv4 和带方括号的 IPv6 绑定。
- [x] 验证绑定地址、端口范围和实际 Compose 渲染结果。

Bash 语法检查和安装脚本回归测试通过。用 Compose 官方解析库校验 v4 和 v6 模板，纯端口号、`127.0.0.1` 和 `[::1]` 均按预期生成前后端端口映射。
