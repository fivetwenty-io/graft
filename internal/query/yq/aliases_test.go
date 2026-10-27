package yq

import (
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/internal/query/lexer"
)

// callableSpellings expands the name rules of the lexer into the plain
// identifiers they accept: each alternative, with and without the
// optional underscore. A rule that is a number, a format, or a pattern
// with a bracket or a backslash in it is not a plain name, so it drops
// out here.
func callableSpellings(t *testing.T) []string {
	t.Helper()
	seen := map[string]bool{}
	for _, rule := range namePatterns {
		for _, alt := range strings.Split(rule, "|") {
			spellings := []string{""}
			for _, part := range strings.SplitAfter(alt, "_?") {
				var next []string
				for _, s := range spellings {
					if before, ok := strings.CutSuffix(part, "_?"); ok {
						next = append(next, s+before, s+before+"_")
					} else {
						next = append(next, s+part)
					}
				}
				spellings = next
			}
			for _, s := range spellings {
				if isPlainName(s) {
					seen[s] = true
				}
			}
		}
	}
	var out []string
	for s := range seen {
		toks, err := lexer.Tokenize(s, Grammar().Tokens)
		if err != nil || len(toks) == 0 || toks[0].Kind != lexer.Ident {
			continue
		}
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// isPlainName reports whether s is made of letters and underscores.
func isPlainName(s string) bool {
	for _, r := range s {
		if r != '_' && (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') {
			return false
		}
	}
	return s != ""
}

// definedAt reports whether name resolves with the given number of
// arguments, by running a call to it and looking for the evaluator's
// complaint that no function has that name.
func definedAt(t *testing.T, name string, arity int) bool {
	t.Helper()
	args := make([]string, arity)
	for i := range args {
		args[i] = "."
	}
	call := name
	if arity > 0 {
		call += "(" + strings.Join(args, "; ") + ")"
	}
	_, err := runYQ(t, testOpts, call, "a: 1\n", false)
	return err == nil || !strings.Contains(err.Error(), name+"/"+strconv.Itoa(arity)+" is not defined")
}

// undefinedByDesign are spellings the lexer accepts, as yq's does, that
// graft yq does not implement. They are the conversions for the formats
// graft yq leaves out and the system operator.
var undefinedByDesign = map[string]bool{
	"from_props": true, "fromprops": true, "to_props": true, "toprops": true,
	"from_xml": true, "fromxml": true, "to_xml": true, "toxml": true,
	"from_csv": true, "fromcsv": true, "to_csv": true, "tocsv": true,
	"from_tsv": true, "fromtsv": true, "to_tsv": true, "totsv": true,
	"load_props": true, "loadprops": true,
	"load_xml": true, "loadxml": true, "xml_load": true, "xmlload": true,
	"system": true,
}

func TestEveryLexedSpellingResolves(t *testing.T) {
	names := callableSpellings(t)
	if len(names) < 100 {
		t.Fatalf("expanded %d spellings from the name rules, want many more", len(names))
	}
	for _, name := range names {
		defined := false
		for arity := 0; arity <= 3 && !defined; arity++ {
			defined = definedAt(t, name, arity)
		}
		switch {
		case undefinedByDesign[name] && defined:
			t.Errorf("%s resolves, so it should leave undefinedByDesign", name)
		case !undefinedByDesign[name] && !defined:
			t.Errorf("%s is lexed but no function answers to it", name)
		}
	}
}

func TestAliasesBehaveAsTheirBuiltin(t *testing.T) {
	cases := []struct{ alias, builtin, in string }{
		{"fromunix", "from_unix", "1700000000"},
		{"tounix", "to_unix", "2023-11-14T22:13:20Z"},
		{"arraytomap", "array_to_map", "[x, y]"},
		{"toyaml", "to_yaml", "{b: 1}"},
		{"fromyaml", "from_yaml", "'b: 1'"},
		{"splitDoc", "split_doc", "[a, b]"},
		{"splitdoc", "split_doc", "[a, b]"},
		{"iskey", "is_key", "1"},
		{"fileindex", "file_index", "1"},
		{"documentindex", "document_index", "1"},
		{"toentries", "to_entries", "{a: 1}"},
		{"toEntries", "to_entries", "{a: 1}"},
		{"fromentries", "from_entries", "[{key: a, value: 1}]"},
		{"fromEntries", "from_entries", "[{key: a, value: 1}]"},
	}
	for _, c := range cases {
		in := "v: " + c.in + "\n"
		want, werr := runYQ(t, testOpts, ".v | "+c.builtin, in, false)
		got, gerr := runYQ(t, testOpts, ".v | "+c.alias, in, false)
		if werr != nil || gerr != nil {
			t.Errorf("%s: alias error %v, builtin error %v", c.alias, gerr, werr)
		} else if got != want {
			t.Errorf("%s gave %q, %s gave %q", c.alias, got, c.builtin, want)
		}
	}
	calls := []struct{ alias, builtin, in string }{
		{"mapvalues(. + 1)", "map_values(. + 1)", "{a: 1}"},
		{"sortKeys(.)", "sort_keys(.)", "{b: 1, a: 2}"},
		{"sortkeys(.)", "sort_keys(.)", "{b: 1, a: 2}"},
		{"uniqueby(.a)", "unique_by(.a)", "[{a: 1}, {a: 1}]"},
		{"groupby(.a)", "group_by(.a)", "[{a: 1}, {a: 1}, {a: 2}]"},
		{"sortby(.a)", "sort_by(.a)", "[{a: 2}, {a: 1}]"},
		{"withentries(.)", "with_entries(.)", "{a: 1}"},
		{"withEntries(.)", "with_entries(.)", "{a: 1}"},
		{"set_path([\"a\"]; 5)", "setpath([\"a\"]; 5)", "{a: 1}"},
		{"del_paths([[\"a\"]])", "delpaths([[\"a\"]])", "{a: 1, b: 2}"},
		{"toyaml(4)", "to_yaml(4)", "{b: 1}"},
		{"loadstr(\"in.yml\")", "load_str(\"in.yml\")", "x"},
		{"str_load(\"in.yml\")", "load_str(\"in.yml\")", "x"},
	}
	for _, c := range calls {
		in := "v: " + c.in + "\n"
		want, werr := runYQ(t, testOpts, ".v | "+c.builtin, in, false)
		got, gerr := runYQ(t, testOpts, ".v | "+c.alias, in, false)
		if (gerr == nil) != (werr == nil) || (gerr != nil && gerr.Error() != werr.Error()) {
			t.Errorf("%s: alias error %v, builtin error %v", c.alias, gerr, werr)
		} else if got != want {
			t.Errorf("%s gave %q, %s gave %q", c.alias, got, c.builtin, want)
		}
	}
}
