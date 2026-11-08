package proxykit

import (
	"bytes"
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"gopkg.in/yaml.v3"
)

// ProxyGroupSpec describes one proxy group in the subscription generator.
type ProxyGroupSpec struct {
	Name            string   `json:"name"`
	Type            string   `json:"type"` // select | url-test | fallback | load-balance
	Proxies         []string `json:"proxies"`
	IncludeAllNodes bool     `json:"includeAllNodes"`
	Filter          string   `json:"filter,omitempty"`
	URL             string   `json:"url,omitempty"`
	Interval        int      `json:"interval,omitempty"`
	Tolerance       int      `json:"tolerance,omitempty"`
	Strategy        string   `json:"strategy,omitempty"`
	Lazy            bool     `json:"lazy,omitempty"`
	Hidden          bool     `json:"hidden,omitempty"`
	Icon            string   `json:"icon,omitempty"`
}

// GeneratorConfig is stored with a generated subscription and replayed every
// time the subscription is downloaded, so it always reflects current nodes.
type GeneratorConfig struct {
	NodeIDs       []int64          `json:"nodeIds"`
	AllNodes      bool             `json:"allNodes"`
	Tags          []string         `json:"tags,omitempty"`
	Template      string           `json:"template"`
	Groups        []ProxyGroupSpec `json:"groups"`
	Rules         []string         `json:"rules"`
	BaseConfig    string           `json:"baseConfig,omitempty"`
	RuleProviders string           `json:"ruleProviders,omitempty"`
}

// Built-in policy names that rules and groups may reference.
var builtinPolicies = map[string]bool{
	"DIRECT": true, "REJECT": true, "REJECT-DROP": true, "PASS": true, "COMPATIBLE": true, "GLOBAL": true,
}

var validGroupTypes = map[string]bool{
	"select": true, "url-test": true, "fallback": true, "load-balance": true, "relay": true,
}

const defaultTestURL = "https://www.gstatic.com/generate_204"

// UniqueNames returns copies of proxies whose names are unique, appending
// " 2", " 3" ... to duplicates, as Clash refuses duplicate proxy names.
func UniqueNames(proxies []Proxy) []Proxy {
	used := map[string]bool{}
	out := make([]Proxy, 0, len(proxies))
	for _, p := range proxies {
		c := CloneProxy(p)
		if c == nil {
			continue
		}
		base := Name(c)
		if base == "" {
			base = strings.ToUpper(Type(c))
		}
		name := base
		for i := 2; used[name] || builtinPolicies[name]; i++ {
			name = fmt.Sprintf("%s %d", base, i)
		}
		used[name] = true
		c["name"] = name
		out = append(out, c)
	}
	return out
}

// RenderResult carries the generated YAML and non-fatal warnings.
type RenderResult struct {
	Content  string   `json:"content"`
	Warnings []string `json:"warnings"`
}

// Render builds a complete Clash/mihomo configuration from the generator
// configuration and the selected nodes.
func Render(cfg GeneratorConfig, nodes []Proxy) (RenderResult, error) {
	result := RenderResult{}
	proxies := UniqueNames(nodes)
	nodeNames := make([]string, 0, len(proxies))
	for _, p := range proxies {
		nodeNames = append(nodeNames, Name(p))
	}

	groups := cfg.Groups
	rules := cfg.Rules
	if len(groups) == 0 {
		tpl := TemplateByID(cfg.Template)
		groups = tpl.Groups
		if len(rules) == 0 {
			rules = tpl.Rules
		}
	}
	baseConfig := effectiveBaseConfig(cfg.BaseConfig)

	groupNames := map[string]bool{}
	for _, g := range groups {
		name := strings.TrimSpace(g.Name)
		if name == "" {
			return result, fmt.Errorf("代理组名称不能为空")
		}
		if groupNames[name] {
			return result, fmt.Errorf("代理组名称重复: %s", name)
		}
		groupNames[name] = true
	}
	nodeSet := map[string]bool{}
	for _, n := range nodeNames {
		if groupNames[n] {
			result.Warnings = append(result.Warnings, fmt.Sprintf("节点名「%s」与代理组同名，已跳过该节点", n))
			continue
		}
		nodeSet[n] = true
	}

	groupNodes := make([]*yaml.Node, 0, len(groups))
	for _, g := range groups {
		node, warnings, err := buildGroupNode(g, nodeNames, nodeSet, groupNames)
		if err != nil {
			return result, err
		}
		result.Warnings = append(result.Warnings, warnings...)
		groupNodes = append(groupNodes, node)
	}

	fallback := "DIRECT"
	if len(groups) > 0 {
		fallback = strings.TrimSpace(groups[0].Name)
	}
	finalRules, ruleWarnings := normalizeRules(rules, groupNames, nodeSet, fallback)
	result.Warnings = append(result.Warnings, ruleWarnings...)

	doc, err := parseMapping(baseConfig)
	if err != nil {
		return result, fmt.Errorf("基础配置不是有效的 YAML 映射: %w", err)
	}

	proxyList := &yaml.Node{Kind: yaml.SequenceNode}
	for _, p := range proxies {
		if !nodeSet[Name(p)] {
			continue
		}
		node, err := proxyToNode(p)
		if err != nil {
			return result, err
		}
		proxyList.Content = append(proxyList.Content, node)
	}
	groupList := &yaml.Node{Kind: yaml.SequenceNode, Content: groupNodes}
	ruleList := &yaml.Node{Kind: yaml.SequenceNode}
	for _, r := range finalRules {
		ruleList.Content = append(ruleList.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: r})
	}

	setMappingKey(doc, "proxies", proxyList)
	setMappingKey(doc, "proxy-groups", groupList)
	if rp := strings.TrimSpace(cfg.RuleProviders); rp != "" {
		providers, err := parseMapping(rp)
		if err != nil {
			return result, fmt.Errorf("rule-providers 不是有效的 YAML 映射: %w", err)
		}
		if inner := mappingValue(providers, "rule-providers"); inner != nil {
			providers = inner
		}
		setMappingKey(doc, "rule-providers", providers)
	}
	setMappingKey(doc, "rules", ruleList)

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{doc}}); err != nil {
		return result, err
	}
	_ = enc.Close()
	result.Content = prettifyYAMLUnicode(buf.String())
	return result, nil
}

func buildGroupNode(g ProxyGroupSpec, nodeNames []string, nodeSet, groupNames map[string]bool) (*yaml.Node, []string, error) {
	warnings := []string{}
	name := strings.TrimSpace(g.Name)
	typ := strings.TrimSpace(g.Type)
	if typ == "" {
		typ = "select"
	}
	if !validGroupTypes[typ] {
		return nil, nil, fmt.Errorf("代理组「%s」的类型无效: %s", name, typ)
	}

	members := []string{}
	seen := map[string]bool{}
	add := func(m string) {
		if m == "" || m == name || seen[m] {
			return
		}
		seen[m] = true
		members = append(members, m)
	}
	for _, m := range g.Proxies {
		m = strings.TrimSpace(m)
		switch {
		case m == "":
		case builtinPolicies[m] || groupNames[m] || nodeSet[m]:
			add(m)
		default:
			warnings = append(warnings, fmt.Sprintf("代理组「%s」引用的「%s」不存在，已忽略", name, m))
		}
	}
	if g.IncludeAllNodes {
		var re *regexp.Regexp
		if f := strings.TrimSpace(g.Filter); f != "" {
			compiled, err := regexp.Compile(f)
			if err != nil {
				return nil, nil, fmt.Errorf("代理组「%s」的节点筛选正则无效: %v", name, err)
			}
			re = compiled
		}
		for _, n := range nodeNames {
			if !nodeSet[n] {
				continue
			}
			if re != nil && !re.MatchString(n) {
				continue
			}
			add(n)
		}
	}
	if len(members) == 0 {
		members = append(members, "DIRECT")
		warnings = append(warnings, fmt.Sprintf("代理组「%s」没有可用节点，已填入 DIRECT", name))
	}

	node := &yaml.Node{Kind: yaml.MappingNode}
	appendKV(node, "name", scalar(name))
	appendKV(node, "type", scalar(typ))
	if g.Icon != "" {
		appendKV(node, "icon", scalar(g.Icon))
	}
	if typ == "url-test" || typ == "fallback" || typ == "load-balance" {
		url := strings.TrimSpace(g.URL)
		if url == "" {
			url = defaultTestURL
		}
		interval := g.Interval
		if interval <= 0 {
			interval = 300
		}
		appendKV(node, "url", scalar(url))
		appendKV(node, "interval", intScalar(interval))
		if typ == "url-test" {
			tolerance := g.Tolerance
			if tolerance <= 0 {
				tolerance = 50
			}
			appendKV(node, "tolerance", intScalar(tolerance))
		}
		if typ == "load-balance" && g.Strategy != "" {
			appendKV(node, "strategy", scalar(g.Strategy))
		}
		if g.Lazy {
			appendKV(node, "lazy", boolScalar(true))
		}
	}
	if g.Hidden {
		appendKV(node, "hidden", boolScalar(true))
	}
	list := &yaml.Node{Kind: yaml.SequenceNode}
	for _, m := range members {
		list.Content = append(list.Content, scalar(m))
	}
	appendKV(node, "proxies", list)
	return node, warnings, nil
}

// normalizeRules makes sure every rule targets an existing policy and that
// the list ends with a MATCH rule.
func normalizeRules(rules []string, groupNames, nodeSet map[string]bool, fallback string) ([]string, []string) {
	warnings := []string{}
	out := make([]string, 0, len(rules)+1)
	hasMatch := false
	for _, raw := range rules {
		rule := strings.TrimSpace(raw)
		if rule == "" || strings.HasPrefix(rule, "#") {
			continue
		}
		rule = strings.TrimPrefix(strings.TrimPrefix(rule, "- "), "-")
		rule = strings.TrimSpace(rule)
		parts := splitRule(rule)
		if len(parts) < 2 {
			warnings = append(warnings, fmt.Sprintf("规则格式错误，已忽略: %s", rule))
			continue
		}
		ruleType := strings.ToUpper(strings.TrimSpace(parts[0]))
		targetIdx := len(parts) - 1
		for targetIdx > 1 && isRuleOption(parts[targetIdx]) {
			targetIdx--
		}
		if ruleType == "MATCH" || ruleType == "FINAL" {
			targetIdx = 1
		}
		target := strings.TrimSpace(parts[targetIdx])
		if !builtinPolicies[target] && !groupNames[target] && !nodeSet[target] {
			warnings = append(warnings, fmt.Sprintf("规则「%s」的目标「%s」不存在，已改为「%s」", rule, target, fallback))
			parts[targetIdx] = fallback
			rule = strings.Join(parts, ",")
		}
		if ruleType == "MATCH" || ruleType == "FINAL" {
			hasMatch = true
			out = append(out, "MATCH,"+strings.TrimSpace(parts[1]))
			break
		}
		out = append(out, rule)
	}
	if !hasMatch {
		out = append(out, "MATCH,"+fallback)
	}
	return out, warnings
}

func isRuleOption(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "no-resolve", "src":
		return true
	}
	return false
}

// splitRule splits a rule on commas that are not inside parentheses, so
// logical rules such as AND,((DOMAIN,a.com),(NETWORK,UDP)),DIRECT work.
func splitRule(rule string) []string {
	parts := []string{}
	depth := 0
	start := 0
	for i, r := range rule {
		switch r {
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 {
				parts = append(parts, rule[start:i])
				start = i + 1
			}
		}
	}
	parts = append(parts, rule[start:])
	return parts
}

func parseMapping(text string) (*yaml.Node, error) {
	var root yaml.Node
	if err := yaml.Unmarshal([]byte(text), &root); err != nil {
		return nil, err
	}
	if root.Kind == 0 {
		return &yaml.Node{Kind: yaml.MappingNode}, nil
	}
	if root.Kind != yaml.DocumentNode || len(root.Content) == 0 {
		return nil, fmt.Errorf("不是 YAML 文档")
	}
	doc := root.Content[0]
	if doc.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("顶层必须是键值映射")
	}
	return doc, nil
}

func mappingValue(m *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

func setMappingKey(m *yaml.Node, key string, value *yaml.Node) {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			// Move the key to the end so proxies/groups/rules keep their order.
			m.Content = append(m.Content[:i], m.Content[i+2:]...)
			break
		}
	}
	appendKV(m, key, value)
}

func appendKV(m *yaml.Node, key string, value *yaml.Node) {
	m.Content = append(m.Content, scalar(key), value)
}

func scalar(v string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: v}
}

func intScalar(v int) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: fmt.Sprint(v)}
}

func boolScalar(v bool) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: fmt.Sprint(v)}
}

func proxyToNode(p Proxy) (*yaml.Node, error) {
	node := &yaml.Node{Kind: yaml.MappingNode}
	for _, k := range orderedKeys(p) {
		var value yaml.Node
		if err := value.Encode(p[k]); err != nil {
			return nil, fmt.Errorf("节点「%s」字段 %s 无法编码: %w", Name(p), k, err)
		}
		appendKV(node, k, &value)
	}
	return node, nil
}

// RenderProxiesOnly renders a minimal YAML that only contains `proxies:`
// (used for previews and proxy-provider style exports).
func RenderProxiesOnly(nodes []Proxy) (string, error) {
	proxies := UniqueNames(nodes)
	list := &yaml.Node{Kind: yaml.SequenceNode}
	for _, p := range proxies {
		node, err := proxyToNode(p)
		if err != nil {
			return "", err
		}
		list.Content = append(list.Content, node)
	}
	doc := &yaml.Node{Kind: yaml.MappingNode}
	appendKV(doc, "proxies", list)
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{doc}}); err != nil {
		return "", err
	}
	_ = enc.Close()
	return prettifyYAMLUnicode(buf.String()), nil
}

// ValidateClashDocument checks that uploaded content is a YAML mapping with
// at least one of proxies / proxy-providers.
func ValidateClashDocument(content []byte) (int, error) {
	doc, err := parseMapping(string(content))
	if err != nil {
		return 0, fmt.Errorf("不是有效的 Clash YAML: %w", err)
	}
	proxies := mappingValue(doc, "proxies")
	providers := mappingValue(doc, "proxy-providers")
	if proxies == nil && providers == nil {
		return 0, fmt.Errorf("配置中没有 proxies 或 proxy-providers")
	}
	count := 0
	if proxies != nil && proxies.Kind == yaml.SequenceNode {
		count = len(proxies.Content)
	}
	return count, nil
}

// prettifyYAMLUnicode turns the \UXXXXXXXX escapes yaml.v3 emits for
// characters outside the BMP (emoji such as 🚀 in group names) back into the
// characters themselves. The result is re-parsed and compared with the input;
// if anything differs the original text is returned, so the data never changes.
const backslash = byte(0x5c)

func prettifyYAMLUnicode(text string) string {
	if !strings.Contains(text, string(backslash)+"U") {
		return text
	}
	var b strings.Builder
	b.Grow(len(text))
	changed := false
	for i := 0; i < len(text); i++ {
		c := text[i]
		if c != backslash {
			b.WriteByte(c)
			continue
		}
		// Count the run of backslashes; only an odd run starts an escape.
		j := i
		for j < len(text) && text[j] == backslash {
			j++
		}
		run := j - i
		b.WriteString(text[i : j-1])
		i = j - 1
		if run%2 == 1 && j+9 <= len(text) && text[j] == 'U' {
			if r, err := strconv.ParseUint(text[j+1:j+9], 16, 32); err == nil && r >= 0x10000 && r <= unicode.MaxRune && unicode.IsGraphic(rune(r)) {
				b.WriteRune(rune(r))
				i = j + 8
				changed = true
				continue
			}
		}
		b.WriteByte(backslash)
	}
	if !changed {
		return text
	}
	out := b.String()
	var before, after interface{}
	if yaml.Unmarshal([]byte(text), &before) != nil || yaml.Unmarshal([]byte(out), &after) != nil || !reflect.DeepEqual(before, after) {
		return text
	}
	return out
}
