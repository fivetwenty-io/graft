package yaml_test

import (
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/pkg/yaml"
)

const setSrc = "# config\nname: web   # the app\nport: 8080\nhost: \"db.local\"\nmode: 'fast'\nlist:\n  - a\n  - b # second\nflow: {k: v, n: 1}\nratio: 0.5\n"

func TestDocumentSetKeepsEveryOtherByte(t *testing.T) {
	cases := []struct {
		path string
		v    any
		want string
	}{
		{"name", "api", strings.Replace(setSrc, "name: web ", "name: api ", 1)},
		{"port", 9090, strings.Replace(setSrc, "8080", "9090", 1)},
		{"port", uint16(1), strings.Replace(setSrc, "8080", "1", 1)},
		{"host", "db.prod", strings.Replace(setSrc, `"db.local"`, `"db.prod"`, 1)},
		{"mode", "it's", strings.Replace(setSrc, `'fast'`, `'it''s'`, 1)},
		{"list.1", "c", strings.Replace(setSrc, "- b #", "- c #", 1)},
		{"flow.n", 2, strings.Replace(setSrc, "n: 1}", "n: 2}", 1)},
		{"flow.k", "w", strings.Replace(setSrc, "{k: v,", "{k: w,", 1)},
		{"name", true, strings.Replace(setSrc, "name: web ", "name: true ", 1)},
		{"port", nil, strings.Replace(setSrc, "8080", "null", 1)},
		{"ratio", 1.25, strings.Replace(setSrc, "0.5", "1.25", 1)},
		{"name", "two\nlines", strings.Replace(setSrc, "name: web ", `name: "two\nlines" `, 1)},
	}
	for _, c := range cases {
		d := mustDoc(t, setSrc)
		if err := d.Set(c.path, c.v); err != nil {
			t.Errorf("Set(%q, %#v): %v", c.path, c.v, err)
			continue
		}
		if got := string(d.Bytes()); got != c.want {
			t.Errorf("Set(%q, %#v):\n got %q\nwant %q", c.path, c.v, got, c.want)
		}
	}
}

func TestDocumentSetQuotesWhatPlainWouldMisread(t *testing.T) {
	for _, s := range []string{"yes", "1.0", "~", "a: b", "#x", ""} {
		d := mustDoc(t, setSrc)
		if err := d.Set("name", s); err != nil {
			t.Fatalf("Set(name, %q): %v", s, err)
		}
		n, err := d.Get("name")
		if err != nil || n.Tag != "!!str" || n.Value != s {
			t.Errorf("Set(name, %q) reads back %+v, %v", s, n, err)
		}
		var got string
		if err := n.Decode(&got); err != nil || got != s {
			t.Errorf("Decode after Set(name, %q) = %q, %v", s, got, err)
		}
	}
}

func TestDocumentSetRefusals(t *testing.T) {
	cases := []struct {
		src, path string
		v         any
		want      error
	}{
		{setSrc, "name", []string{"a"}, yaml.ErrUnsupportedEdit},
		{setSrc, "name", map[string]any{}, yaml.ErrUnsupportedEdit},
		{setSrc, "missing", "x", yaml.ErrPathNotFound},
		{setSrc, "flow", "x", yaml.ErrUnsupportedEdit},
		{"a: &x 1\nb: *x\n", "a", 2, yaml.ErrUnsupportedEdit},
		{"a: 1\r\n", "a", 2, yaml.ErrUnsupportedEdit},
		{"a: |\n  x\n", "a", "y\n", yaml.ErrUnsupportedEdit},
	}
	for _, c := range cases {
		d := mustDoc(t, c.src)
		if err := d.Set(c.path, c.v); !errors.Is(err, c.want) {
			t.Errorf("%q Set(%q, %#v) = %v, want %v", c.src, c.path, c.v, err, c.want)
		}
		if string(d.Bytes()) != c.src {
			t.Errorf("a refused Set changed the bytes to %q", d.Bytes())
		}
	}
	var de *yaml.DocumentError
	err := mustDoc(t, setSrc).Set("name", struct{}{})
	if !errors.As(err, &de) || de.Reason != "Set does not write a value of Go type struct {}" {
		t.Errorf("got %v", err)
	}
}

func TestDocumentSetTwiceKeepsLines(t *testing.T) {
	d := mustDoc(t, setSrc)
	if err := d.Set("port", 1); err != nil {
		t.Fatal(err)
	}
	if err := d.Set("list.0", "z"); err != nil {
		t.Fatal(err)
	}
	n, err := d.Get("list.0")
	if err != nil || n.Value != "z" || n.Line != 7 {
		t.Errorf("Get(list.0) = %+v, %v", n, err)
	}
}

// TestDocumentSetEveryNumberType checks that each Go integer and float
// type, and the floats with no decimal form, read back as the value Set
// was given.
func TestDocumentSetEveryNumberType(t *testing.T) {
	for _, v := range []any{
		int(-1), int8(-128), int16(300), int32(-70000), int64(math.MinInt64),
		uint(7), uint8(255), uint16(65535), uint32(math.MaxUint32), uint64(math.MaxUint64),
		float32(0.1), 1.0, 1e21, math.Inf(1), math.Inf(-1),
	} {
		d := mustDoc(t, setSrc)
		if err := d.Set("port", v); err != nil {
			t.Errorf("Set(port, %T %v): %v", v, v, err)
			continue
		}
		n, err := d.Get("port")
		if err != nil {
			t.Fatal(err)
		}
		got := reflect.New(reflect.TypeOf(v))
		if err := n.Decode(got.Interface()); err != nil || !reflect.DeepEqual(got.Elem().Interface(), v) {
			t.Errorf("Set(port, %T %v) decodes to %v, %v", v, v, got.Elem().Interface(), err)
		}
		if !strings.HasSuffix(string(d.Bytes()), "\nhost: \"db.local\"\nmode: 'fast'\nlist:\n  - a\n  - b # second\nflow: {k: v, n: 1}\nratio: 0.5\n") {
			t.Errorf("Set(port, %T %v) changed another line: %q", v, v, d.Bytes())
		}
	}
	d := mustDoc(t, setSrc)
	if err := d.Set("ratio", math.NaN()); err != nil {
		t.Fatal(err)
	}
	if got := string(d.Bytes()); got != strings.Replace(setSrc, "0.5", ".nan", 1) {
		t.Errorf("Set(ratio, NaN) = %q", got)
	}
}

// TestDocumentSetQuotesFlowIndicatorsInAFlowCollection checks that a
// string a block value could hold plain is quoted inside a flow
// collection when its text would end the scalar there.
func TestDocumentSetQuotesFlowIndicatorsInAFlowCollection(t *testing.T) {
	for _, s := range []string{"a,b", "x]", "y}", "a, b", "{x", "[x]", "it's"} {
		d := mustDoc(t, setSrc)
		if err := d.Set("flow.k", s); err != nil {
			t.Errorf("Set(flow.k, %q): %v", s, err)
			continue
		}
		n, err := d.Get("flow.k")
		if err != nil || n.Value != s || n.Tag != "!!str" {
			t.Errorf("Set(flow.k, %q) reads back %+v, %v", s, n, err)
		}
		if m, err := d.Get("flow.n"); err != nil || m.Value != "1" {
			t.Errorf("Set(flow.k, %q) moved flow.n: %+v, %v", s, m, err)
		}
	}
}

func TestDocumentSetWritesALongStringOnOneLine(t *testing.T) {
	long := strings.Repeat("word ", 40) + "end"
	d := mustDoc(t, setSrc)
	if err := d.Set("name", long); err != nil {
		t.Fatal(err)
	}
	if got := string(d.Bytes()); got != strings.Replace(setSrc, "name: web ", "name: "+long+" ", 1) {
		t.Errorf("Bytes = %q", got)
	}
}

func TestDocumentSetTheRootScalar(t *testing.T) {
	d := mustDoc(t, "# head\nhello # c\n")
	if err := d.Set("", "bye"); err != nil {
		t.Fatal(err)
	}
	if got := string(d.Bytes()); got != "# head\nbye # c\n" {
		t.Errorf("Bytes = %q", got)
	}
}

func TestDocumentSetErrorNamesTheOperation(t *testing.T) {
	var de *yaml.DocumentError
	err := mustDoc(t, setSrc).Set("flow", "x")
	if !errors.As(err, &de) || de.Op != "set" || de.Path != "flow" || de.Line != 9 {
		t.Errorf("Set(flow) = %#v", err)
	}
	if err := mustDoc(t, setSrc).Set("name", uintptr(1)); !errors.As(err, &de) || de.Reason != "Set does not write a value of Go type uintptr" {
		t.Errorf("Set(name, uintptr) = %v", err)
	}
}

// TestDocumentSetFloatsReadBackAsFloats checks that a float written in
// exponent form decodes as a float64 into an interface, through both
// Node.Decode and Unmarshal, and not as the string a bare 1e+06 reads
// as.
func TestDocumentSetFloatsReadBackAsFloats(t *testing.T) {
	for _, f := range []float64{1e6, 3e10, 1e21, 1e100, 1e-7, -2.5e-9, 1.25, 1.0} {
		d := mustDoc(t, setSrc)
		if err := d.Set("ratio", f); err != nil {
			t.Errorf("Set(ratio, %v): %v", f, err)
			continue
		}
		n, err := d.Get("ratio")
		if err != nil {
			t.Fatal(err)
		}
		var viaNode any
		if err := n.Decode(&viaNode); err != nil || viaNode != f {
			t.Errorf("Set(ratio, %v) writes %s, which Decode reads as %T %v, %v", f, n.Value, viaNode, viaNode, err)
		}
		var doc map[string]any
		if err := yaml.Unmarshal(d.Bytes(), &doc); err != nil || doc["ratio"] != f {
			t.Errorf("Set(ratio, %v) writes %s, which Unmarshal reads as %T %v, %v", f, n.Value, doc["ratio"], doc["ratio"], err)
		}
	}
}

// TestDocumentSetToTheSameValue checks that setting a slot to the value
// it already holds succeeds and leaves the bytes as they were.
func TestDocumentSetToTheSameValue(t *testing.T) {
	for _, c := range []struct {
		src, path string
		v         any
	}{
		{"a: x\n", "a", "x"},
		{"a: 'x'\n", "a", "x"},
		{"a: \"x\"\n", "a", "x"},
		{"a: ''\n", "a", ""},
		{"a: 1\n", "a", 1},
		{"a: 1 # c\nb: 2\n", "a", 1},
		{"x\n", "", "x"},
		{setSrc, "list.1", "b"},
		{setSrc, "flow.n", 1},
	} {
		d := mustDoc(t, c.src)
		if err := d.Set(c.path, c.v); err != nil {
			t.Errorf("%q Set(%q, %#v): %v", c.src, c.path, c.v, err)
		}
		if string(d.Bytes()) != c.src {
			t.Errorf("%q Set(%q, %#v) changed the bytes to %q", c.src, c.path, c.v, d.Bytes())
		}
	}
}

// TestDocumentSetRefusesToMoveAComment pins a refusal the parser
// forces. A comment line right after a plain value is that value's
// key's foot comment, but after a quoted value it is the document's, so
// quoting the value would move the comment, and Set refuses.
func TestDocumentSetRefusesToMoveAComment(t *testing.T) {
	const src = "a: X\n# end\n"
	d := mustDoc(t, src)
	if err := d.Set("a", "y"); !errors.Is(err, yaml.ErrUnsupportedEdit) || string(d.Bytes()) != src {
		t.Errorf("Set(a, y) = %v, Bytes = %q", err, d.Bytes())
	}
	if err := d.Set("a", "z"); err != nil || string(d.Bytes()) != "a: z\n# end\n" {
		t.Errorf("Set(a, z) = %v, Bytes = %q", err, d.Bytes())
	}
}
