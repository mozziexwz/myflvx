package proxykit

import (
	"fmt"
	"net/url"
	"reflect"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

type dnsProxyReference struct {
	target          string
	server          *yaml.Node
	defaultUpstream bool
}

func dnsProxyReferences(dns *yaml.Node) []dnsProxyReference {
	if dns == nil || dns.Kind != yaml.MappingNode {
		return nil
	}
	var refs []dnsProxyReference
	for _, field := range []string{
		"nameserver", "fallback", "default-nameserver", "direct-nameserver", "proxy-server-nameserver",
		"nameserver-policy", "proxy-server-nameserver-policy",
	} {
		seen := map[*yaml.Node]bool{}
		var walk func(*yaml.Node)
		walk = func(node *yaml.Node) {
			if node == nil || seen[node] {
				return
			}
			seen[node] = true
			switch node.Kind {
			case yaml.ScalarNode:
				if target := dnsProxyTarget(node.Value); target != "" {
					refs = append(refs, dnsProxyReference{target, node, field == "nameserver" || field == "fallback"})
				}
			case yaml.AliasNode:
				walk(node.Alias)
			case yaml.SequenceNode:
				for _, child := range node.Content {
					walk(child)
				}
			case yaml.MappingNode:
				for i := 1; i < len(node.Content); i += 2 {
					walk(node.Content[i])
				}
			}
		}
		walk(mappingValue(dns, field))
	}
	return refs
}

func dnsProxyTarget(server string) string {
	_, fragment, found := strings.Cut(server, "#")
	if !found {
		return ""
	}
	decoded, err := url.PathUnescape(fragment)
	if err != nil {
		return ""
	}
	// Mihomo uses the last fragment segment without an assignment as the proxy or interface name.
	var target string
	for _, part := range strings.Split(decoded, "&") {
		if !strings.Contains(part, "=") {
			target = part
		}
	}
	return target
}

func isBuiltinOverseasDNSGroup(group ProxyGroupSpec) bool {
	if len(group.Proxies) != 0 {
		return false
	}
	group.Name = strings.TrimSpace(group.Name)
	group.Proxies = nil
	return reflect.DeepEqual(group, overseasDNSGroup())
}

func appendBuiltinDNSGroup(groups []ProxyGroupSpec, groupNames, allNodeNames map[string]bool, refs []dnsProxyReference) ([]ProxyGroupSpec, string, string) {
	groups = slices.Clone(groups)
	if i := groupIndex(groups, groupOverseasDNS); i >= 0 && isBuiltinOverseasDNSGroup(groups[i]) {
		groups = slices.Delete(groups, i, i+1)
		delete(groupNames, groupOverseasDNS)
	}
	group := overseasDNSGroup()
	var warning string
	if groupNames[group.Name] || allNodeNames[group.Name] {
		base := group.Name + " (内置)"
		group.Name = base
		for i := 2; groupNames[group.Name] || allNodeNames[group.Name]; i++ {
			group.Name = fmt.Sprintf("%s %d", base, i)
		}
		// The built-in document has a bare group fragment on both overseas resolvers.
		for i := range refs {
			if refs[i].target == groupOverseasDNS {
				endpoint, _, _ := strings.Cut(refs[i].server.Value, "#")
				refs[i].server.Value = endpoint + "#" + group.Name
				refs[i].target = group.Name
			}
		}
		warning = fmt.Sprintf("内置 DNS 组「%s」与现有名称冲突，导出时使用「%s」", groupOverseasDNS, group.Name)
	}
	groupNames[group.Name] = true
	return append(groups, group), group.Name, warning
}

func isRealProxyNode(proxy Proxy) bool {
	switch Type(proxy) {
	case "", "direct", "compatible", "reject", "reject-drop", "pass", "dns", "rematch":
		return false
	default:
		return true
	}
}

func targetMayReachDirect(target string, resolvedMembers map[string][]string, directNodes map[string]bool) bool {
	pending := []string{target}
	seen := map[string]bool{}
	for len(pending) > 0 {
		name := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if seen[name] {
			continue
		}
		seen[name] = true
		if name == "DIRECT" || name == "COMPATIBLE" || name == "GLOBAL" || directNodes[name] {
			return true
		}
		pending = append(pending, resolvedMembers[name]...)
	}
	return false
}

func dnsProxyWarnings(refs []dnsProxyReference, groupNames, nodeSet map[string]bool, resolvedMembers map[string][]string, directNodes map[string]bool) []string {
	var warnings []string
	seen := map[string]bool{}
	for _, ref := range refs {
		var warning string
		switch {
		case ref.target == "RULES" || builtinPolicies[ref.target]:
			continue
		case !groupNames[ref.target] && !nodeSet[ref.target]:
			warning = fmt.Sprintf("DNS 上游引用「%s」不是导出的代理名，Mihomo 会将其作为网卡名使用", ref.target)
		case ref.defaultUpstream && targetMayReachDirect(ref.target, resolvedMembers, directNodes):
			warning = fmt.Sprintf("DNS 上游引用「%s」可走直连，请检查节点和子组", ref.target)
		}
		if warning != "" && !seen[warning] {
			seen[warning] = true
			warnings = append(warnings, warning)
		}
	}
	return warnings
}
