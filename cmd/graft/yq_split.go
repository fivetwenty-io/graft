package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/fivetwenty-io/graft/log"
	"github.com/fivetwenty-io/graft/pkg/query/value"
	"github.com/fivetwenty-io/graft/pkg/yq"
)

// errSplitInplace is what yq answers when -i meets a split expression.
const errSplitInplace = "write in place cannot be used with split file"

// extYML is the extension graft gives a YAML file it names itself.
const extYML = ".yml"

// yqSplitExtension is the extension a split file gets for an output
// format.
func yqSplitExtension(format yq.Format) string {
	if format == yq.FormatJSON {
		return ".json"
	}
	return extYML
}

// yqSplitExpression is the expression that names the split files, which
// is the contents of --split-exp-file when it was given and -s otherwise.
func yqSplitExpression(opts yqOptions) (string, error) {
	if opts.SplitExpFile == "" {
		return opts.SplitExp, nil
	}
	data, err := os.ReadFile(opts.SplitExpFile) // #nosec G304 -- the user names the expression file
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// splitWriter returns the function that opens the file an output document
// goes to. ev is the compiled file-name expression expr. It runs against
// the document, and the first value it yields, as text, names the file, so
// that an empty result gives ".yml". A name with a directory in it gets
// that directory made. An existing file is truncated.
func splitWriter(expr string, ev *yq.Evaluator, ext string) func(doc yq.Document, v *value.Value) (io.WriteCloser, error) {
	log.DEBUG("split expression: %s", expr)
	return func(doc yq.Document, v *value.Value) (io.WriteCloser, error) {
		name, err := splitName(ev, doc.Value)
		if err != nil {
			return nil, err
		}
		path := name + ext
		if dir := filepath.Dir(path); dir != "." {
			if err := os.MkdirAll(dir, 0o750); err != nil {
				return nil, err
			}
		}
		return os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o666) // #nosec G302,G304 -- the user's expression names the file
	}
}

// splitName is the text of the first value ev yields for doc. A map or a
// list has no text, and a null is "null".
func splitName(ev *yq.Evaluator, doc *value.Value) (string, error) {
	for v, err := range ev.Eval(context.Background(), doc) {
		if err != nil {
			return "", err
		}
		return scalarText(v)
	}
	return "", nil
}

// scalarText is v as an unquoted scalar, and "" for a map or a list.
func scalarText(v *value.Value) (string, error) {
	if v.Kind() == value.KindObject || v.Kind() == value.KindArray {
		return "", nil
	}
	var buf bytes.Buffer
	enc := yq.NewEncoder(&buf, yq.EncodeOptions{Format: yq.FormatYAML, UnwrapScalar: true, NoDocSeparators: true})
	if err := enc.Encode(v.WithMeta(nil)); err != nil {
		return "", err
	}
	if err := enc.Flush(); err != nil {
		return "", err
	}
	return strings.TrimSuffix(buf.String(), "\n"), nil
}

// documentOf is the public shape of the document an output value belongs
// to, read from the position its Meta records.
func documentOf(v *value.Value) yq.Document {
	d := yq.Document{Value: v}
	if m := v.Meta(); m != nil {
		d.Index, d.FileIndex, d.Filename = m.DocIndex, m.FileIndex, m.Filename
	}
	return d
}

// splitSink is the writer the one split encoder writes through, which
// points at the file being written. The encoder is shared by every file so
// that the "---" between documents follows the whole run, as it does in
// yq.
type splitSink struct{ w io.Writer }

func (s *splitSink) Write(p []byte) (int, error) { return s.w.Write(p) }

// writeSplit writes v to the file the split writer opens for it, and
// closes the file.
func (o *yqOutput) writeSplit(v *value.Value) (err error) {
	w, err := o.split(documentOf(v), v)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := w.Close(); err == nil {
			err = cerr
		}
	}()
	o.sink.w = w
	if err := o.enc.Encode(v); err != nil {
		return err
	}
	return o.enc.Flush()
}
