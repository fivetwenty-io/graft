package jqout

import (
	"fmt"
	"strings"
)

// Palette holds the SGR parameters for each kind of token, without the
// escape introducer or the trailing m.
type Palette struct {
	Null, False, True, Number, String, Array, Object, ObjectKey string
}

// plainFg is the default foreground jq uses for false, true, and numbers.
const plainFg = "0;39"

// DefaultPalette is jq 1.8's default color scheme.
var DefaultPalette = Palette{
	Null:      "0;90",
	False:     plainFg,
	True:      plainFg,
	Number:    plainFg,
	String:    "0;32",
	Array:     "1;39",
	Object:    "1;39",
	ObjectKey: "1;34",
}

// ParsePalette applies a JQ_COLORS value to the default palette. The fields
// are colon separated in the order null, false, true, numbers, strings,
// arrays, objects, and object keys. A missing trailing field keeps its
// default, an empty field becomes an empty color as in jq, and a field with
// anything but digits and semicolons is an error that leaves the default
// palette in place.
func ParsePalette(jqColors string) (Palette, error) {
	p := DefaultPalette
	fields := []*string{&p.Null, &p.False, &p.True, &p.Number, &p.String, &p.Array, &p.Object, &p.ObjectKey}
	for i, f := range strings.Split(jqColors, ":") {
		if i >= len(fields) {
			break
		}
		for _, c := range f {
			if c != ';' && (c < '0' || c > '9') {
				return DefaultPalette, fmt.Errorf("invalid color %q", f)
			}
		}
		*fields[i] = f
	}
	return p, nil
}

const colorReset = "\x1b[0m"

func colorStart(sgr string) string { return "\x1b[" + sgr + "m" }
