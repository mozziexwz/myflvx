# 074 Claude 分流与 2.2.5 发布

「常用分流」目前通过 `GEOSITE,category-ai-!cn` 识别 AI 服务。参考页面和 Claude 官方文档列出了登录、下载、MCP、附件和遥测需要的主机。Mihomo 按规则顺序匹配，因此 Claude 的明确规则应优先于通用分类。[Net.Coffee 分流参考](https://ip.net.coffee/claude/site.html) · [Claude Code 网络要求](https://code.claude.com/docs/en/network-config#network-access-requirements) · [Mihomo 路由规则](https://wiki.metacubex.one/en/config/rules/)

域名规则匹配 Claude 服务的后缀及具体的第三方主机。IP 兜底使用 Anthropic 公布的 API 入站网段，附带 `no-resolve`。通过原有的 AI 服务组选择出口。[Claude API 入站网段](https://platform.claude.com/docs/en/api/ip-addresses#inbound-ip-addresses)

读取旧的生成订阅时，只有规则与旧版「常用分流」完全一致且目标代理组有效，才采用新版默认规则。显式保存的规则由订阅自己的配置决定。`2.2.5` 标签触发现有 GitHub Actions 流程，构建镜像并生成 Release。

## 任务清单

- [x] 核对参考页面、官方规则依据、现有模板和版本标签。
- [x] 补充 Claude 域名及 IPv4、IPv6 规则，更新旧版默认订阅的读取逻辑。
- [x] 验证新版模板输出、旧默认订阅和自定义规则的兼容性。

订阅契约测试、`proxykit` 测试和 `go vet` 通过。Windows 后端全量测试与 `2.2.4` 的失败用例一致，共 31 项，涉及 SQLite 临时文件清理、Unix 文件权限及 shell 命令。
