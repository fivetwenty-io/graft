package yq

import "testing"

func TestYQKeysAndHas(t *testing.T) {
	in := "x: &k foo\n*k : 1\n2: two\nm: {a: 1, b: 2} # cm\ns: [1, 2] # cs\nn: 3\n"
	runMergeCases(t, in, []mergeCase{
		{"keys", "- x\n- *k\n- 2\n- m\n- s\n- n\n", ""},
		{"keys | .[1] | alias", "k\n", ""},
		{"keys | .[0] | line", "1\n", ""},
		{"keys | .[0] | key", "0\n", ""},
		{"keys | path", "[]\n", ""},
		{".s | keys", "- 0\n- 1\n", ""},
		{".m | keys", "- a\n- b\n", ""},
		{".n | keys", "", "cannot get keys of !!int, keys only works for maps and arrays"},
		{".zz | keys", "", "cannot get keys of !!null, keys only works for maps and arrays"},
		{"has(\"x\")", "true\n", ""},
		{"has(\"foo\")", "false\n", ""},
		{"has(\"2\")", "true\n", ""},
		{"has(2)", "true\n", ""},
		{"has(null)", "false\n", ""},
		{".s | has(1)", "true\n", ""},
		{".s | has(2)", "false\n", ""},
		{".s | has(-1)", "false\n", ""},
		{".s | has(\"1\")", "false\n", ""},
		{".n | has(\"a\")", "false\n", ""},
		{".zz | has(\"a\")", "false\n", ""},
		{".m | has(.a)", "false\n", ""},
	})
}

func TestYQEntries(t *testing.T) {
	in := "# head\na: 1 # la\nb: [1] # lb\nc: {d: 2}\nw: !foo [{key: a, value: 1}]\nf: &an [{key: a, value: 1}] # cf\nt: [1]\nu: [{value: 1}]\nv: [{key: a}]\n"
	runMergeCases(t, in, []mergeCase{
		{".c | to_entries", "- key: d\n  value: 2\n", ""},
		{".b | to_entries", "- key: 0\n  value: 1\n", ""},
		{".b | to_entries | .[0].key | tag", "!!int\n", ""},
		{".c | to_entries | .[0].key | path", "- c\n- 0\n- key\n", ""},
		{".c | to_entries | .[0].value | path", "- c\n- 0\n- value\n", ""},
		{".c | to_entries | path", "- c\n", ""},
		{".a | to_entries", "", "!!int has no keys"},
		{".zz | to_entries", "", ""},
		{".f | from_entries", "&an {a: 1} # cf\n", ""},
		{".w | from_entries", "!!map {a: 1}\n", ""},
		{".f | from_entries | path", "- f\n", ""},
		{".t | from_entries", "", "expected to find one 'key' entry but found 0 in position 0"},
		{".u | from_entries", "", "expected to find one 'key' entry but found 0 in position 0"},
		{".v | from_entries", "", "expected to find one 'value' entry but found 0 in position 0"},
		{".c | from_entries", "", "from entries only runs against arrays"},
		{".zz | from_entries", "", "from entries only runs against arrays"},
		{"[] | from_entries", "{}\n", ""},
		{"[{\"key\": 2, \"value\": \"x\"}, {\"key\": null, \"value\": 1}] | from_entries", "2: x\nnull: 1\n", ""},
		{".c | to_entries | from_entries", "d: 2\n", ""},
		{".c | with_entries(.)", "d: 2\n", ""},
		{".b | with_entries(.)", "0: 1\n", ""},
		{".c | with_entries(.value += 1)", "d: 3\n", ""},
		{".c | with_entries(.key |= . + \"x\")", "dx: 2\n", ""},
		{"with_entries(select(.key == \"a\"))", "# head\na: 1 # la\n", ""},
		{".c | with_entries(.) | path", "[]\n", ""},
		{"collect(.a, .b[])", "- 1 # la\n- 1\n", ""},
		{".c | collect(.d) | path", "- c\n", ""},
		{".c | collect(.zz)", "- null\n", ""},
	})
}

func TestYQPickAndOmit(t *testing.T) {
	in := "# head\na: 1 # la\n2: two\n\"3\": three\nm: &am {x: 1, y: 2} # cm\ns: [1, 2, 3] # cs\ne: # ce\n  q: 1\nn: 3\n"
	runMergeCases(t, in, []mergeCase{
		{"pick([\"m\", \"a\"])", "# head\nm: &am {x: 1, y: 2} # cm\na: 1 # la\n", ""},
		{"pick([\"a\", \"zz\"])", "# head\na: 1 # la\n", ""},
		{"pick([2])", "# head\n2: two\n", ""},
		{"pick([\"2\"])", "# head\n{}\n", ""},
		{"pick([\"3\"])", "# head\n\"3\": three\n", ""},
		{"pick([3])", "# head\n{}\n", ""},
		{"pick([])", "# head\n{}\n", ""},
		{".m | pick([\"y\"])", "&am {y: 2} # cm\n", ""},
		{".m | pick([\"y\"]) | path", "- m\n", ""},
		{".s | pick([2, 0, 7, -1])", "[3, 1] # cs\n", ""},
		{".s | pick([\"1\"])", "[2] # cs\n", ""},
		{".s | pick([\"a\"])", "", "cannot index array with a"},
		{".n | pick([\"a\"])", "", "cannot pick indices from type !!int (n)"},
		{".zz | pick([\"a\"])", "", "cannot pick indices from type !!null (zz)"},
		{"omit([\"m\", \"s\", \"e\", \"n\"])", "# head\na: 1 # la\n2: two\n\"3\": three\n", ""},
		{"omit([2])", "# head\na: 1 # la\n\"3\": three\nm: &am {x: 1, y: 2} # cm\ns: [1, 2, 3] # cs\ne: # ce\n  q: 1\nn: 3\n", ""},
		{"omit([\"3\"])", "# head\na: 1 # la\n2: two\nm: &am {x: 1, y: 2} # cm\ns: [1, 2, 3] # cs\ne: # ce\n  q: 1\nn: 3\n", ""},
		{"omit([])", "# head\na: 1 # la\n2: two\n\"3\": three\nm: &am {x: 1, y: 2} # cm\ns: [1, 2, 3] # cs\ne: # ce\n  q: 1\nn: 3\n", ""},
		{".m | omit([\"x\"])", "&am {y: 2} # cm\n", ""},
		{".m | omit([\"x\"]) | path", "- m\n", ""},
		{".s | omit([0, 2])", "[2] # cs\n", ""},
		{".s | omit([\"0\"])", "[1, 2, 3] # cs\n", ""},
		{".n | omit([\"a\"])", "3\n", ""},
		{".zz | omit([\"a\"])", "null\n", ""},
		{".m | omit([\"x\", \"y\"])", "&am {} # cm\n", ""},
	})
}

func TestYQDelete(t *testing.T) {
	in := "# head\na: 1 # la\n2: two\n\"3\": three\nm: &am {x: 1, y: 2} # cm\ns: [1, 2, 3] # cs\ne: # ce\n  q: 1\nn: 3\n"
	runMergeCases(t, in, []mergeCase{
		{"del(.a)", "# head\n2: two\n\"3\": three\nm: &am {x: 1, y: 2} # cm\ns: [1, 2, 3] # cs\ne: # ce\n  q: 1\nn: 3\n", ""},
		{"del(.m.x, .s[0])", "# head\na: 1 # la\n2: two\n\"3\": three\nm: &am {y: 2} # cm\ns: [2, 3] # cs\ne: # ce\n  q: 1\nn: 3\n", ""},
		{"del(.[\"3\"])", "# head\na: 1 # la\n2: two\nm: &am {x: 1, y: 2} # cm\ns: [1, 2, 3] # cs\ne: # ce\n  q: 1\nn: 3\n", ""},
		{"del(.s[-1])", "# head\na: 1 # la\n2: two\n\"3\": three\nm: &am {x: 1, y: 2} # cm\ns: [1, 2] # cs\ne: # ce\n  q: 1\nn: 3\n", ""},
		{"del(.s[])", "# head\na: 1 # la\n2: two\n\"3\": three\nm: &am {x: 1, y: 2} # cm\ns: [] # cs\ne: # ce\n  q: 1\nn: 3\n", ""},
		{"del(.m[])", "# head\na: 1 # la\n2: two\n\"3\": three\nm: &am {} # cm\ns: [1, 2, 3] # cs\ne: # ce\n  q: 1\nn: 3\n", ""},
		{"del(.e.q)", "# head\na: 1 # la\n2: two\n\"3\": three\nm: &am {x: 1, y: 2} # cm\ns: [1, 2, 3] # cs\ne: {} # ce\nn: 3\n", ""},
		{"del(.zz)", "# head\na: 1 # la\n2: two\n\"3\": three\nm: &am {x: 1, y: 2} # cm\ns: [1, 2, 3] # cs\ne: # ce\n  q: 1\nn: 3\n", ""},
		{"del(.n.x)", "# head\na: 1 # la\n2: two\n\"3\": three\nm: &am {x: 1, y: 2} # cm\ns: [1, 2, 3] # cs\ne: # ce\n  q: 1\nn: 3\n", ""},
		{".m | del(.)", "&am {x: 1, y: 2} # cm\n", ""},
		{".m | del(.x)", "&am {y: 2} # cm\n", ""},
		{".m | del(.x) | path", "- m\n", ""},
		{".s | del(.[0], .[1])", "[3] # cs\n", ""},
		{"del(.s[] | select(. > 1))", "# head\na: 1 # la\n2: two\n\"3\": three\nm: &am {x: 1, y: 2} # cm\ns: [1] # cs\ne: # ce\n  q: 1\nn: 3\n", ""},
		{"del(.. | select(. == 1))", "# head\n2: two\n\"3\": three\nm: &am {y: 2} # cm\ns: [2, 3] # cs\ne: {} # ce\nn: 3\n", ""},
		{"del(.)", "", ""},
	})
}
