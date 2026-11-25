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
// Sniffed domains are used for matching while preserving the target address.
const DefaultBaseConfig = baseGeneralSettings + baseDNSSettings

const baseGeneralSettings = legacyGeneralSettingsV2 + `sniffer:
  enable: true
  force-dns-mapping: true
  parse-pure-ip: true
  override-destination: false
  sniff:
    HTTP:
      ports: [80, 8080-8880]
    TLS:
      ports: [443, 8443]
    QUIC:
      ports: [443, 8443]
  skip-domain:
    - "Mijia Cloud"
    - "+.push.apple.com"
    - "+.lan"
    - "+.local"
    - "+.home.arpa"
    - "+.internal"
  skip-dst-address:
    - 10.0.0.0/8
    - 127.0.0.0/8
    - 169.254.0.0/16
    - 172.16.0.0/12
    - 192.168.0.0/16
    - "::1/128"
    - "fc00::/7"
    - "fe80::/10"
`

const legacyGeneralSettingsV2 = `mixed-port: 7890
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

func usesDefaultBaseConfig(stored string) bool {
	switch strings.TrimSpace(strings.ReplaceAll(stored, "\r\n", "\n")) {
	case "", strings.TrimSpace(DefaultBaseConfig),
		strings.TrimSpace(legacyGeneralSettingsV1 + legacyDNSSettingsV1),
		strings.TrimSpace(legacyGeneralSettingsV2 + legacyDNSSettingsV1):
		return true
	}
	return false
}

func effectiveBaseConfig(stored string) string {
	if usesDefaultBaseConfig(stored) {
		return DefaultBaseConfig
	}
	return strings.TrimSpace(strings.ReplaceAll(stored, "\r\n", "\n"))
}

const legacyDNSSettingsV1 = `dns:
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

const baseDNSSettings = `dns:
  enable: true
  ipv6: true
  listen: 127.0.0.1:1053
  enhanced-mode: fake-ip
  fake-ip-range: 198.18.0.1/16
  respect-rules: false
  fake-ip-filter:
    - "*"
    - "+.localhost"
    - "+.lan"
    - "+.local"
    - "+.home.arpa"
    - "+.internal"
    - "geosite:private"
    - "+.msftconnecttest.com"
    - "+.msftncsi.com"
    - "localhost.ptlogin2.qq.com"
    - "+.stun.*.*"
    - "+.stun.*.*.*"
    - "time.*.com"
    - "ntp.*.com"
    - "+.pool.ntp.org"
  default-nameserver:
    - https://223.5.5.5/dns-query
    - 119.29.29.29
  proxy-server-nameserver:
    - "https://223.5.5.5/dns-query#DIRECT"
    - "https://doh.pub/dns-query#DIRECT"
  nameserver:
    - "https://1.1.1.1/dns-query#` + groupOverseasDNS + `"
    - "https://8.8.8.8/dns-query#` + groupOverseasDNS + `"
  # 私有域名默认返回 NXDOMAIN。这也会影响 Plex、Tailscale、NAS 和公司内网。
  # 需要内网解析时，在拦截规则之前将实际使用的后缀指向可信内网 DNS。
  nameserver-policy:
    "localhost.ptlogin2.qq.com,+.msftconnecttest.com,+.msftncsi.com,time.*.com,ntp.*.com,+.pool.ntp.org":
      - "https://223.5.5.5/dns-query#DIRECT"
      - "https://doh.pub/dns-query#DIRECT"
    "*,+.localhost,+.lan,+.local,+.home.arpa,+.internal": "rcode://name_error"
    "geosite:private": "rcode://name_error"
    "+.cn":
      - "https://223.5.5.5/dns-query#DIRECT"
      - "https://doh.pub/dns-query#DIRECT"
    "geosite:category-ai-!cn,youtube,netflix,disney,spotify,telegram,github,google":
      - "https://1.1.1.1/dns-query#` + groupOverseasDNS + `"
      - "https://8.8.8.8/dns-query#` + groupOverseasDNS + `"
    "geosite:cn,apple-cn,apple@cn,microsoft@cn":
      - "https://223.5.5.5/dns-query#DIRECT"
      - "https://doh.pub/dns-query#DIRECT"
`

const (
	groupSelect      = "🚀 节点选择"
	groupAuto        = "⚡ 自动选择"
	groupFallback    = "🔯 故障转移"
	groupFinal       = "🐟 漏网之鱼"
	groupDirect      = "🎯 全球直连"
	groupAI          = "🤖 AI 服务"
	groupMedia       = "🎥 流媒体"
	groupRFCEmby     = "RFC-EMBY"
	groupOverseasDNS = "海外 DNS"
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

const claudeASNRule = "IP-ASN,399358," + groupAI + ",no-resolve"

var legacyStandardRulesV3 = slices.Concat(claudeRulesV2, []string{claudeASNRule}, legacyStandardRules)

var privateDirectRules = []string{
	"DOMAIN-SUFFIX,lan,DIRECT",
	"DOMAIN-SUFFIX,local,DIRECT",
	"DOMAIN-SUFFIX,home.arpa,DIRECT",
	"DOMAIN-SUFFIX,internal,DIRECT",
	"GEOSITE,private,DIRECT",
	"GEOIP,private,DIRECT,no-resolve",
}

func claudeUDPRejectRule(rules []string) string {
	matchers := make([]string, 0, len(rules))
	for _, rule := range rules {
		// Logical subrules retain no-resolve and omit the standalone target.
		parts := slices.Delete(splitRule(rule), 2, 3)
		matchers = append(matchers, "("+strings.Join(parts, ",")+")")
	}
	return "AND,((NETWORK,UDP),(OR,(" + strings.Join(matchers, ",") + "))),REJECT"
}

func standardRules() []string {
	claudeRules := slices.Concat(claudeRulesV2, []string{claudeASNRule})
	return slices.Concat(claudeRules, []string{
		claudeUDPRejectRule(claudeRules),
		"GEOSITE,category-ads-all,🛑 广告拦截",
	}, privateDirectRules, []string{
		"GEOSITE,category-ai-!cn," + groupAI,
		"AND,((NETWORK,UDP),(GEOSITE,category-ai-!cn)),REJECT",
		"GEOSITE,youtube,📹 YouTube",
		"GEOSITE,netflix," + groupMedia,
		"GEOSITE,disney," + groupMedia,
		"GEOSITE,spotify," + groupMedia,
		"DOMAIN-SUFFIX,rfc.uhdnow.com," + groupRFCEmby,
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
	})
}

// UpgradeLegacyTemplateRules retains the legacy entry point for callers.
func (cfg *GeneratorConfig) UpgradeLegacyTemplateRules(version int) {
	cfg.UpgradeLegacyTemplateConfig(version)
}

// UpgradeLegacyTemplateConfig migrates stored defaults without replacing customized settings.
func (cfg *GeneratorConfig) UpgradeLegacyTemplateConfig(version int) {
	if version >= 4 {
		return
	}
	if usesDefaultBaseConfig(cfg.BaseConfig) {
		cfg.BaseConfig = ""
	}
	if cfg.Template == "" || cfg.Template == "standard" {
		cfg.upgradeLegacyStandard(version)
	}
	if len(cfg.Groups) > 0 && cfg.BaseConfig == "" && groupIndex(cfg.Groups, groupOverseasDNS) < 0 {
		cfg.Groups = append(cfg.Groups, overseasDNSGroup())
	}
}

func (cfg *GeneratorConfig) upgradeLegacyStandard(version int) {
	ai := groupIndex(cfg.Groups, groupAI)
	if ai >= 0 && matchesAIGroup(cfg.Groups[ai], []string{groupSelect, groupAuto}) {
		cfg.Groups[ai].Proxies = nil
	}

	var previousRules []string
	switch version {
	case 0:
		previousRules = legacyStandardRules
	case 2:
		previousRules = slices.Concat(claudeRulesV2, legacyStandardRules)
	case 3:
		previousRules = legacyStandardRulesV3
	default:
		return
	}
	if !slices.Equal(cfg.Rules, previousRules) || (len(cfg.Groups) > 0 && ai < 0) {
		return
	}
	cfg.Rules = standardRules()
	if len(cfg.Groups) == 0 || groupIndex(cfg.Groups, groupRFCEmby) >= 0 {
		return
	}
	members := make([]string, 0, 3)
	for _, name := range []string{groupSelect, groupAuto} {
		if groupIndex(cfg.Groups, name) >= 0 {
			members = append(members, name)
		}
	}
	members = append(members, "DIRECT")
	position := len(cfg.Groups)
	if media := groupIndex(cfg.Groups, groupMedia); media >= 0 {
		position = media + 1
	}
	cfg.Groups = slices.Insert(cfg.Groups, position, selectGroup(groupRFCEmby, members, true))
}

func groupIndex(groups []ProxyGroupSpec, name string) int {
	return slices.IndexFunc(groups, func(group ProxyGroupSpec) bool {
		return strings.TrimSpace(group.Name) == name
	})
}

func matchesAIGroup(group ProxyGroupSpec, members []string) bool {
	return strings.TrimSpace(group.Name) == groupAI && group.Type == "select" && group.IncludeAllNodes &&
		strings.TrimSpace(group.Filter) == "" && slices.Equal(group.Proxies, members)
}

func autoGroup(name, typ string) ProxyGroupSpec {
	return ProxyGroupSpec{Name: name, Type: typ, IncludeAllNodes: true, URL: defaultTestURL, Interval: 300}
}

func selectGroup(name string, members []string, includeNodes bool) ProxyGroupSpec {
	return ProxyGroupSpec{Name: name, Type: "select", Proxies: members, IncludeAllNodes: includeNodes}
}

func overseasDNSGroup() ProxyGroupSpec {
	group := autoGroup(groupOverseasDNS, "fallback")
	group.Lazy = true
	group.Hidden = true
	return group
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
				overseasDNSGroup(),
			},
			Rules: slices.Concat(privateDirectRules, []string{
				"GEOSITE,cn,DIRECT",
				"GEOIP,cn,DIRECT",
				"MATCH," + groupFinal,
			}),
		},
		{
			ID:          "standard",
			Name:        "常用分流",
			Description: "AI（含 Claude Code）、YouTube、流媒体、RFC-EMBY、Telegram、Google、GitHub、微软、苹果分流，广告拦截",
			BaseConfig:  DefaultBaseConfig,
			Groups: []ProxyGroupSpec{
				selectGroup(groupSelect, []string{groupAuto, groupFallback, "DIRECT"}, true),
				autoGroup(groupAuto, "url-test"),
				autoGroup(groupFallback, "fallback"),
				selectGroup(groupAI, nil, true),
				selectGroup("📹 YouTube", []string{groupSelect, groupAuto}, true),
				selectGroup(groupMedia, []string{groupSelect, groupAuto}, true),
				selectGroup(groupRFCEmby, []string{groupSelect, groupAuto, "DIRECT"}, true),
				selectGroup("📲 Telegram", []string{groupSelect, groupAuto}, true),
				selectGroup("🔍 Google", []string{groupSelect, groupAuto, "DIRECT"}, true),
				selectGroup("🐱 GitHub", []string{groupSelect, "DIRECT"}, true),
				selectGroup("Ⓜ️ 微软服务", []string{"DIRECT", groupSelect}, true),
				selectGroup("🍎 苹果服务", []string{"DIRECT", groupSelect}, true),
				selectGroup(groupDirect, []string{"DIRECT", groupSelect}, false),
				selectGroup("🛑 广告拦截", []string{"REJECT", "DIRECT"}, false),
				selectGroup(groupFinal, []string{groupSelect, groupAuto, "DIRECT"}, false),
				overseasDNSGroup(),
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
				overseasDNSGroup(),
			},
			Rules: slices.Concat(privateDirectRules, []string{
				"MATCH," + groupSelect,
			}),
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
