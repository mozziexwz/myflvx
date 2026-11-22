package proxykit

import (
	"slices"
	"strings"
)

// Template is a built-in starting point for the subscription generator.
type Template struct {
	ID          string           `json:"id"`
	Name        string           `json:"name"`
	Description string           `json:"description"`
	Groups      []ProxyGroupSpec `json:"groups"`
	Rules       []string         `json:"rules"`
	BaseConfig  string           `json:"baseConfig"`
}

// DefaultBaseConfig holds the general settings written before proxies. It
// targets mihomo (Clash Meta) clients such as Clash Verge Rev, Mihomo Party
// and FlClash, which are required for SS 2022 / mieru / Sudoku nodes anyway.
//
// The general options follow the defaults that subscription converters such
// as miaomiaowu / sublink-worker ship: no sniffer (it rewrites connection
// targets), no tcp-concurrent, no global client fingerprint. With those enabled
// users reported that sites opened by IP:port stopped working while the same
// node worked with a miaomiaowu subscription.
const DefaultBaseConfig = baseGeneralSettings + baseDNSSettings

const baseGeneralSettings = `mixed-port: 7890
allow-lan: false
mode: rule
log-level: info
ipv6: true
unified-delay: true
geodata-mode: true
geodata-loader: standard
geo-auto-update: true
geo-update-interval: 24
geox-url:
  geoip: https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@release/geoip.dat
  geosite: https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@release/geosite.dat
  mmdb: https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@release/country.mmdb
  asn: https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@release/GeoLite2-ASN.mmdb
profile:
  store-selected: true
  store-fake-ip: true
`

// legacyGeneralSettingsV1 is the general block of myflvx 2.2.1. A generator
// config that stored this default verbatim is upgraded to the current default.
const legacyGeneralSettingsV1 = `mixed-port: 7890
allow-lan: false
mode: rule
log-level: info
ipv6: true
unified-delay: true
tcp-concurrent: true
find-process-mode: strict
global-client-fingerprint: chrome
geodata-mode: true
geo-auto-update: true
geo-update-interval: 24
geox-url:
  geoip: https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@release/geoip.dat
  geosite: https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@release/geosite.dat
  mmdb: https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@release/country.mmdb
  asn: https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@release/GeoLite2-ASN.mmdb
profile:
  store-selected: true
  store-fake-ip: true
sniffer:
  enable: true
  sniff:
    HTTP:
      ports: [80, 8080-8880]
      override-destination: true
    TLS:
      ports: [443, 8443]
    QUIC:
      ports: [443, 8443]
`

// effectiveBaseConfig returns the base config a generator config renders with:
// the current default when none is stored, or when the stored text is an old
// built-in default that was copied verbatim.
func effectiveBaseConfig(stored string) string {
	text := strings.TrimSpace(strings.ReplaceAll(stored, "\r\n", "\n"))
	if text == "" || text == strings.TrimSpace(legacyGeneralSettingsV1+baseDNSSettings) {
		return DefaultBaseConfig
	}
	return text
}

const baseDNSSettings = `dns:
  enable: true
  ipv6: true
  listen: 0.0.0.0:1053
  enhanced-mode: fake-ip
  fake-ip-range: 198.18.0.1/16
  fake-ip-filter:
    - "*.lan"
    - "*.local"
    - "+.msftconnecttest.com"
    - "+.msftncsi.com"
    - "localhost.ptlogin2.qq.com"
    - "+.stun.*.*"
    - "+.stun.*.*.*"
    - "time.*.com"
    - "ntp.*.com"
  default-nameserver:
    - 223.5.5.5
    - 119.29.29.29
  nameserver:
    - https://dns.alidns.com/dns-query
    - https://doh.pub/dns-query
  proxy-server-nameserver:
    - https://dns.alidns.com/dns-query
    - https://doh.pub/dns-query
`

const (
	groupSelect   = "🚀 节点选择"
	groupAuto     = "⚡ 自动选择"
	groupFallback = "🔯 故障转移"
	groupFinal    = "🐟 漏网之鱼"
	groupDirect   = "🎯 全球直连"
	groupAI       = "🤖 AI 服务"
)

// legacyStandardRules is the exact rule list saved by the pre-2.2.5 editor.
var legacyStandardRules = []string{
	"GEOSITE,category-ads-all,🛑 广告拦截",
	"GEOSITE,private," + groupDirect,
	"GEOIP,private," + groupDirect + ",no-resolve",
	"GEOSITE,category-ai-!cn," + groupAI,
	"GEOSITE,youtube,📹 YouTube",
	"GEOSITE,netflix,🎥 流媒体",
	"GEOSITE,disney,🎥 流媒体",
	"GEOSITE,spotify,🎥 流媒体",
	"GEOSITE,telegram,📲 Telegram",
	"GEOIP,telegram,📲 Telegram,no-resolve",
	"GEOSITE,github,🐱 GitHub",
	"GEOSITE,google,🔍 Google",
	"GEOSITE,microsoft,Ⓜ️ 微软服务",
	"GEOSITE,apple,🍎 苹果服务",
	"GEOSITE,geolocation-!cn," + groupSelect,
	"GEOSITE,cn," + groupDirect,
	"GEOIP,cn," + groupDirect,
	"MATCH," + groupFinal,
}

// claudeRulesV2 is the rule block saved before the general categories in 2.2.5.
var claudeRulesV2 = []string{
	"DOMAIN-SUFFIX,anthropic.com," + groupAI,
	"DOMAIN-SUFFIX,claude.ai," + groupAI,
	"DOMAIN-SUFFIX,claude.com," + groupAI,
	"DOMAIN-SUFFIX,clau.de," + groupAI,
	"DOMAIN-SUFFIX,claudemcpclient.com," + groupAI,
	"DOMAIN-SUFFIX,claudemcpcontent.com," + groupAI,
	"DOMAIN-SUFFIX,claudeusercontent.com," + groupAI,
	"DOMAIN,servd-anthropic-website.b-cdn.net," + groupAI,
	"DOMAIN,anthropic.com.cdn.cloudflare.net," + groupAI,
	"DOMAIN,anthropic.auth0.com," + groupAI,
	"DOMAIN,anthropic-com.ghost.io," + groupAI,
	"DOMAIN,http-intake.logs.us5.datadoghq.com," + groupAI,
	"DOMAIN,browser-intake-us5-datadoghq.com," + groupAI,
	// Inbound ranges from https://platform.claude.com/docs/en/api/ip-addresses.
	"IP-CIDR,160.79.104.0/23," + groupAI + ",no-resolve",
	"IP-CIDR,153.61.224.0/22," + groupAI + ",no-resolve",
	"IP-CIDR6,2607:6bc0::/48," + groupAI + ",no-resolve",
}

func standardRules() []string {
	return slices.Concat(claudeRulesV2, []string{"IP-ASN,399358," + groupAI + ",no-resolve"}, legacyStandardRules)
}

// UpgradeLegacyTemplateRules updates the stored rules of an older subscription
// that still uses the default standard rule list and its AI group.
func (cfg *GeneratorConfig) UpgradeLegacyTemplateRules(version int) {
	if cfg.Template != "" && cfg.Template != "standard" {
		return
	}
	var previousRules []string
	switch version {
	case 0:
		previousRules = legacyStandardRules
	case 2:
		previousRules = slices.Concat(claudeRulesV2, legacyStandardRules)
	default:
		return
	}
	if !slices.Equal(cfg.Rules, previousRules) {
		return
	}
	if len(cfg.Groups) > 0 && !slices.ContainsFunc(cfg.Groups, func(group ProxyGroupSpec) bool {
		return strings.TrimSpace(group.Name) == groupAI
	}) {
		return
	}
	cfg.Rules = standardRules()
}

func autoGroup(name, typ string) ProxyGroupSpec {
	return ProxyGroupSpec{Name: name, Type: typ, IncludeAllNodes: true, URL: defaultTestURL, Interval: 300}
}

func selectGroup(name string, members []string, includeNodes bool) ProxyGroupSpec {
	return ProxyGroupSpec{Name: name, Type: "select", Proxies: members, IncludeAllNodes: includeNodes}
}

// Templates returns the built-in generator templates.
func Templates() []Template {
	return []Template{
		{
			ID:          "minimal",
			Name:        "极简",
			Description: "节点选择 + 自动测速，国内直连，其余走代理",
			BaseConfig:  DefaultBaseConfig,
			Groups: []ProxyGroupSpec{
				selectGroup(groupSelect, []string{groupAuto, "DIRECT"}, true),
				autoGroup(groupAuto, "url-test"),
				selectGroup(groupFinal, []string{groupSelect, "DIRECT"}, false),
			},
			Rules: []string{
				"GEOSITE,private,DIRECT",
				"GEOIP,private,DIRECT,no-resolve",
				"GEOSITE,cn,DIRECT",
				"GEOIP,cn,DIRECT",
				"MATCH," + groupFinal,
			},
		},
		{
			ID:          "standard",
			Name:        "常用分流",
			Description: "AI（含 Claude Code）、YouTube、流媒体、Telegram、Google、GitHub、微软、苹果分流，广告拦截",
			BaseConfig:  DefaultBaseConfig,
			Groups: []ProxyGroupSpec{
				selectGroup(groupSelect, []string{groupAuto, groupFallback, "DIRECT"}, true),
				autoGroup(groupAuto, "url-test"),
				autoGroup(groupFallback, "fallback"),
				selectGroup(groupAI, []string{groupSelect, groupAuto}, true),
				selectGroup("📹 YouTube", []string{groupSelect, groupAuto}, true),
				selectGroup("🎥 流媒体", []string{groupSelect, groupAuto}, true),
				selectGroup("📲 Telegram", []string{groupSelect, groupAuto}, true),
				selectGroup("🔍 Google", []string{groupSelect, groupAuto, "DIRECT"}, true),
				selectGroup("🐱 GitHub", []string{groupSelect, "DIRECT"}, true),
				selectGroup("Ⓜ️ 微软服务", []string{"DIRECT", groupSelect}, true),
				selectGroup("🍎 苹果服务", []string{"DIRECT", groupSelect}, true),
				selectGroup(groupDirect, []string{"DIRECT", groupSelect}, false),
				selectGroup("🛑 广告拦截", []string{"REJECT", "DIRECT"}, false),
				selectGroup(groupFinal, []string{groupSelect, groupAuto, "DIRECT"}, false),
			},
			Rules: standardRules(),
		},
		{
			ID:          "global",
			Name:        "全局代理",
			Description: "除局域网外全部走代理",
			BaseConfig:  DefaultBaseConfig,
			Groups: []ProxyGroupSpec{
				selectGroup(groupSelect, []string{groupAuto, "DIRECT"}, true),
				autoGroup(groupAuto, "url-test"),
			},
			Rules: []string{
				"GEOSITE,private,DIRECT",
				"GEOIP,private,DIRECT,no-resolve",
				"MATCH," + groupSelect,
			},
		},
	}
}

// TemplateByID returns the template with the given id (standard by default).
func TemplateByID(id string) Template {
	all := Templates()
	for _, t := range all {
		if t.ID == id {
			return t
		}
	}
	return all[1]
}
