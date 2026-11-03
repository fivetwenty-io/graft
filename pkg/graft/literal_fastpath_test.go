package graft_test

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/fivetwenty-io/graft/pkg/graft"
)

// bigPlainData builds a document of n keys, each holding a list and a
// map, with no literal in it.
func bigPlainData(n int) map[string]interface{} {
	data := make(map[string]interface{}, n)
	for i := 0; i < n; i++ {
		data[fmt.Sprintf("k%d", i)] = map[string]interface{}{
			"list": []interface{}{"a", i},
			"name": "v",
		}
	}
	return data
}

// TestOutputWithoutLiteralDoesNotWalk checks that a document with no
// literal hands out its own map, as it did before literals existed, and
// that once it knows it holds none, the output calls allocate nothing.
func TestOutputWithoutLiteralDoesNotWalk(t *testing.T) {
	data := bigPlainData(2000)
	doc := graft.NewDocument(data)
	own := reflect.ValueOf(data).Pointer()
	calls := map[string]func() interface{}{
		"RawData": doc.RawData,
		"GetData": doc.GetData,
		"ToMap":   func() interface{} { return doc.(interface{ ToMap() map[string]interface{} }).ToMap() },
		"Get root": func() interface{} {
			v, _ := doc.Get("$")
			return v
		},
	}
	for name, call := range calls {
		if got := reflect.ValueOf(call()).Pointer(); got != own {
			t.Errorf("%s did not return the document's own map", name)
		}
		if allocs := testing.AllocsPerRun(20, func() { _ = call() }); allocs != 0 {
			t.Errorf("%s allocates %v times per call on a document with no literal", name, allocs)
		}
	}
	sub, err := doc.Get("k7")
	if err != nil {
		t.Fatal(err)
	}
	if reflect.ValueOf(sub).Pointer() != reflect.ValueOf(data["k7"]).Pointer() {
		t.Error("Get did not return the document's own map")
	}
}

// TestOutputSeesALiteralSetLater checks that a literal put into a
// document with Set after an output call is still stripped from every
// later output.
func TestOutputSeesALiteralSetLater(t *testing.T) {
	doc := graft.NewDocument(bigPlainData(10))
	_ = doc.RawData()
	if err := doc.Set("k3.secret", graft.NewLiteral(map[string]interface{}{"x": "y"})); err != nil {
		t.Fatal(err)
	}
	if err := doc.Set("k4", map[string]interface{}{"deep": graft.NewLiteral("z")}); err != nil {
		t.Fatal(err)
	}
	raw := doc.RawData().(map[string]interface{})
	if got := raw["k3"].(map[string]interface{})["secret"]; !reflect.DeepEqual(got, map[string]interface{}{"x": "y"}) {
		t.Errorf("RawData kept the wrapper: %#v", got)
	}
	if got := raw["k4"].(map[string]interface{})["deep"]; got != "z" {
		t.Errorf("RawData kept the wrapper: %#v", got)
	}

	root := graft.NewDocument(nil)
	_ = root.RawData()
	if err := root.Set("$", map[string]interface{}{"p": graft.NewLiteral("w")}); err != nil {
		t.Fatal(err)
	}
	if got := root.RawData().(map[string]interface{})["p"]; got != "w" {
		t.Errorf("RawData after a root Set kept the wrapper: %#v", got)
	}
}

func BenchmarkRawDataWithoutLiteral(b *testing.B) {
	doc := graft.NewDocument(bigPlainData(2000))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = doc.RawData()
	}
}

func BenchmarkRawDataWithLiteral(b *testing.B) {
	data := bigPlainData(2000)
	data["params"] = graft.NewLiteral(map[string]interface{}{"x": "y"})
	doc := graft.NewDocument(data)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = doc.RawData()
	}
}
