package yq

import (
	"bytes"
	"encoding/base64"
	"encoding/csv"
	"net/url"
	"strconv"
	"strings"

	"github.com/fivetwenty-io/graft/internal/query/jqout"
	"github.com/fivetwenty-io/graft/internal/query/runtime"
	"github.com/fivetwenty-io/graft/pkg/query"
	"github.com/fivetwenty-io/graft/pkg/query/value"
)

// uriHint ends yq's message for an encoder given a node that is not a
// string.
const uriHint = ", can only operate on strings. Please first pipe through another encoding operator to convert the value to a string"

// registerConvert adds yq's encoders and its conversions to and from JSON
// and YAML text. Text that is read back keeps the presentation it was
// written in, as yq parses it as YAML.
func registerConvert(r *runtime.Registry) {
	for name, enc := range map[string]*base64.Encoding{"@base64": base64.StdEncoding, "@base64url": base64.URLEncoding} {
		encoder(r, name, func(in *value.Value) (*value.Value, error) {
			if !isStr(in) {
				return nil, &query.RuntimeError{Msg: "cannot encode " + tagOf(in) + " as base64, can only operate on strings"}
			}
			return value.Str(enc.EncodeToString([]byte(in.Str()))), nil
		})
		encoder(r, name+"d", func(in *value.Value) (*value.Value, error) {
			b, err := decodeBase64(enc, nodeText(in))
			if err != nil {
				return nil, &query.RuntimeError{Msg: err.Error()}
			}
			return value.Str(string(b)), nil
		})
	}
	encoder(r, "@uri", func(in *value.Value) (*value.Value, error) {
		if !isStr(in) {
			return nil, &query.RuntimeError{Msg: "cannot encode " + tagOf(in) + " as URI" + uriHint}
		}
		return value.Str(url.QueryEscape(in.Str())), nil
	})
	encoder(r, "@urid", func(in *value.Value) (*value.Value, error) {
		s, err := url.QueryUnescape(nodeText(in))
		if err != nil {
			return nil, &query.RuntimeError{Msg: err.Error()}
		}
		return value.Str(s), nil
	})
	encoder(r, "@sh", func(in *value.Value) (*value.Value, error) {
		if !isStr(in) {
			return nil, &query.RuntimeError{Msg: "cannot encode " + tagOf(in) + " as URI" + uriHint}
		}
		return value.Str(shellQuote(in.Str())), nil
	})
	encoder(r, "@csv", func(in *value.Value) (*value.Value, error) { return delimited(in, ',') })
	encoder(r, "@tsv", func(in *value.Value) (*value.Value, error) { return delimited(in, '\t') })
	encoder(r, "@json", func(in *value.Value) (*value.Value, error) { return value.Str(jsonText(in, -1)), nil })
	encoder(r, "@yaml", func(in *value.Value) (*value.Value, error) { return value.Str(yamlDoc(in, 2)), nil })
	for _, name := range []string{"to_json", "tojson"} {
		encoder(r, name, func(in *value.Value) (*value.Value, error) { return value.Str(jsonText(in, 2)), nil })
		r.RegisterSingle(name, 1, indented(jsonText))
	}
	encoder(r, "to_yaml", func(in *value.Value) (*value.Value, error) { return value.Str(yamlDoc(in, 2)), nil })
	r.RegisterSingle("to_yaml", 1, indented(yamlDoc))
	for _, name := range []string{"from_json", "fromjson"} {
		encoder(r, name, func(in *value.Value) (*value.Value, error) { return parseYAML(nodeText(in), false) })
	}
	r.RegisterSingle("from_yaml", 0, func(ctx *runtime.Ctx, in *value.Value, _ []runtime.Thunk, emit runtime.Emit) error {
		noHeader := false
		if d, ok := ctx.Dialect.(*Dialect); ok {
			noHeader = d.opts.NoHeaderPreprocess
		}
		v, err := fromYAML(nodeText(in), noHeader)
		if err != nil {
			return err
		}
		return emit(v, nil)
	})
}

// encoder registers a one-input, one-output operator.
func encoder(r *runtime.Registry, name string, f func(*value.Value) (*value.Value, error)) {
	r.RegisterSingle(name, 0, func(_ *runtime.Ctx, in *value.Value, _ []runtime.Thunk, emit runtime.Emit) error {
		v, err := f(in)
		if err != nil {
			return err
		}
		return emit(v, nil)
	})
}

// indented registers the form of a conversion that takes an indent.
func indented(f func(in *value.Value, n int) string) runtime.Native {
	return func(_ *runtime.Ctx, in *value.Value, args []runtime.Thunk, emit runtime.Emit) error {
		return args[0].Each(in, func(n *value.Value, _ value.Path) error {
			return emit(value.Str(f(in, int(n.Float()))), nil)
		})
	}
}

// jsonText is v as JSON indented n spaces a level and ending in a line
// break, or on one line without one when n is zero or less.
func jsonText(v *value.Value, n int) string {
	var b bytes.Buffer
	enc := jqout.New(&b, jqout.Options{Indent: n, Compact: n <= 0})
	if err := enc.Write(v); err != nil {
		return ""
	}
	if err := enc.Flush(); err != nil {
		return ""
	}
	if n <= 0 {
		return strings.TrimSuffix(b.String(), "\n")
	}
	return b.String()
}

// yamlDoc is v as a YAML document indented n spaces a level. A scalar is
// its bare text, as yq unwraps one.
func yamlDoc(v *value.Value, n int) string {
	if v.Kind() != value.KindArray && v.Kind() != value.KindObject {
		return nodeText(v) + "\n"
	}
	return yamlText(v, n) + "\n"
}

// decodeBase64 reads padded or unpadded base64 in enc's alphabet, which
// is a padded encoding.
func decodeBase64(enc *base64.Encoding, s string) ([]byte, error) {
	if len(s)%4 != 0 {
		return enc.WithPadding(base64.NoPadding).DecodeString(strings.TrimRight(s, "="))
	}
	return enc.DecodeString(s)
}

// shellQuote quotes s for a POSIX shell the way yq's @sh does. It leaves
// the characters a shell takes literally alone, opens a single quote at
// the first one it does not, and writes a single quote as \'.
func shellQuote(s string) string {
	var b strings.Builder
	quoted := false
	for _, c := range s {
		switch {
		case c == '\'':
			if quoted {
				b.WriteByte('\'')
				quoted = false
			}
			b.WriteString(`\'`)
		case shellSafe(c):
			b.WriteRune(c)
		default:
			if !quoted {
				b.WriteByte('\'')
				quoted = true
			}
			b.WriteRune(c)
		}
	}
	if quoted {
		b.WriteByte('\'')
	}
	return b.String()
}

func shellSafe(c rune) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("_@%+=:,./-", c)
}

// delimited writes a sequence as CSV or TSV rows: a sequence of scalars
// as one row, a sequence of sequences as one row each, and a sequence of
// maps as a header row of the first map's keys and a row for each map. A
// string is already encoded and passes through.
func delimited(in *value.Value, comma rune) (*value.Value, error) {
	switch in.Kind() {
	case value.KindString:
		return in, nil
	case value.KindArray:
	default:
		if in.Kind() != value.KindObject {
			return value.Str(nodeText(in)), nil
		}
		return nil, &query.RuntimeError{Msg: "csv encoding only works for arrays, got: " + tagOf(in)}
	}
	var rows [][]string
	elems := in.Elems()
	switch {
	case len(elems) > 0 && elems[0].Kind() == value.KindObject:
		header := elems[0].Keys()
		rows = append(rows, header)
		for _, e := range elems {
			row := make([]string, len(header))
			for i, k := range header {
				if v, ok := e.Get(k); ok {
					row[i] = nodeText(v)
				}
			}
			rows = append(rows, row)
		}
	case len(elems) > 0 && elems[0].Kind() == value.KindArray:
		for _, e := range elems {
			row, err := scalarRow(e)
			if err != nil {
				return nil, err
			}
			rows = append(rows, row)
		}
	default:
		row, err := scalarRow(in)
		if err != nil {
			return nil, err
		}
		rows = append(rows, row)
	}
	var b bytes.Buffer
	w := csv.NewWriter(&b)
	w.Comma = comma
	if err := w.WriteAll(rows); err != nil {
		return nil, &query.RuntimeError{Msg: err.Error()}
	}
	return value.Str(strings.TrimSuffix(b.String(), "\n")), nil
}

// scalarRow is the text of each element of a sequence of scalars.
func scalarRow(v *value.Value) ([]string, error) {
	row := make([]string, v.Len())
	for i, e := range v.Elems() {
		if e.Kind() == value.KindArray || e.Kind() == value.KindObject {
			return nil, &query.RuntimeError{Msg: "csv encoding only works for arrays of scalars (string/numbers/booleans), child[" + strconv.Itoa(i) + "] is a " + tagOf(e)}
		}
		row[i] = nodeText(e)
	}
	return row, nil
}
