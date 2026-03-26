package graft

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
	case map[string]interface{}:
		return c.ConvertMapValues(val)
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
