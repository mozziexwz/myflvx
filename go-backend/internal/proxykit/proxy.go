// Package proxykit holds the protocol-level logic of the myflvx subscription
// features: Clash/mihomo proxy maps, share links, subscription content parsing
// and rendering, and the mihomo_script deployment helpers. It has no database
// or HTTP dependencies so it can be unit tested in isolation.
package proxykit

import (
	"encoding/json"
	"fmt"
	"math"
	"net"
	"sort"
	"strconv"
	"strings"
)

// Proxy is a Clash/mihomo proxy definition (one entry of `proxies:`).
type Proxy = map[string]any

// SupportedTypes lists the proxy types the panel knows how to display. Other
// types are still accepted and passed through to subscriptions untouched.
var SupportedTypes = []string{
	"ss", "ssr", "vmess", "vless", "trojan", "hysteria", "hysteria2", "tuic",
	"anytls", "mieru", "sudoku", "wireguard", "socks5", "http", "snell", "naive",
}

// CloneProxy returns a deep copy of p.
func CloneProxy(p Proxy) Proxy {
	if p == nil {
		return nil
	}
	raw, err := json.Marshal(p)
	if err != nil {
		out := make(Proxy, len(p))
		for k, v := range p {
			out[k] = v
		}
		return out
	}
	var out Proxy
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil
	}
	return NormalizeNumbers(out)
}

// NormalizeNumbers converts float64 values that hold integers (as produced by
// encoding/json) back to int so YAML output does not contain "443.0"-style
// numbers and port comparisons work.
func NormalizeNumbers(p Proxy) Proxy {
	for k, v := range p {
		p[k] = normalizeValue(v)
	}
	return p
}

func normalizeValue(v any) any {
	switch t := v.(type) {
	case float64:
		if t == math.Trunc(t) && math.Abs(t) < 1<<53 {
			return int(t)
		}
		return t
	case map[string]any:
		for k, item := range t {
			t[k] = normalizeValue(item)
		}
		return t
	case []any:
		for i, item := range t {
			t[i] = normalizeValue(item)
		}
		return t
	default:
		return v
	}
}

// GetString returns the string form of p[key].
func GetString(p Proxy, key string) string {
	if p == nil {
		return ""
	}
	switch v := p[key].(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(v)
	case fmt.Stringer:
		return strings.TrimSpace(v.String())
	default:
		return strings.TrimSpace(fmt.Sprint(v))
	}
}

// GetInt returns p[key] as an int (0 when missing or not numeric).
func GetInt(p Proxy, key string) int {
	if p == nil {
		return 0
	}
	switch v := p[key].(type) {
	case int:
		return v
	case int64:
		return int(v)
	case int32:
		return int(v)
	case uint16:
		return int(v)
	case float64:
		return int(v)
	case json.Number:
		n, _ := v.Int64()
		return int(n)
	case string:
		n, _ := strconv.Atoi(strings.TrimSpace(v))
		return n
	default:
		return 0
	}
}

// GetBool returns p[key] as a bool.
func GetBool(p Proxy, key string) bool {
	if p == nil {
		return false
	}
	switch v := p[key].(type) {
	case bool:
		return v
	case string:
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "1", "true", "yes", "on":
			return true
		}
	case int:
		return v != 0
	case float64:
		return v != 0
	}
	return false
}

// Name returns the proxy name.
func Name(p Proxy) string { return GetString(p, "name") }

// Type returns the lower-cased proxy type.
func Type(p Proxy) string { return strings.ToLower(GetString(p, "type")) }

// Server returns the proxy server address.
func Server(p Proxy) string { return GetString(p, "server") }

// Port returns the proxy port.
func Port(p Proxy) int { return GetInt(p, "port") }

// ValidateProxy checks the fields every proxy needs.
func ValidateProxy(p Proxy) error {
	if p == nil {
		return fmt.Errorf("节点配置为空")
	}
	if Name(p) == "" {
		return fmt.Errorf("节点名称不能为空")
	}
	if Type(p) == "" {
		return fmt.Errorf("节点类型不能为空")
	}
	if Server(p) == "" {
		return fmt.Errorf("节点地址不能为空")
	}
	if Port(p) <= 0 || Port(p) > 65535 {
		if GetString(p, "port-range") == "" && GetString(p, "ports") == "" {
			return fmt.Errorf("节点端口无效")
		}
	}
	return nil
}

// PrepareProxy normalises a proxy coming from user input or a parser:
// numbers become ints, the type is lower-cased and the port is an int.
func PrepareProxy(p Proxy) (Proxy, error) {
	if p == nil {
		return nil, fmt.Errorf("节点配置为空")
	}
	p = CloneProxy(p)
	if p == nil {
		return nil, fmt.Errorf("节点配置无效")
	}
	p["type"] = Type(p)
	if port := GetInt(p, "port"); port > 0 {
		p["port"] = port
	}
	if name := Name(p); name != "" {
		p["name"] = name
	}
	if server := Server(p); server != "" {
		p["server"] = strings.Trim(server, "[]")
	}
	if err := ValidateProxy(p); err != nil {
		return nil, err
	}
	return p, nil
}

// WithEndpoint returns a copy of p that connects to host:port instead, used
// for relay (tunnel) nodes.
func WithEndpoint(p Proxy, name, host string, port int) Proxy {
	out := CloneProxy(p)
	if out == nil {
		out = Proxy{}
	}
	out["name"] = name
	out["server"] = strings.Trim(strings.TrimSpace(host), "[]")
	out["port"] = port
	// A relay listens on a single port, so drop port hopping settings.
	delete(out, "port-range")
	delete(out, "ports")
	return out
}

// HostPort formats host:port, bracketing IPv6 literals.
func HostPort(host string, port int) string {
	host = strings.Trim(strings.TrimSpace(host), "[]")
	return net.JoinHostPort(host, strconv.Itoa(port))
}

// orderedKeys returns the keys of p with the common identity fields first.
func orderedKeys(p Proxy) []string {
	priority := []string{"name", "type", "server", "port"}
	seen := make(map[string]bool, len(p))
	keys := make([]string, 0, len(p))
	for _, k := range priority {
		if _, ok := p[k]; ok {
			keys = append(keys, k)
			seen[k] = true
		}
	}
	rest := make([]string, 0, len(p))
	for k := range p {
		if !seen[k] {
			rest = append(rest, k)
		}
	}
	sort.Strings(rest)
	return append(keys, rest...)
}
