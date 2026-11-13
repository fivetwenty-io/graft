package yamlcodec

import (
	"fmt"
	"testing"

	"github.com/goccy/go-yaml"
)

func TestLayoutDocumentStart(t *testing.T) {
	less := func(a, b string) bool { return a < b }
	values := []interface{}{
		map[string]interface{}{"a": 1, "b": []interface{}{"x"}},
		"two\nlines\n",
		nil,
		[]interface{}{},
		yaml.MapSlice{{Key: "b", Value: 1}, {Key: "a", Value: "x\ny\n"}},
	}
	layouts := []Layout{
		{},
		{Indent: 4, IndentSequence: true},
		{Indent: 3},
	}
	for _, layout := range layouts {
		for _, v := range values {
			name := fmt.Sprintf("%+v/%#v", layout, v)
			plain, err := MarshalYAMLTypedLayout(v, less, layout)
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			withStart := layout
			withStart.DocumentStart = true
			started, err := MarshalYAMLTypedLayout(v, less, withStart)
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			if string(started) != "---\n"+string(plain) {
				t.Errorf("%s:\n got %q\nwant %q", name, started, "---\n"+string(plain))
			}
		}
	}
}
