package yq

import "testing"

func TestYQMetadataOperators(t *testing.T) {
	wantYQ(t, `.a line_comment = "c"`, "a: 1\n", "a: 1 # c\n")
	wantYQ(t, `.a style = "double"`, "a: x\n", "a: \"x\"\n")
	wantYQ(t, `.a style = ""`, "a: \"x\"\n", "a: x\n")
	wantYQ(t, `.a tag = "!!str"`, "a: 1\n", "a: \"1\"\n")
	wantYQ(t, `.a tag = "!!str"`, "a: true\n", "a: \"true\"\n")
	wantYQ(t, `.a tag = "!!foo"`, "a: [1]\n", "a: !!foo [1]\n")
	wantYQ(t, `.a anchor = "x" | .b alias = "x"`, "a: 1\nb: 2\n", "a: &x 1\nb: *x\n")
	wantYQ(t, `.a anchor = ""`, "a: 1\n", "a: 1\n")
	wantYQ(t, `.a alias = ""`, "a: &x 1\n", "a: &x 1\n")
	wantYQ(t, `explode(.)`, "a: &x 1\nb: *x\n", "a: 1\nb: 1\n")
}

func TestYQMetadataAssignment(t *testing.T) {
	// The left side creates what it names, and "=" takes the first output
	// of a right side that runs once, or an empty comment when it has none.
	wantYQ(t, `.b line_comment = "c"`, "a: 1\n", "a: 1\nb: null # c\n")
	wantYQ(t, `.[] line_comment = (.a, .b)`, "a: 1\nb: 2\n", "a: 1 # 1\nb: 2 # 1\n")
	wantYQ(t, `.[] line_comment = .nope`, "a: 1 # q\nb: 2\n", "a: 1\nb: 2\n")
	wantYQ(t, `.a line_comment = .a`, "a: [x]\n", "a: [x]\n")
	// "|=" runs the right side on each node, and a node it gives nothing
	// for takes the comment the node before it took.
	wantYQ(t, `(.a, .b) line_comment |= .`, "a: 1\nb: 2\n", "a: 1 # 1\nb: 2 # 2\n")
	wantYQ(t, `.[] line_comment |= select(. != 2)`, "a: 1\nb: 2\nc: 3\n", "a: 1 # 1\nb: 2 # 1\nc: 3 # 3\n")
	wantYQ(t, `.a style = "flow" | .a.b style = "double"`, "a: {b: 1}\n", "a: {b: \"1\"}\n")
	// A head comment on the document replaces its leading content.
	lead := "# lead\n\n# head\na: 1 # line\n"
	wantYQ(t, `. head_comment = "x"`, lead, "# x\na: 1 # line\n")
	wantYQ(t, `. foot_comment = "x"`, lead, lead+"# x\n")
	wantYQ(t, `. comments = "z"`, lead, "# z\na: 1 # line\n# z\n")
	wantYQ(t, `. = {"x": 1}`, lead, "# lead\n\n# head\nx: 1\n")
	wantYQError(t, `.a style = "bogus"`, "a: 1\n", "unknown style bogus")
	wantYQError(t, `line_comment = "c"`, "a: 1\n", "'line_comment' expects 2 args but there is 1")
	wantYQError(t, `(line_comment = "c")`, "a: 1\n", "'line_comment' expects 2 args but there is 1")
	wantYQError(t, `.a | line_comment = "c"`, "a: 1\n", "'|' expects 2 args but there is 1")
}

func TestYQMetadataReaders(t *testing.T) {
	wantYQ(t, `.a | line_comment`, "a: 1 # x\n", "x\n")
	wantYQ(t, `.a | line_comment | path`, "a: 1 # x\n", "- a\n")
	wantYQ(t, `head_comment`, "# lead\n\n# head\na: 1 # line\n", "lead\n\nhead\n")
	wantYQ(t, `[.[] | style]`, "a: x\nb: \"y\"\nc: |\n  z\nd: [1]\ne: !!str q\n", "- \"\"\n- double\n- literal\n- flow\n- tagged\n")
	wantYQ(t, `[.[] | tag]`, "a: 1\nb: [1]\nc: {}\n", "- '!!int'\n- '!!seq'\n- '!!map'\n")
	wantYQ(t, `[.[] | kind]`, "a: 1\nb: [1]\nc: {}\nd: &x 2\ne: *x\n", "- scalar\n- seq\n- map\n- scalar\n- alias\n")
	wantYQ(t, `[.[] | anchor]`, "a: &x 1\nb: *x\n", "- x\n- \"\"\n")
	wantYQ(t, `[.[] | alias]`, "a: &x 1\nb: *x\n", "- \"1\"\n- x\n")
	wantYQ(t, `.a | tag | path`, "a: 1\n", "- a\n")
	wantYQ(t, `.a | kind | key`, "a: 1\n", "a\n")
	// Only the root carries the leading content, and a value the run built
	// belongs to the first document, so it never starts one of its own.
	wantYQ(t, `.a`, "# lead\n\na: 1\n", "1\n")
	docs := "# one\na: 1\n---\n# two\na: 2\n---\na: 3\n"
	wantYQ(t, `tag`, docs, "!!map\n!!map\n!!map\n")
	wantYQ(t, `head_comment`, docs, "one\n\n\n")
	wantYQ(t, `.a, "x"`, docs, "1\nx\n---\n2\n---\nx\n---\n3\n---\nx\n")
}

func TestYQExplode(t *testing.T) {
	merge := "a: &x {p: 1}\nb:\n  <<: *x\n  q: 2\nc:\n  <<: [*x, {r: 3}]\n  p: 9\nd: {<<: {s: 1}}\n"
	wantYQ(t, `explode(.)`, merge, "a: {p: 1}\nb:\n  p: 1\n  q: 2\nc:\n  p: 9\nd: {}\n")
	wantYQ(t, `explode(.b)`, merge, "a: &x {p: 1}\nb:\n  p: 1\n  q: 2\nc:\n  <<: [*x, {r: 3}]\n  p: 9\nd: {<<: {s: 1}}\n")
	wantYQ(t, `.b | explode(.)`, merge, "p: 1\nq: 2\n")
	wantYQ(t, `explode(.d)`, merge, "a: &x {p: 1}\nb:\n  <<: *x\n  q: 2\nc:\n  <<: [*x, {r: 3}]\n  p: 9\nd: {}\n")
}

func TestYQExplodeMergeToSpec(t *testing.T) {
	spec := Options{MergeAnchorToSpec: true}
	cases := []struct{ in, want string }{
		{"a: &x {p: 1}\nb:\n  <<: *x\n  q: 2\nc:\n  <<: [*x, {r: 3}]\n  p: 9\nd: {<<: {s: 1}}\n", "a: {p: 1}\nb:\n  p: 1\n  q: 2\nc:\n  r: 3\n  p: 9\nd: {s: 1}\n"},
		{"a: &x {p: 1, q: 1}\nb: &y {q: 2, r: 2}\nc:\n  r: 0\n  <<: [*x, *y]\n", "a: {p: 1, q: 1}\nb: {q: 2, r: 2}\nc:\n  r: 0\n  p: 1\n  q: 1\n"},
		{"a: &x {p: 1}\nb:\n  \"<<\": *x\n", "a: {p: 1}\nb:\n  \"<<\": {p: 1}\n"},
	}
	for _, c := range cases {
		got, err := runYQ(t, spec, `explode(.)`, c.in, false)
		if err != nil {
			t.Fatalf("explode(.) on %q: %v", c.in, err)
		}
		wantEqual(t, got, c.want)
	}
	_, err := runYQ(t, spec, `explode(.)`, "a: &x 1\nb:\n  <<: *x\n", false)
	wantErrContains(t, err, "can only use merge anchors with maps (!!map) or sequences (!!seq) of maps, but got sequence containing !!int")
}
