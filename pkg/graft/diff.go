package graft

import (
	"regexp"
	"sort"
	"strings"

	"github.com/geofffranks/yaml"

	fmt "github.com/fivetwenty-io/graft/internal/utils/ansi"
)

func pad1(pad, s string) string {
	return strings.TrimSpace(indent(pad, s))
}

func indent(pad, s string) string {
	re := regexp.MustCompile(`(?m)^`)
	return re.ReplaceAllString(s, pad)
}

func yamlstring(f string, x Diffable) string {
	const defaultPad = "    "
	s, _ := yaml.Marshal(x.Value())
	return fmt.Sprintf(f, indent(defaultPad, strings.TrimSuffix(string(s), "\n")))
}

func yamlmarshal(x interface{}) string {
	s, _ := yaml.Marshal(x)
	return fmt.Sprintf("%s", s)
}

func sortkeys(m map[string]Diffable) []string {
	kk := make([]string, 0)
	for k := range m {
		kk = append(kk, k)
	}
	sort.Strings(kk)
	return kk
}

// Type represents the type category of a YAML value.
type Type int

// Type constants for YAML value classification.
const (
	// Scalar represents a scalar value (string, number, boolean, null).
	Scalar Type = iota
	// Map represents a map/object value.
	Map
	// SimpleList represents a list of non-keyed items.
	SimpleList
	// KeyedList represents a list of keyed items (maps with name/id/key).
	KeyedList
)

func keyed(l []interface{}) string {
	for _, v := range l {
		if typeof(v) != Map {
			return ""
		}
	}

KEYSEARCH:
	for _, k := range []string{"name", "id", "key"} {
		for _, v := range l {
			o, ok := v.(map[interface{}]interface{})
			if !ok {
				continue KEYSEARCH
			}
			if _, ok := o[k]; !ok {
				continue KEYSEARCH
			}
		}

		return k
	}

	return ""
}

func mapify(l []interface{}, key string) map[interface{}]interface{} {
	m := make(map[interface{}]interface{})

	for _, v := range l {
		if typeof(v) != Map {
			return nil
		}
		o, ok := v.(map[interface{}]interface{})
		if !ok {
			return nil
		}
		k, ok := o[key]
		if !ok {
			return nil
		}
		m[k] = v
	}

	return m
}

func (t Type) String() string {
	switch t {
	case Scalar:
		return "scalar"
	case Map:
		return valueTypeMap
	case SimpleList:
		return "simple list"
	case KeyedList:
		return "keyed list"
	default:
		return "unknown type"
	}
}

func typeof(x interface{}) Type {
	switch v := x.(type) {
	case map[interface{}]interface{}:
		return Map
	case []interface{}:
		if keyed(v) != "" {
			return KeyedList
		}
		return SimpleList

	default:
		return Scalar
	}
}

// Diffable represents a value that can be compared in a diff operation.
type Diffable interface {
	// Changed returns true if the value has changed.
	Changed() bool
	// String returns a formatted string representation of the change.
	String(key string) string
	// Value returns the current value.
	Value() interface{}
}

// DiffNone represents an unchanged value.
type DiffNone struct {
	Orig interface{}
}

// Changed returns false for unchanged values.
func (d DiffNone) Changed() bool {
	return false
}

// String returns an empty string for unchanged values.
func (d DiffNone) String(key string) string {
	return ""
}

// Value returns the original value.
func (d DiffNone) Value() interface{} {
	return d.Orig
}

// DiffType represents a type change between old and new values.
type DiffType struct {
	Old interface{}
	New interface{}
}

// Changed returns true if the types differ.
func (d DiffType) Changed() bool {
	return typeof(d.Old) != typeof(d.New)
}

// String returns a formatted description of the type change.
func (d DiffType) String(key string) string {
	return fmt.Sprintf("  @C{%s} changed type\n    from @R{%s}\n      to @G{%s}\n\n",
		key, typeof(d.Old), typeof(d.New))
}

// Value returns nil for type changes.
func (d DiffType) Value() interface{} {
	return nil
}

// DiffScalar represents a change between two scalar values.
type DiffScalar struct {
	Old string
	New string
}

// Changed returns true if old and new scalar values differ.
func (d DiffScalar) Changed() bool {
	return d.Old != d.New
}

// String returns a formatted description of the scalar change.
func (d DiffScalar) String(key string) string {
	return fmt.Sprintf("  @C{%s} changed value\n    from @R{%s}\n      to @G{%s}\n\n",
		key, pad1("         ", d.Old), pad1("         ", d.New))
}

// Value returns nil for scalar changes.
func (d DiffScalar) Value() interface{} {
	return nil
}

// DiffMap represents changes between two maps.
type DiffMap struct {
	Removed map[string]Diffable
	Added   map[string]Diffable
	Common  map[string]Diffable
}

// Changed returns true if any entries were added, removed, or modified.
func (d DiffMap) Changed() bool {
	if len(d.Removed)+len(d.Added) > 0 {
		return true
	}

	for _, x := range d.Common {
		if x.Changed() {
			return true
		}
	}
	return false
}

// String returns a formatted description of the map changes.
func (d DiffMap) String(key string) string {
	s := ""

	for _, k := range sortkeys(d.Added) {
		v := d.Added[k]
		s += fmt.Sprintf("  @C{%s.%s} added\n", key, k)
		s += yamlstring("@G{%s}\n\n", v)
	}
	for _, k := range sortkeys(d.Removed) {
		v := d.Removed[k]
		s += fmt.Sprintf("  @C{%s.%s} removed\n", key, k)
		s += yamlstring("@R{%s}\n\n", v)
	}

	for _, k := range sortkeys(d.Common) {
		v := d.Common[k]
		if v.Changed() {
			s += v.String(fmt.Sprintf("%s.%v", key, k))
		}
	}
	return s
}

// Value returns nil for map changes.
func (d DiffMap) Value() interface{} {
	return nil
}

// DiffList represents changes between two lists.
type DiffList struct {
	Removed map[string]Diffable
	Added   map[string]Diffable
	Common  map[string]Diffable
}

// Changed returns true if any list items were added, removed, or modified.
func (d DiffList) Changed() bool {
	if len(d.Removed)+len(d.Added) > 0 {
		return true
	}
	for _, x := range d.Common {
		if x.Changed() {
			return true
		}
	}
	return false
}

// String returns a formatted description of the list changes.
func (d DiffList) String(key string) string {
	s := ""

	for _, k := range sortkeys(d.Added) {
		v := d.Added[k]
		s += fmt.Sprintf("  @C{%s[%s]} added\n", key, k)
		s += yamlstring("@G{%s}\n\n", v)
	}
	for _, k := range sortkeys(d.Removed) {
		v := d.Removed[k]
		s += fmt.Sprintf("  @C{%s[%s]} removed\n", key, k)
		s += yamlstring("@R{%s}\n\n", v)
	}
	for _, k := range sortkeys(d.Common) {
		v := d.Common[k]
		if v.Changed() {
			s += v.String(fmt.Sprintf("%s[%s]", key, k))
		}
	}
	return s
}

// Value returns nil for list changes.
func (d DiffList) Value() interface{} {
	return nil
}

// Diff computes the difference between two values and returns a Diffable.
func Diff(a, b interface{}) (Diffable, error) {
	if typeof(a) != typeof(b) {
		return DiffType{Old: a, New: b}, nil
	}

	switch typeof(a) {
	case Scalar:
		return DiffScalar{Old: yamlmarshal(a), New: yamlmarshal(b)}, nil
	case Map:
		aMap, aOk := a.(map[interface{}]interface{})
		bMap, bOk := b.(map[interface{}]interface{})
		if !aOk || !bOk {
			return DiffScalar{}, fmt.Errorf("expected map types")
		}
		return diffMaps(aMap, bMap)
	case SimpleList:
		aList, aOk := a.([]interface{})
		bList, bOk := b.([]interface{})
		if !aOk || !bOk {
			return DiffScalar{}, fmt.Errorf("expected list types")
		}
		return diffSimpleLists(aList, bList)
	case KeyedList:
		aList, aOk := a.([]interface{})
		bList, bOk := b.([]interface{})
		if !aOk || !bOk {
			return DiffScalar{}, fmt.Errorf("expected list types")
		}
		return diffKeyedLists(aList, bList)
	default:
		return DiffScalar{}, fmt.Errorf("diff not implemented for this type")
	}
}

// diffMaps computes the difference between two maps.
func diffMaps(ma, mb map[interface{}]interface{}) (DiffMap, error) {
	x := DiffMap{
		Removed: make(map[string]Diffable),
		Added:   make(map[string]Diffable),
		Common:  make(map[string]Diffable),
	}

	for k, v1 := range ma {
		key := fmt.Sprintf("%v", k)
		if v2, ok := mb[k]; ok {
			d, err := Diff(v1, v2)
			if err != nil {
				return x, err
			}
			x.Common[key] = d
		} else {
			x.Removed[key] = DiffNone{v1}
		}
	}

	for k, v2 := range mb {
		if _, ok := ma[k]; !ok {
			x.Added[fmt.Sprintf("%v", k)] = DiffNone{v2}
		}
	}
	return x, nil
}

// diffSimpleLists computes the difference between two simple lists.
func diffSimpleLists(la, lb []interface{}) (DiffList, error) {
	x := DiffList{
		Removed: make(map[string]Diffable),
		Added:   make(map[string]Diffable),
		Common:  make(map[string]Diffable),
	}

	for i, v1 := range la {
		key := fmt.Sprintf("%d", i)
		if i < len(lb) {
			d, err := Diff(v1, lb[i])
			if err != nil {
				return x, err
			}
			x.Common[key] = d
		} else {
			x.Removed[key] = DiffNone{v1}
		}
	}

	for i := len(la); i < len(lb); i++ {
		x.Added[fmt.Sprintf("%d", i)] = DiffNone{lb[i]}
	}
	return x, nil
}

// diffKeyedLists computes the difference between two keyed lists.
func diffKeyedLists(la, lb []interface{}) (DiffList, error) {
	key := keyed(la)
	ma := mapify(la, key)
	mb := mapify(lb, key)

	x := DiffList{
		Removed: make(map[string]Diffable),
		Added:   make(map[string]Diffable),
		Common:  make(map[string]Diffable),
	}

	for k, v1 := range ma {
		keyStr := fmt.Sprintf("%v", k)
		if v2, ok := mb[k]; ok {
			d, err := Diff(v1, v2)
			if err != nil {
				return x, err
			}
			x.Common[keyStr] = d
		} else {
			x.Removed[keyStr] = DiffNone{v1}
		}
	}

	for k, v2 := range mb {
		if _, ok := ma[k]; !ok {
			x.Added[fmt.Sprintf("%v", k)] = DiffNone{v2}
		}
	}
	return x, nil
}
