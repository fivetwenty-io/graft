package yamlcodec

import (
	"testing"
)

func TestLayoutDocumentStart(t *testing.T) {
	less := func(a, b string) bool { return a < b }
	values := []interface{}{
		map[string]interface{}{"a": 1, "b": []interface{}{"x"}},
		"two\nlines\n",
		nil,
		[]interface{}{},
	}
	for _, v := range values {
		plain, err := MarshalYAMLTypedLayout(v, less, Layout{Indent: 4, IndentSequence: true})
		if err != nil {
			t.Fatal(err)
		}
		started, err := MarshalYAMLTypedLayout(v, less, Layout{Indent: 4, IndentSequence: true, DocumentStart: true})
		if err != nil {
			t.Fatal(err)
		}
		if string(started) != "---\n"+string(plain) {
			t.Errorf("%#v:\n got %q\nwant %q", v, started, "---\n"+string(plain))
		}
	}
}
