package load

import (
	"errors"
	"io"
	"strings"
	"testing"
)

func TestJSONConcatenatedValuesWithLines(t *testing.T) {
	r := Open(Source{"f", strings.NewReader("{\"x\":1}{\"x\":2}\n 3\n\"s\"")}, 0, LoadOptions{Format: FormatJSON})
	d, _ := r.Next()
	wantEqual(t, r.Line(), 1)
	wantEqual(t, mustGet(d.Value, "x").Float(), 1.0)
	_, _ = r.Next()
	d, _ = r.Next()
	wantEqual(t, r.Line(), 2)
	wantEqual(t, d.Value.Float(), 3.0)
	wantEqual(t, d.Line, 2)
	d, _ = r.Next()
	wantEqual(t, r.Line(), 3)
	wantEqual(t, d.Value.Str(), "s")
}

func TestJSONLineCountsConsumedLines(t *testing.T) {
	r := Open(Source{"f", strings.NewReader("{\n\"a\":1\n}\n")}, 0, LoadOptions{Format: FormatJSON})
	_, _ = r.Next()
	wantEqual(t, r.Line(), 3) // input_line_number prints 3 here in jq 1.8.2
}

func TestReaderIsIncremental(t *testing.T) {
	r := Open(Source{"f", strings.NewReader("1\n{")}, 0, LoadOptions{})
	d, err := r.Next()
	if err != nil {
		t.Fatal(err)
	}
	wantEqual(t, d.Value.Float(), 1.0)
	_, err = r.Next()
	wantErrContains(t, err, "line 2") // the broken value is reported only when asked for
}

func TestRawLinesKeepCRAndLastLine(t *testing.T) {
	docs := mustReadAll(t, "x\r\ny", LoadOptions{RawLines: true})
	wantEqual(t, len(docs), 2)
	wantEqual(t, docs[0].Value.Str(), "x\r")
	wantEqual(t, docs[1].Value.Str(), "y")
	docs = mustReadAll(t, "a\xffb\n", LoadOptions{RawLines: true})
	wantEqual(t, docs[0].Value.Str(), "a�b")
	docs = mustReadAll(t, "a\n\nb\n", LoadOptions{RawLines: true})
	wantEqual(t, len(docs), 3)
	wantEqual(t, docs[1].Value.Str(), "")
}

func TestChainCrossesSources(t *testing.T) {
	r := Chain(Open(Source{"a", strings.NewReader("1")}, 0, LoadOptions{}), Open(Source{"b", strings.NewReader("2 3")}, 1, LoadOptions{}))
	var got []float64
	var files []int
	for {
		d, err := r.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, d.Value.Float())
		files = append(files, d.FileIndex)
	}
	wantEqual(t, got, []float64{1, 2, 3})
	wantEqual(t, files, []int{0, 1, 1})
	if _, err := r.Next(); !errors.Is(err, io.EOF) {
		t.Fatalf("want io.EOF after the end, got %v", err)
	}
}

func TestEmptyInputYieldsNoDocuments(t *testing.T) {
	for _, opts := range []LoadOptions{{}, {Format: FormatJSON}, {Format: FormatYAML}, {RawLines: true}} {
		_, err := Open(Source{"f", strings.NewReader("")}, 0, opts).Next()
		if !errors.Is(err, io.EOF) {
			t.Fatalf("%+v: want io.EOF, got %v", opts, err)
		}
	}
	if _, err := Chain().Next(); !errors.Is(err, io.EOF) {
		t.Fatalf("empty chain: want io.EOF, got %v", err)
	}
}

func TestDepthLimitReportedAsError(t *testing.T) {
	_, err := Open(Source{"f", strings.NewReader(strings.Repeat("[", 10001))}, 0, LoadOptions{Format: FormatJSON}).Next()
	wantErrContains(t, err, "exceeds maximum depth")
	_, err = Open(Source{"f", strings.NewReader(strings.Repeat("[", 10001))}, 0, LoadOptions{Format: FormatYAML}).Next()
	wantErrContains(t, err, "depth") // yamlnode's own pre-parse guard
}

func TestYAMLDocumentsCarryPositionsAndOptions(t *testing.T) {
	docs := mustReadAll(t, "# lead\na: 1\n---\nb: 2\n", LoadOptions{StringKeys: true})
	wantEqual(t, len(docs), 2)
	wantEqual(t, docs[1].Line, 4)
	wantEqual(t, docs[0].Source, "f")
	r := Open(Source{"f", strings.NewReader("a: 1\n---\nb: [\n")}, 0, LoadOptions{})
	_, err := r.Next()
	wantErrContains(t, err, "f: ")
}

func TestStreamIsNotSupportedYet(t *testing.T) {
	_, err := Open(Source{"f", strings.NewReader("1")}, 0, LoadOptions{Stream: true}).Next()
	wantErrContains(t, err, "--stream")
}

func TestBOMIsDropped(t *testing.T) {
	docs := mustReadAll(t, "\xEF\xBB\xBF{\"a\":1}", LoadOptions{})
	wantEqual(t, mustGet(docs[0].Value, "a").Float(), 1.0)
	docs = mustReadAll(t, "\xEF\xBB\xBFa: 1\n", LoadOptions{})
	wantEqual(t, mustGet(docs[0].Value, "a").Float(), 1.0)
}

func TestLeadingContentStaysEmptyInJ1(t *testing.T) {
	docs := mustReadAll(t, "# c\na: 1\n", LoadOptions{KeepMeta: true})
	wantEqual(t, docs[0].LeadingContent, "")
}

func TestYAMLDepthLimitOption(t *testing.T) {
	r := Open(Source{"f", strings.NewReader("a: [[1]]\n")}, 0, LoadOptions{Format: FormatYAML, DepthLimit: 2})
	_, err := r.Next()
	wantErrContains(t, err, "exceeds maximum depth of 2")
	docs := mustReadAll(t, "a: [[1]]\n", LoadOptions{Format: FormatYAML, DepthLimit: 3})
	wantEqual(t, len(docs), 1)
}

func TestConversionErrorsNameTheirLineOnce(t *testing.T) {
	_, err := Open(Source{"f", strings.NewReader("x: 1\nm:\n  <<: 5\n")}, 0, LoadOptions{StringKeys: true}).Next()
	wantEqual(t, err.Error(), "f: line 3: merge key value must be a mapping or a sequence of mappings")
	_, err = Open(Source{"f", strings.NewReader("a: 1\nb: 2\nb: 3\n")}, 0, LoadOptions{StringKeys: true}).Next()
	wantErrContains(t, err, `line 3: duplicate key "b"`)
	if strings.Count(err.Error(), "line ") != 1 {
		t.Errorf("line prefix repeated in %q", err)
	}
}

func TestEmptyChainIsAChain(t *testing.T) {
	c := Chain()
	if c.chain == nil {
		t.Fatal("Chain() is not a chain")
	}
	wantEqual(t, c.Line(), 0)
}
