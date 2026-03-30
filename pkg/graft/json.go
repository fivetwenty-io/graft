package graft

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/goccy/go-yaml"

	"github.com/fivetwenty-io/graft/internal/utils/ansi"
)

func jsonifyData(data []byte, strict bool) (string, error) {
	doc := make(map[string]interface{})
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return "", ansi.Errorf("@R{Root of YAML document is not a hash/map}: %s\n", err.Error())
	}

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

		docs := bytes.Split(data, []byte("\n---\n"))
		// strip off empty document created if the first three bytes of the file are the doc separator
		// keeps the indexing correct for when used with error messages
		if len(docs[0]) == 0 {
			docs = docs[1:]
		}
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
