package yamlnode

import "testing"

func TestSplitStreamMarksEmptyDocuments(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in      string
		content int  // documents that are not Empty
		failed  bool // some document carries Err
	}{
		{"a: 1\n---\nb: 2\n", 2, false},
		{"", 0, false},
		{"---\n", 0, false},
		{"--- \n---\n", 0, false},
		{"# c\n", 0, false},
		{"a: 1\n---\n", 1, false},
		{"a: 1\n---\n...\n---\nb: 2\n", 2, false},
		{"a: 1\n...\nb: 2\n", 1, true},
	}
	for _, c := range cases {
		docs := SplitStream(c.in)
		content, failed := 0, false
		for _, d := range docs {
			if !d.Empty && d.Err == nil {
				content++
			}
			failed = failed || d.Err != nil
		}
		if content != c.content || failed != c.failed {
			t.Errorf("%q: %d documents with content and failed %v, want %d and %v", c.in, content, failed, c.content, c.failed)
		}
	}
}

func TestSplitStreamCountsLinesFromTheStreamStart(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		want []StreamDocument
	}{
		{"a: 1\n---\nb: 2\n", []StreamDocument{
			{Text: "a: 1\n"},
			{Text: "---\nb: 2\n", LineOffset: 1},
		}},
		{"a: 1\r\n---\r\nb: 2\r\n", []StreamDocument{
			{Text: "a: 1\n"},
			{Text: "---\nb: 2\n", LineOffset: 1},
		}},
		{"a: 1\r---\rb: 2\r", []StreamDocument{
			{Text: "a: 1\n"},
			{Text: "---\nb: 2\n", LineOffset: 1},
		}},
		{"\uFEFF# c\n", nil},
		{"a: 1\n# c\n\n---\nb: 2\n", []StreamDocument{
			{Text: "a: 1\n# c\n\n"},
			{Text: "---\nb: 2\n", LineOffset: 3},
		}},
		{"a: 1\n...\n# c\n---\nb: 2\n", []StreamDocument{
			{Text: "a: 1\n...\n"},
			{Text: "# c\n---\nb: 2\n", LineOffset: 2},
		}},
	}
	for _, c := range cases {
		got := SplitStream(c.in)
		if len(got) != len(c.want) {
			t.Errorf("%q: got %+v, want %+v", c.in, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("%q: document %d is %+v, want %+v", c.in, i, got[i], c.want[i])
			}
		}
	}
}
