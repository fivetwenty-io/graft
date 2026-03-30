package graft

import (
	"fmt"
	"regexp"
)

// injectKeyStandaloneRe matches <<<: as a standalone map key.
var injectKeyStandaloneRe = regexp.MustCompile(`(?m)^(\s*(?:- )?)<<<:`)

// injectKeyDottedRe matches <<<: at the end of a dotted path key (e.g., host.web1.<<<:).
var injectKeyDottedRe = regexp.MustCompile(`(?m)^(\s*(?:- )?)(\S+\.<<<):`)

// QuoteInjectKeys pre-processes YAML bytes to quote the graft-specific
// <<<: inject key, which goccy/go-yaml rejects when unquoted because
// it interprets <<< as a variant of the YAML merge key <<.
// Handles both standalone (<<<:) and dotted path (foo.<<<:) forms.
func QuoteInjectKeys(data []byte) []byte {
	// First quote dotted paths (must be first to avoid double-quoting)
	data = injectKeyDottedRe.ReplaceAll(data, []byte(`${1}"${2}":`))
	// Then quote standalone <<<:
	data = injectKeyStandaloneRe.ReplaceAll(data, []byte(`${1}"<<<":`))
	return data
}

// NormalizeMap deep-converts any map[interface{}]interface{} values
// to map[string]interface{} throughout the tree. This is needed because
// yaml.v3 produces map[interface{}]interface{} when maps contain
// non-string keys (e.g., integer keys like 1:, 2:).
func NormalizeMap(data map[string]interface{}) map[string]interface{} {
	if data == nil {
		return nil
	}
	for k, v := range data {
		data[k] = normalizeValue(v)
	}
	return data
}

func normalizeValue(v interface{}) interface{} {
	switch val := v.(type) {
	case map[interface{}]interface{}:
		converted := make(map[string]interface{}, len(val))
		for k, v := range val {
			converted[fmt.Sprintf("%v", k)] = normalizeValue(v)
		}
		return converted
	case map[string]interface{}:
		return NormalizeMap(val)
	case []interface{}:
		for i, item := range val {
			val[i] = normalizeValue(item)
		}
		return val
	case uint64:
		if val > uint64(^uint(0)>>1) {
			return val // preserve for values exceeding int range
		}
		return int(val)
	case int64:
		return int(val)
	case float32:
		return float64(val)
	default:
		return v
	}
}

// YAMLCompat controls YAML 1.1 backward compatibility behavior.
type YAMLCompat struct {
	// ConvertYAML11Booleans converts "yes"/"no"/"on"/"off" strings to booleans.
	ConvertYAML11Booleans bool
}

// DefaultYAMLCompat returns compat settings with YAML 1.1 booleans enabled.
func DefaultYAMLCompat() *YAMLCompat {
	return &YAMLCompat{ConvertYAML11Booleans: true}
}

// ConvertValue applies YAML 1.1 compatibility conversions to a string value.
func (c *YAMLCompat) ConvertValue(s string) interface{} {
	if !c.ConvertYAML11Booleans {
		return s
	}
	switch s {
	case "yes", "Yes", "YES", "on", "On", "ON":
		return true
	case "no", "No", "NO", "off", "Off", "OFF":
		return false
	default:
		return s
	}
}

// ConvertMapValues recursively applies YAML 1.1 compatibility conversions.
func (c *YAMLCompat) ConvertMapValues(data map[string]interface{}) map[string]interface{} {
	if !c.ConvertYAML11Booleans {
		return data
	}
	for k, v := range data {
		data[k] = c.convertAny(v)
	}
	return data
}

func (c *YAMLCompat) convertAny(v interface{}) interface{} {
	switch val := v.(type) {
	case string:
		return c.ConvertValue(val)
	case uint64:
		if val > uint64(^uint(0)>>1) {
			return val
		}
		return int(val)
	case int64:
		return int(val)
	case float32:
		return float64(val)
	case map[string]interface{}:
		return c.ConvertMapValues(val)
	case map[interface{}]interface{}:
		// yaml.v3 produces this for maps with non-string keys (e.g., integer keys)
		for k, v := range val {
			val[k] = c.convertAny(v)
		}
		return val
	case []interface{}:
		return c.convertSlice(val)
	default:
		return v
	}
}

func (c *YAMLCompat) convertSlice(s []interface{}) []interface{} {
	for i, v := range s {
		s[i] = c.convertAny(v)
	}
	return s
}
