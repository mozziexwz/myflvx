package proxykit

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestRenderKeepsEmojiReadable(t *testing.T) {
	res, err := Render(GeneratorConfig{Template: "standard"}, []Proxy{{"name": "🇭🇰 香港 01", "type": "socks5", "server": "1.1.1.1", "port": 1080}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(res.Content, `\U0001F`) {
		t.Fatalf("emoji should not be escaped:\n%s", res.Content)
	}
	if !strings.Contains(res.Content, "🚀 节点选择") || !strings.Contains(res.Content, "🇭🇰 香港 01") {
		t.Fatalf("expected readable emoji names:\n%s", res.Content)
	}
	var doc map[string]any
	if err := yaml.Unmarshal([]byte(res.Content), &doc); err != nil {
		t.Fatalf("prettified yaml invalid: %v", err)
	}
}

func TestPrettifyYAMLUnicodeKeepsEscapedBackslashes(t *testing.T) {
	// `x\\U0001F680` is a literal backslash followed by text; only the second,
	// unescaped `\U0001F680` may become 🚀.
	in := `a: "x\\U0001F680 \U0001F680"` + "\n"
	out := prettifyYAMLUnicode(in)
	var before, after map[string]any
	if err := yaml.Unmarshal([]byte(in), &before); err != nil {
		t.Fatal(err)
	}
	if err := yaml.Unmarshal([]byte(out), &after); err != nil {
		t.Fatalf("output invalid: %v (%q)", err, out)
	}
	if before["a"] != after["a"] {
		t.Fatalf("value changed: %q -> %q", before["a"], after["a"])
	}
	if !strings.Contains(out, `x\\U0001F680 🚀`) {
		t.Fatalf("expected only the unescaped sequence to be replaced: %q", out)
	}
}
