package yamlprep

import (
	"bytes"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
	"github.com/goccy/go-yaml/parser"
)

func TestQuoteBracePlaceholders(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"after a mapping value", "a: {{x}}\n", "a: '{{x}}'\n"},
		{"spaced with a comment", "a: {{ .Release.Name }} # c\n", "a: '{{ .Release.Name }}' # c\n"},
		{"after a sequence entry", "- {{x}}\n- b\n", "- '{{x}}'\n- b\n"},
		{"inside a flow sequence", "k: [a, {{y}}, {{z}}w]\n", "k: [a, '{{y}}', '{{z}}w']\n"},
		{"first in a flow sequence", "k: [{{y}}]\n", "k: ['{{y}}']\n"},
		{"glued suffix", "a: {{x}}-suffix\n", "a: '{{x}}-suffix'\n"},
		{"glued suffix then comment", "a: {{x}}-suffix # c\n", "a: '{{x}}-suffix' # c\n"},
		{"apostrophe doubled", "a: {{it's}}\n", "a: '{{it''s}}'\n"},
		{"tab separator", "a:\t{{x}}\n", "a:\t'{{x}}'\n"},
		{"multibyte key", "é: {{x}}\n", "é: '{{x}}'\n"},
		{"after a block scalar", "a: |\n  {{y}}\nb: {{z}}\n", "a: |\n  {{y}}\nb: '{{z}}'\n"},
		{"crlf", "a: {{x}}\r\nb: {{y}}\r\n", "a: '{{x}}'\r\nb: '{{y}}'\r\n"},
		{"crlf after comments", "# a\r\n# b\r\n# c\r\njobs:\r\n- curlies: {{x}} # d\r\n  flow: [{{y}}]\r\n", "# a\r\n# b\r\n# c\r\njobs:\r\n- curlies: '{{x}}' # d\r\n  flow: ['{{y}}']\r\n"},
		{"glued hash", "a: {{x}}#c\n", "a: '{{x}}#c'\n"},
		{"glued hash then comment", "a: {{base}}#frag # real\n", "a: '{{base}}#frag' # real\n"},
		{"glued hash in a flow sequence", "k: [{{a}}#x, b]\n", "k: ['{{a}}#x', b]\n"},
		{"comma inside", "a: {{x, y}}\n", "a: '{{x, y}}'\n"},
		{"colon inside", "a: {{x: 1}}\n", "a: '{{x: 1}}'\n"},
		{"concourse", "jobs:\n- name: thing1\n  curlies: {{my-variable_123}}\n", "jobs:\n- name: thing1\n  curlies: '{{my-variable_123}}'\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := string(QuoteBracePlaceholders([]byte(c.in))); got != c.want {
				t.Fatalf("QuoteBracePlaceholders(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func TestQuoteBracePlaceholdersLeavesOtherFormsAlone(t *testing.T) {
	for _, in := range []string{
		"{{x}}: 1\n",                  // a mapping key
		"- {{x}}: 1\n",                // a key inside a sequence entry
		"{a: 1, {{x}}: 2}\n",          // a key inside a flow mapping
		"{{{{\n",                      // unbalanced
		"a: {{x}\n",                   // unbalanced
		"a: {{x}}#c: d\n",             // glued text that reads as a key
		"a: {{ x\n  y }}\n",           // spans two lines
		"a: &n {{x}}\n",               // after an anchor
		"a: !t {{x}}\n",               // after a tag
		"a: |\n  {{y}}\n",             // inside a block scalar
		"c: \"{{z}}\"\n",              // already double-quoted
		"c: '{{z}}'\n",                // already single-quoted
		"p: (( grab a.{{seg}}.b ))\n", // operator text
		"# {{x}}\n",                   // a comment
		"plain: no braces here\n",
	} {
		if got := QuoteBracePlaceholders([]byte(in)); string(got) != in {
			t.Errorf("QuoteBracePlaceholders(%q) = %q, want it unchanged", in, got)
		}
	}
}

func TestQuoteBracePlaceholdersZeroCopy(t *testing.T) {
	in := []byte("a: 1\nb: [x]\n")
	out := QuoteBracePlaceholders(in)
	if !bytes.Equal(in, out) || &out[0] != &in[0] {
		t.Fatal("input without {{ must come back as the original slice")
	}
}

func TestQuoteBracePlaceholdersKeepsUnbalancedFixturesBroken(t *testing.T) {
	for _, path := range []string{
		"../../assets/vaultinfo/improper.yml",
		"../../tests/spruce-compat/vaultinfo/invalid.yml",
	} {
		in, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		out := QuoteBracePlaceholders(in)
		if !bytes.Equal(in, out) {
			t.Errorf("%s changed: %q", path, out)
		}
		if _, err := parser.ParseBytes(out, 0); err == nil {
			t.Errorf("%s must stay a parse error", path)
		}
	}
}

func TestQuotedPlaceholdersParseAsStrings(t *testing.T) {
	src := QuoteBracePlaceholders([]byte("a: {{x}}\nb: {{x}}-suffix\nc: [{{y}}]\nd: {{it's}}\ne: {{base}}#frag # real\n"))
	var got map[string]interface{}
	if err := yaml.Unmarshal(src, &got); err != nil {
		t.Fatalf("rewritten input failed to parse: %v\n%s", err, src)
	}
	want := map[string]interface{}{
		"a": "{{x}}",
		"b": "{{x}}-suffix",
		"c": []interface{}{"{{y}}"},
		"d": "{{it's}}",
		"e": "{{base}}#frag",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parsed %#v, want %#v", got, want)
	}
}

// FuzzQuoteBracePlaceholders checks two promises of the rewrite. Input
// that goccy parsed before the rewrite still parses after it, and CRLF
// input gets the same quotes as its LF form while keeping its line
// breaks.
func FuzzQuoteBracePlaceholders(f *testing.F) {
	for _, seed := range []string{
		"a: {{x}}\n", "- {{x}}\n", "k: [a, {{y}}, {{z}}w]\n", "a: {{x}}-s # c\n", "a: {{x}}#c\n",
		"k: [{{a}}#x, b]\n", "# c\na: |\n  {{y}}\nb: {{z}}\n", "{{x}}: 1\n", "a: {{it's}}\n",
		"a: {{base}}#frag # real\n", "a: {{x}}#c: d\n", "a: {x: {{y}}}\n",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, in string) {
		if strings.Contains(in, "\r") {
			return
		}
		out := QuoteBracePlaceholders([]byte(in))
		if _, err := parser.ParseBytes([]byte(in), 0); err == nil {
			if _, err := parser.ParseBytes(out, 0); err != nil {
				t.Fatalf("%q parsed before the rewrite, but %q fails: %v", in, out, err)
			}
		}
		crlf := strings.ReplaceAll(in, "\n", "\r\n")
		if got, want := string(QuoteBracePlaceholders([]byte(crlf))), strings.ReplaceAll(string(out), "\n", "\r\n"); got != want {
			t.Fatalf("CRLF input %q became %q, want %q", crlf, got, want)
		}
	})
}
