package controlflow

import "testing"

// TestRewriteVarRefs_PathContinuations pins which names rewriteVarRefs
// treats as a loop variable. A name is the variable only where the
// expression lexer reads a name that the evaluator resolves from the top
// of the document. That is a bare identifier, the first segment of a
// reference path, or the first segment of a path inside a bracket index.
// A name that continues a path, an environment variable, the value side
// of a "field=value" selector, and a name after "$." all stay as written.
func TestRewriteVarRefs_PathContinuations(t *testing.T) {
	const target = "__graft_loop.l1.0.svc"
	targets := map[string]string{"svc": target}
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"bare variable", "grab svc", "grab " + target},
		{"variable with a dotted tail", "grab svc.ip", "grab " + target + ".ip"},
		{"variable with a slash in its tail", "grab svc.ip/0", "grab " + target + ".ip/0"},
		{"variable with a bracket index", "grab svc[0]", "grab " + target + "[0]"},
		{"name after a slash in a path", "grab jobs.web/svc.ip", "grab jobs.web/svc.ip"},
		{"name after a hyphen in a path", "grab jobs.web-svc.ip", "grab jobs.web-svc.ip"},
		{"name after a slash after a bracket", "grab arr[0]/svc", "grab arr[0]/svc"},
		{"name after a dot after a bracket", "grab arr[0].svc", "grab arr[0].svc"},
		{"name after a slash after a dot", "grab arr.0./svc", "grab arr.0./svc"},
		{"name after a slash after a digit", "grab 0/svc", "grab 0/svc"},
		{"name after a dollar name after a slash", "grab a/$X.svc", "grab a/$X.svc"},
		{"name glued by a plus in a dotted segment", "grab a.b+svc", "grab a.b+svc"},
		{"slash segment that starts with the name", "grab svc/0", "grab svc/0"},
		{"hyphenated name that starts with the name", "grab svc-name", "grab svc-name"},
		{"unspaced subtraction reads as one name", "grab svc-1", "grab svc-1"},
		{"spaced subtraction", "svc - 1", target + " - 1"},
		{"spaced division by the variable", "a / svc", "a / " + target},
		{"spaced division of a number by the variable", "4 / svc", "4 / " + target},
		{"division with a space before the slash", "a /svc", "a /" + target},
		{"division after a closing paren", "(a)/svc", "(a)/" + target},
		{"subtraction after a bracket", "arr[0]-svc", "arr[0]-" + target},
		{"subtraction after a number", "5-svc", "5-" + target},
		{"variable inside an index", "grab list[svc]", "grab list[" + target + "]"},
		{"variable path inside an index", "grab list[svc.name]", "grab list[" + target + ".name]"},
		{"variable inside a root index", "grab $[svc]", "grab $[" + target + "]"},
		{"variable in a quoted string is left alone", `concat "svc/" svc`, `concat "svc/" ` + target},
		{"variable on both sides of a string", `concat svc "/" svc`, "concat " + target + ` "/" ` + target},
		{"left of ||", "svc || x", target + " || x"},
		{"right of ||", "x || svc", "x || " + target},
		{"ternary on the variable", `svc ? "a" : "b"`, target + ` ? "a" : "b"`},
		{"ternary branches", "t ? svc : svc.ip", "t ? " + target + " : " + target + ".ip"},
		{"leading slash begins a key", "grab /svc", "grab /svc"},
		{"leading slash after ||", "x || /svc", "x || /svc"},
		{"environment variable", "grab $svc", "grab $svc"},
		{"braced environment variable", "grab ${svc}", "grab ${svc}"},
		{"environment variable segment", "grab m.$svc", "grab m.$svc"},
		{"selector value in a dotted path", "grab jobs.name=svc.ip", "grab jobs.name=svc.ip"},
		{"selector value in a bracket", "grab jobs[name=svc].ip", "grab jobs[name=svc].ip"},
		{"root path named like the variable", "grab $.svc", "grab $.svc"},
		{"root path with a tail", "grab $.svc.name", "grab $.svc.name"},
		{"root path beside the variable", `concat $.svc.name "-" svc.name`, `concat $.svc.name "-" ` + target + ".name"},
		{"root path whose later segment is the name", "grab $.a.svc", "grab $.a.svc"},
		{"exponent slash path beside the variable", `concat 1e+5/2 "-" svc`, `concat 1e+5/2 "-" ` + target},
		{"decimal exponent slash path beside the variable", `concat 1.5e3/2 "-" svc`, `concat 1.5e3/2 "-" ` + target},
		{"plus name after the variable stays an addition", "svc+x", target + " +x"},
		{"equals after the variable stays outside the path", "svc==x", target + " ==x"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := rewriteVarRefs(tc.in, targets, nil); got != tc.want {
				t.Fatalf("rewriteVarRefs(%q)\n got  %q\n want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestRewriteVarRefs_DollarPlusSegment pins that a loop variable named
// like any part of a "/$name+name" segment stays as written, because the
// lexer reads "a/$X+y" as the one key "a/$X+y", so even "a" is not a
// name of its own there.
func TestRewriteVarRefs_DollarPlusSegment(t *testing.T) {
	targets := map[string]string{"y": "__graft_loop.l1.0.y", "X": "__graft_loop.l1.0.X", "a": "__graft_loop.l1.0.a"}
	for _, in := range []string{"grab a/$X+y", "grab m.x/$X+y.k", "grab /$X+y"} {
		if got := rewriteVarRefs(in, targets, nil); got != in {
			t.Fatalf("rewriteVarRefs(%q)\n got  %q\n want it unchanged", in, got)
		}
	}
}

// TestRewriteVarRefs_LiteralNames pins that a loop variable named like a
// boolean or null literal is not rewritten, because the lexer reads
// "true" and "null" as literals, never as references.
func TestRewriteVarRefs_LiteralNames(t *testing.T) {
	targets := map[string]string{"true": "__graft_loop.l1.0.true", "null": "__graft_loop.l1.0.null"}
	for _, in := range []string{"grab true", "concat null true"} {
		if got := rewriteVarRefs(in, targets, nil); got != in {
			t.Fatalf("rewriteVarRefs(%q)\n got  %q\n want it unchanged", in, got)
		}
	}
}

// TestRewriteVarRefs_ExponentSlashPaths pins that a loop variable named
// "e" is not found inside a number in exponent form followed by a '/',
// because the lexer reads "1e+5/2" and "1.5e3/2" as paths whose first
// segments start with a digit, so the "e" is not a name of its own.
func TestRewriteVarRefs_ExponentSlashPaths(t *testing.T) {
	const target = "__graft_loop.l1.0.e"
	targets := map[string]string{"e": target}
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"exponent with a sign", "grab 1e+5/2", "grab 1e+5/2"},
		{"exponent after a decimal point", "grab 1.5e3/2", "grab 1.5e3/2"},
		{"exponent path beside the variable", `concat 1e+5/2 "-" e.k`, `concat 1e+5/2 "-" ` + target + ".k"},
		{"decimal exponent path beside the variable", `concat 1.5e3/2 "-" e.k`, `concat 1.5e3/2 "-" ` + target + ".k"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := rewriteVarRefs(tc.in, targets, nil); got != tc.want {
				t.Fatalf("rewriteVarRefs(%q)\n got  %q\n want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestRewriteVarRefs_OperatorNames pins which names the rewrite leaves
// alone because they name an operator.
func TestRewriteVarRefs_OperatorNames(t *testing.T) {
	const join = "__graft_loop.l1.0.join"
	targets := map[string]string{"join": join}
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"operator name and argument", ` join ", " join `, ` join ", " ` + join + ` `},
		{"operator name in a group", `concat (join "," join) "x"`, `concat (join "," ` + join + `) "x"`},
		{"operator name as a lone operand", ` join `, ` ` + join + ` `},
		{"operator name before ||", ` join || "z" `, ` ` + join + ` || "z" `},
		{"operator name as a reference", `grab join.x`, `grab ` + join + `.x`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := rewriteVarRefs(tc.in, targets, nil); got != tc.want {
				t.Fatalf("rewriteVarRefs(%q)\n got  %q\n want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestRewriteVarRefs_CalcStrings pins which names the rewrite finds
// inside the quoted expression of a calc call, which calc reads in its
// own expression language.
func TestRewriteVarRefs_CalcStrings(t *testing.T) {
	const i, svc = "__graft_loop.l1.0.i", "__graft_loop.l1.0.svc"
	targets := map[string]string{"i": i, "svc": svc}
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"bare name", `calc "i + 100"`, `calc "` + i + ` + 100"`},
		{"single quotes", `calc 'i * 2'`, `calc '` + i + ` * 2'`},
		{"dotted path", `calc "svc.n - 1"`, `calc "` + svc + `.n - 1"`},
		{"subtraction with no spaces", `calc "i-1"`, `calc "` + i + ` -1"`},
		{"dotted path before a dash", `calc "svc.n-1"`, `calc "` + svc + `.n-1"`},
		{"function argument", `calc "max(i, 2)"`, `calc "max(` + i + `, 2)"`},
		{"name before a paren is a function", `calc "i (2)"`, `calc "i (2)"`},
		{"inner string", `calc "'i' == 'x' ? i : 0"`, `calc "'i' == 'x' ? ` + i + ` : 0"`},
		{"escaped inner quotes", `calc "\"i\" == \"x\" ? i : 0"`, `calc "\"i\" == \"x\" ? ` + i + ` : 0"`},
		{"escaped name", `calc "[i] + i"`, `calc "[i] + ` + i + `"`},
		{"keyword, number, and longer name", `calc "true && 1i > 0 && i_x > i"`, `calc "true && 1i > 0 && i_x > ` + i + `"`},
		{"after a dollar", `calc "$i + i"`, `calc "$i + ` + i + `"`},
		{"calc in a group", `concat "v" (calc "i * 10")`, `concat "v" (calc "` + i + ` * 10")`},
		{"string that is not a calc expression", `concat "i" i`, `concat "i" ` + i},
		{"string after a reference named calc", `concat calc "i"`, `concat calc "i"`},
		{"rewritten names of an inner loop", `calc "__graft_loop.l2.0.j + i"`, `calc "__graft_loop.l2.0.j + ` + i + `"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := rewriteVarRefs(tc.in, targets, nil); got != tc.want {
				t.Fatalf("rewriteVarRefs(%q)\n got  %q\n want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestRewriteVarRefs_Defer pins that the rewrite leaves a defer
// expression as written, and still rewrites a path named defer.
func TestRewriteVarRefs_Defer(t *testing.T) {
	const svc, deferVar = "__graft_loop.l1.0.svc", "__graft_loop.l1.0.defer"
	targets := map[string]string{"svc": svc, "defer": deferVar}
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"defer of an operator", ` defer grab svc.ip `, ` defer grab svc.ip `},
		{"defer of a bare path", `defer svc.name`, `defer svc.name`},
		{"grab of a path named defer", `grab defer.svc`, `grab ` + deferVar + `.svc`},
		{"concat with a variable named defer", `concat defer svc`, `concat ` + deferVar + ` ` + svc},
		{"defer inside concat", `concat svc.name (defer grab svc.ip)`, `concat ` + svc + `.name (defer grab svc.ip)`},
		{"defer after a fallback", `grab svc.x || defer grab svc.ip`, `grab ` + svc + `.x || defer grab svc.ip`},
		{"two defers in one call", `concat (defer grab svc.a) svc.b (defer grab svc.c)`, `concat (defer grab svc.a) ` + svc + `.b (defer grab svc.c)`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := rewriteVarRefs(tc.in, targets, nil); got != tc.want {
				t.Fatalf("rewriteVarRefs(%q)\n got  %q\n want %q", tc.in, got, tc.want)
			}
		})
	}
}
