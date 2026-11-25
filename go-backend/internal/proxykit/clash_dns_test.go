package proxykit

import (
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const dnsTestOverseasPolicy = "geosite:category-ai-!cn,youtube,netflix,disney,spotify,telegram,github,google"

type renderedGroup struct {
	Name    string   `yaml:"name"`
	Type    string   `yaml:"type"`
	Proxies []string `yaml:"proxies"`
	Lazy    bool     `yaml:"lazy"`
	Hidden  bool     `yaml:"hidden"`
}

type renderedSubscription struct {
	IPv6    *bool           `yaml:"ipv6"`
	Proxies []Proxy         `yaml:"proxies"`
	Groups  []renderedGroup `yaml:"proxy-groups"`
	Rules   []string        `yaml:"rules"`
	DNS     struct {
		IPv6               *bool          `yaml:"ipv6"`
		Listen             string         `yaml:"listen"`
		RespectRules       *bool          `yaml:"respect-rules"`
		DefaultNameservers []string       `yaml:"default-nameserver"`
		Nameservers        []string       `yaml:"nameserver"`
		DirectNameservers  []string       `yaml:"direct-nameserver"`
		Fallback           []string       `yaml:"fallback"`
		Policy             map[string]any `yaml:"nameserver-policy"`
	} `yaml:"dns"`
	Sniffer struct {
		Enable              bool     `yaml:"enable"`
		OverrideDestination *bool    `yaml:"override-destination"`
		SkipDestinations    []string `yaml:"skip-dst-address"`
	} `yaml:"sniffer"`
}

func renderSubscriptionForTest(t *testing.T, cfg GeneratorConfig, nodes ...Proxy) (renderedSubscription, RenderResult) {
	t.Helper()
	result, err := Render(cfg, nodes)
	if err != nil {
		t.Fatal(err)
	}
	var doc renderedSubscription
	if err := yaml.Unmarshal([]byte(result.Content), &doc); err != nil {
		t.Fatal(err)
	}
	return doc, result
}

func (doc renderedSubscription) group(t *testing.T, name string) renderedGroup {
	t.Helper()
	for _, group := range doc.Groups {
		if group.Name == name {
			return group
		}
	}
	t.Fatalf("missing group %q", name)
	return renderedGroup{}
}

func TestRenderBuiltinDNSAndSniffer(t *testing.T) {
	node := Proxy{"name": "remote", "type": "ss"}
	for _, template := range Templates() {
		t.Run(template.ID, func(t *testing.T) {
			doc, result := renderSubscriptionForTest(t, GeneratorConfig{Template: template.ID}, node)
			if doc.IPv6 == nil || !*doc.IPv6 || doc.DNS.IPv6 == nil || !*doc.DNS.IPv6 {
				t.Fatal("default template must preserve dual-stack support")
			}
			if !doc.Sniffer.Enable || doc.Sniffer.OverrideDestination == nil || *doc.Sniffer.OverrideDestination {
				t.Fatal("sniffing should preserve the destination")
			}
			for _, subnet := range []string{"192.168.0.0/16", "::1/128", "fc00::/7", "fe80::/10"} {
				if !slices.Contains(doc.Sniffer.SkipDestinations, subnet) {
					t.Errorf("sniffer must skip %s", subnet)
				}
			}
			if doc.DNS.Listen != "127.0.0.1:1053" || doc.DNS.RespectRules == nil || *doc.DNS.RespectRules ||
				len(doc.DNS.DirectNameservers) != 0 || len(doc.DNS.Fallback) != 0 ||
				!slices.Equal(doc.DNS.DefaultNameservers, []string{"https://223.5.5.5/dns-query", "119.29.29.29"}) {
				t.Fatalf("unexpected DNS settings: %+v", doc.DNS)
			}
			if doc.DNS.Policy["geosite:private"] != "rcode://name_error" ||
				!strings.Contains(result.Content, "https://doh.pub/dns-query#DIRECT") ||
				strings.Contains(result.Content, "https://1.12.12.12/") {
				t.Fatal("expected private DNS isolation and supported domestic DoH")
			}
			if doc.Groups[0].Name != groupSelect || doc.Groups[len(doc.Groups)-1].Name != groupOverseasDNS {
				t.Fatalf("incorrect first or last group: %v", doc.Groups)
			}
			group := doc.group(t, groupOverseasDNS)
			if group.Type != "fallback" || !group.Lazy || !group.Hidden || !slices.Equal(group.Proxies, []string{"remote"}) {
				t.Fatalf("bad overseas group: %+v", group)
			}
			if len(result.Warnings) != 0 {
				t.Fatalf("built-in template has warnings: %v", result.Warnings)
			}
		})
	}
}

func TestRenderIPv4OnlyLeavesBaseConfigIntact(t *testing.T) {
	for _, base := range []string{"", "ipv6: true\ndns: {enable: true, ipv6: true}\n"} {
		cfg := GeneratorConfig{BaseConfig: base, IPv4Only: true}
		doc, _ := renderSubscriptionForTest(t, cfg)
		if doc.IPv6 == nil || *doc.IPv6 || doc.DNS.IPv6 == nil || *doc.DNS.IPv6 {
			t.Fatal("IPv4-only mode must explicitly disable both IPv6 settings")
		}
		cfg.IPv4Only = false
		doc, _ = renderSubscriptionForTest(t, cfg)
		if doc.IPv6 == nil || !*doc.IPv6 || doc.DNS.IPv6 == nil || !*doc.DNS.IPv6 {
			t.Fatal("disabling IPv4-only mode must restore the base settings")
		}
	}
}

func TestRenderKeepsDNSNamesSeparateFromUserGroupsAndNodes(t *testing.T) {
	const firstAlternate = "海外 DNS (内置)"
	const secondAlternate = "海外 DNS (内置) 2"
	nodes := []Proxy{{"name": groupOverseasDNS, "type": "ss"}, {"name": firstAlternate, "type": "trojan"}}
	doc, _ := renderSubscriptionForTest(t, GeneratorConfig{}, nodes...)
	if len(doc.Proxies) != 2 || Name(doc.Proxies[0]) != groupOverseasDNS || Name(doc.Proxies[1]) != firstAlternate {
		t.Fatalf("built-in group must not hide existing nodes: %+v", doc.Proxies)
	}
	if group := doc.group(t, secondAlternate); !slices.Equal(group.Proxies, []string{groupOverseasDNS, firstAlternate}) {
		t.Fatalf("incorrect renamed group: %+v", group)
	}

	groups := []ProxyGroupSpec{
		selectGroup(groupSelect, nil, true),
		selectGroup(groupOverseasDNS, []string{"empty child"}, false),
		{Name: "empty child", Type: "select", IncludeAllNodes: true, Filter: "^missing$"},
	}
	// The editor can save the complete current default via the fill button.
	cfg := GeneratorConfig{BaseConfig: strings.TrimSpace(DefaultBaseConfig), Groups: groups}
	doc, result := renderSubscriptionForTest(t, cfg, Proxy{"name": "remote", "type": "ss"})
	if !slices.Equal(doc.group(t, groupOverseasDNS).Proxies, []string{"empty child"}) ||
		!slices.Equal(doc.group(t, "empty child").Proxies, []string{"DIRECT"}) ||
		!slices.Equal(doc.group(t, firstAlternate).Proxies, []string{"remote"}) {
		t.Fatalf("custom groups and managed groups should remain distinct: %+v", doc.Groups)
	}
	if len(doc.DNS.Nameservers) != 2 || !strings.HasSuffix(doc.DNS.Nameservers[0], "#"+firstAlternate) ||
		!strings.HasSuffix(doc.DNS.Nameservers[1], "#"+firstAlternate) ||
		strings.Count(result.Content, "/dns-query#"+firstAlternate+`"`) != 4 {
		t.Fatal("both default and policy overseas resolvers must reference the safe group")
	}
	if doc.DNS.Policy[dnsTestOverseasPolicy] == nil || len(result.Warnings) < 2 || len(cfg.Groups) != 3 {
		t.Fatalf("expected policy, warnings, and unchanged input groups: %v", result.Warnings)
	}
}

func TestRenderDefaultDNSAndAIRejectWithoutRealProxyNodes(t *testing.T) {
	nodes := []Proxy{{"name": "local dial", "type": "direct"}, {"name": "local resolver", "type": "dns"}}
	for _, input := range [][]Proxy{nil, nodes} {
		doc, _ := renderSubscriptionForTest(t, GeneratorConfig{Template: "standard"}, input...)
		for _, name := range []string{groupOverseasDNS, groupAI} {
			if !slices.Equal(doc.group(t, name).Proxies, []string{"REJECT"}) {
				t.Fatalf("%q must reject when no real proxy is available", name)
			}
		}
	}
	doc, _ := renderSubscriptionForTest(t, GeneratorConfig{Template: "standard"}, append(nodes, Proxy{"name": "remote", "type": "ss"})...)
	for _, name := range []string{groupOverseasDNS, groupAI} {
		if !slices.Equal(doc.group(t, name).Proxies, []string{"remote"}) {
			t.Fatalf("%q must only include the real proxy", name)
		}
	}
}

func TestRenderWarnsAboutCustomDNSAndAIDirectTargets(t *testing.T) {
	cfg := GeneratorConfig{
		BaseConfig: `dns:
  nameserver:
    - "https://1.1.1.1/dns-query#missing"
    - "https://1.1.1.1/dns-query#DIRECT"
    - "https://1.1.1.1/dns-query#RULES"
    - "https://1.1.1.1/dns-query#海外%20DNS&ecs=1.2.3.4/24"
    - "https://1.1.1.1/dns-query#empty%20child"
`,
		Groups: []ProxyGroupSpec{
			overseasDNSGroup(),
			{Name: "empty child", Type: "select", IncludeAllNodes: true, Filter: "^missing$"},
			selectGroup(groupAI, []string{"local dial"}, false),
		},
	}
	doc, result := renderSubscriptionForTest(t, cfg, Proxy{"name": "remote", "type": "ss"}, Proxy{"name": "local dial", "type": "direct"})
	warnings := strings.Join(result.Warnings, "\n")
	for _, expected := range []string{"DNS 上游引用「missing」", "DNS 上游引用「empty child」可走直连", "代理组「🤖 AI 服务」可走直连"} {
		if !strings.Contains(warnings, expected) {
			t.Errorf("missing %q in %v", expected, result.Warnings)
		}
	}
	for _, target := range []string{"DIRECT", "RULES", groupOverseasDNS} {
		if strings.Contains(warnings, "DNS 上游引用「"+target+"」") {
			t.Errorf("unexpected warning about %q: %v", target, result.Warnings)
		}
	}
	if !slices.Equal(doc.group(t, groupAI).Proxies, []string{"local dial"}) {
		t.Fatal("explicit custom AI members must be preserved")
	}
}

func TestUpgradeLegacyBaseConfig(t *testing.T) {
	for _, base := range []string{"", legacyGeneralSettingsV1 + legacyDNSSettingsV1, legacyGeneralSettingsV2 + legacyDNSSettingsV1, strings.TrimSpace(DefaultBaseConfig)} {
		if !usesDefaultBaseConfig(base) || effectiveBaseConfig(base) != DefaultBaseConfig {
			t.Fatal("saved built-in base did not resolve to the current default")
		}
		cfg := GeneratorConfig{BaseConfig: base, Groups: []ProxyGroupSpec{selectGroup(groupSelect, nil, true)}}
		cfg.UpgradeLegacyTemplateConfig(3)
		if cfg.BaseConfig != "" || groupIndex(cfg.Groups, groupOverseasDNS) != 1 {
			t.Fatal("legacy built-in base should use the current default and gain the overseas group")
		}
	}
	const custom = "ipv6: false\ndns: {enable: false}"
	cfg := GeneratorConfig{BaseConfig: custom}
	cfg.UpgradeLegacyTemplateConfig(3)
	if cfg.BaseConfig != custom || effectiveBaseConfig(cfg.BaseConfig) != custom || usesDefaultBaseConfig(custom) {
		t.Fatal("custom base must be preserved")
	}
}
