package graft

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/fivetwenty-io/graft/internal/utils/ansi"
)

func jsonifyData(data []byte, strict bool) (string, error) {
	// Parse through the same YAML-1.1-compat-aware path ParseYAML uses, so
	// `graft json` and `graft merge` agree on unquoted yes/no/on/off ->
	// bool coercion and on quoted lookalikes staying strings, matching
	// spruce json on both counts.
	root, err := ParseYAML11CompatAware(data)
	if err != nil {
		return "", ansi.Errorf("@R{Root of YAML document is not a hash/map}: %s\n", err.Error())
	}

	// An empty, whitespace-only, or explicit-null document unmarshals to a
	// nil root rather than an error. Require an actual map root here (as
	// spruce's simpleyaml.Map() does) so a dangling multi-doc separator or
	// blank document fails loudly instead of silently emitting "{}".
	doc, ok := root.(map[string]interface{})
	if !ok {
		return "", ansi.Errorf("@R{Root of YAML document is not a hash/map}: %s\n", "type assertion to map[string]interface{} failed")
	}

	doc = DefaultYAMLCompat().ConvertAndUnprotect(doc)

	doc_, err := deinterface(doc, strict)
	if err != nil {
		return "", err
	}

	b, err := json.Marshal(doc_)
	if err != nil {
		return "", err
	}

	return string(b), nil
}

// splitYAMLDocs splits raw multi-document YAML on the "\n---\n" document
// separator, the same convention spruce's JSONifyFiles uses. A leading
// empty document (e.g. input starting with "---\n") is dropped so index
// numbers in error messages line up with the document's actual position.
func splitYAMLDocs(data []byte) [][]byte {
	docs := bytes.Split(data, []byte("\n---\n"))
	if len(docs[0]) == 0 {
		docs = docs[1:]
	}
	return docs
}

// JSONifyIO reads from a reader and converts to JSON format.
func JSONifyIO(in io.Reader, strict bool) (string, error) {
	data, err := io.ReadAll(in)
	if err != nil {
		return "", ansi.Errorf("@R{Error reading input}: %s", err)
	}
	return jsonifyData(data, strict)
}

// JSONifyFiles reads files and converts them to JSON format.
func JSONifyFiles(paths []string, strict bool) ([]string, error) {
	l := []string{}
	var err error
	for _, path := range paths {
		data := []byte{}
		if path == "-" {
			DEBUG("Processing STDIN")
			stat, statErr := os.Stdin.Stat()
			if statErr != nil {
				return nil, ansi.Errorf("@R{Error statting STDIN} - Bailing out: %s\n", statErr.Error())
			}
			if stat.Mode()&os.ModeCharDevice == 0 {
				data, err = io.ReadAll(os.Stdin)
				if err != nil {
					return nil, ansi.Errorf("@R{Error reading STDIN}: %s\n", err.Error())
				}
			}
		} else {
			DEBUG("Processing file '%s'", path)
			// #nosec G304 - File path is from user-provided command line arguments which is expected behavior for processing YAML files
			data, err = os.ReadFile(path)
			if err != nil {
				return nil, ansi.Errorf("@R{Error reading file} @m{%s}: %s", path, err)
			}
		}

		docs := splitYAMLDocs(data)
		for i, doc := range docs {
			jsonData, err := jsonifyData(doc, strict)
			if err != nil {
				return nil, ansi.Errorf("%s[%d]: %s", path, i, err)
			}
			l = append(l, jsonData)
		}
	}

	return l, nil
}

// readJSONSource reads the raw bytes for one JSONifyFiles/YAMLifyFiles
// input path, applying the same stdin ("-") convention both directions
// share: reading os.Stdin only if it is actually piped (not a character
// device), and erroring by path name on any read failure.
func readJSONSource(path string) ([]byte, error) {
	if path == "-" {
		DEBUG("Processing STDIN")
		stat, statErr := os.Stdin.Stat()
		if statErr != nil {
			return nil, ansi.Errorf("@R{Error statting STDIN} - Bailing out: %s\n", statErr.Error())
		}
		if stat.Mode()&os.ModeCharDevice != 0 {
			return []byte{}, nil
		}
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return nil, ansi.Errorf("@R{Error reading STDIN}: %s\n", err.Error())
		}
		return data, nil
	}

	DEBUG("Processing file '%s'", path)
	// #nosec G304 - File path is from user-provided command line arguments which is expected behavior for processing YAML/JSON files
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, ansi.Errorf("@R{Error reading file} @m{%s}: %s", path, err)
	}
	return data, nil
}

// YAMLifyFiles reads files (or stdin via "-") containing JSON and converts
// each top-level JSON value found to a YAML document. Input may be either
// one compact JSON document per line (the shape JSONifyFiles/`graft json`
// itself produces, enabling a `graft json | graft json --reverse`
// round-trip) or a single pretty-printed, multi-line JSON document (e.g.
// piped from `curl`); a streaming decoder is used so both shapes work
// without a format flag. Each returned string is one YAML document with no
// trailing newline and no `---` separator; the caller decides how to join
// multiple documents.
func YAMLifyFiles(paths []string) ([]string, error) {
	out := []string{}
	for _, path := range paths {
		data, err := readJSONSource(path)
		if err != nil {
			return nil, err
		}

		docs, decErr := decodeJSONDocs(data)
		if decErr != nil {
			return nil, ansi.Errorf("@m{%s}: @R{Error parsing JSON}: %s\n", path, decErr.Error())
		}
		if len(docs) == 0 {
			return nil, ansi.Errorf("@m{%s}: @R{Error parsing JSON}: no JSON documents found\n", path)
		}

		for i, doc := range docs {
			yamlBytes, mErr := MarshalYAML(doc)
			if mErr != nil {
				return nil, ansi.Errorf("@m{%s}[%d]: @R{Error converting JSON to YAML}: %s\n", path, i, mErr.Error())
			}
			out = append(out, strings.TrimRight(string(yamlBytes), "\n"))
		}
	}
	return out, nil
}

// decodeJSONDocs decodes every top-level JSON value present in data using a
// streaming decoder, so it accepts both newline-delimited compact JSON
// ("JSON Lines") and a single pretty-printed multi-line JSON value
// identically. Empty (whitespace-only) input yields zero documents and no
// error; the caller decides whether that is itself an error.
func decodeJSONDocs(data []byte) ([]interface{}, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	// UseNumber preserves the source text of every JSON number as a
	// json.Number instead of collapsing every number to float64 (Go's
	// encoding/json default for interface{} targets). Without this,
	// `graft json --reverse` on `{"port":5432}` would emit `port: 5432.0`
	// instead of `port: 5432`. normalizeJSONNumbers converts each
	// json.Number back to int64 (whole numbers) or float64 (fractional)
	// before YAML marshaling.
	dec.UseNumber()
	docs := []interface{}{}
	for {
		var v interface{}
		if err := dec.Decode(&v); err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}
		docs = append(docs, normalizeJSONNumbers(v))
	}
	return docs, nil
}

// normalizeJSONNumbers recursively converts every json.Number produced by a
// UseNumber()-enabled decode into an int64 (when the source text has no
// fractional/exponent part and fits in 64 bits) or a float64 otherwise, so
// YAML marshaling renders whole numbers without a trailing ".0".
func normalizeJSONNumbers(v interface{}) interface{} {
	switch t := v.(type) {
	case json.Number:
		if i, err := t.Int64(); err == nil {
			return i
		}
		if f, err := t.Float64(); err == nil {
			return f
		}
		// Unreachable for well-formed JSON numbers, but fall back to the
		// original string representation rather than dropping data.
		return t.String()
	case map[string]interface{}:
		for k, sub := range t {
			t[k] = normalizeJSONNumbers(sub)
		}
		return t
	case []interface{}:
		for i, sub := range t {
			t[i] = normalizeJSONNumbers(sub)
		}
		return t
	default:
		return v
	}
}

// CombineJSONLines wraps a set of already-serialized compact JSON documents
// (as produced by jsonifyData, one per YAML document) into a single
// pretty-printed JSON array. This backs `graft json --multi-doc`, which
// trades the default one-JSON-object-per-line output for a single
// machine-parseable array value. A nil/empty input produces "[]" rather
// than an error, matching encoding/json's own empty-slice marshaling.
func CombineJSONLines(lines []string) (string, error) {
	raws := make([]json.RawMessage, len(lines))
	for i, line := range lines {
		raws[i] = json.RawMessage(line)
	}

	b, err := json.MarshalIndent(raws, "", "  ")
	if err != nil {
		return "", ansi.Errorf("@R{Error combining JSON documents into an array}: %s", err.Error())
	}
	return string(b), nil
}

func deinterface(o interface{}, strict bool) (interface{}, error) {
	switch v := o.(type) {
	case map[string]interface{}:
		return deinterfaceMap(v, strict)
	case []interface{}:
		return deinterfaceList(v, strict)
	default:
		return o, nil
	}
}

func addKeyToMap(m map[string]interface{}, k, v interface{}, strict bool) error {
	vs := fmt.Sprintf("%v", k)
	_, exists := m[vs]
	if exists {
		NewWarningError(eContextAll, "@Y{Duplicate key detected: %s}", vs).Warn()
		return nil
	}
	dv, err := deinterface(v, strict)
	if err != nil {
		return err
	}
	m[vs] = dv
	return nil
}

func deinterfaceMap(o map[string]interface{}, strict bool) (map[string]interface{}, error) {
	m := map[string]interface{}{}
	for k, v := range o {
		err := addKeyToMap(m, k, v, strict)
		if err != nil {
			return nil, err
		}
	}
	return m, nil
}

func deinterfaceList(o []interface{}, strict bool) ([]interface{}, error) {
	l := make([]interface{}, len(o))
	for i, v := range o {
		v_, err := deinterface(v, strict)
		if err != nil {
			return nil, err
		}
		l[i] = v_
	}
	return l, nil
}
