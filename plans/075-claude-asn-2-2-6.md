# 075 Claude ASN 兜底与 2.2.6 发布

在「常用分流」的 Claude 域名和官方 API 入站网段之后匹配 `IP-ASN,399358,🤖 AI 服务,no-resolve`。Mihomo 使用订阅已配置的 ASN 数据库匹配已知的目标 IP。[Mihomo 路由规则](https://wiki.metacubex.one/en/config/rules/) · [Anthropic 公布的入站网段](https://platform.claude.com/docs/en/api/ip-addresses#inbound-ip-addresses)

读取生成订阅时，通过保存的规则版本识别 2.2.5 和更早版本的完整默认列表。仍使用对应默认列表的订阅在刷新时采用新版。显式自定义的规则按保存顺序生效。使用 `2.2.6` 标签触发镜像构建和 Release 发布。

## 任务清单

- [x] 核对现有 ASN 数据库配置、匹配顺序及发布标签。
- [x] 在「常用分流」加入 ASN 规则并升级旧版默认订阅。
- [x] 验证规则顺序、旧默认订阅升级和自定义规则兼容性。

`proxykit` 测试、后端契约测试和 `go vet` 通过。2.2.5 的 16 条 Claude 域名和 CIDR 规则保持原顺序。
