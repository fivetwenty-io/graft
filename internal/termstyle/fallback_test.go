package termstyle

import (
	"math"
	"strconv"
	"testing"

	"github.com/fivetwenty-io/graft/internal/yamlgolden"
)

func TestClosest16Vectors(t *testing.T) {
	var vectors []struct {
		RGB  [3]uint8 `json:"rgb"`
		Code uint8    `json:"code"`
	}
	yamlgolden.ReadJSON(t, "testdata/golden/closest16.json", &vectors)
	if len(vectors) < 4000 {
		t.Fatalf("closest16.json holds %d vectors, want at least 4000", len(vectors))
	}
	for _, v := range vectors {
		if got := Closest16(RGB{v.RGB[0], v.RGB[1], v.RGB[2]}); got != v.Code {
			t.Errorf("Closest16(%v) = %d, want %d", v.RGB, got, v.Code)
		}
	}
}

func TestClosest16UserColors(t *testing.T) {
	for c, want := range map[RGB]uint8{
		{255, 0, 255}:   95, // \e[38;2;255;0;255m inside a value, and palette index 201
		{222, 56, 43}:   91, // bunt's parse-table red, so a re-parsed 31 renders as 91
		{100, 149, 237}: 94, // CornflowerBlue, for URL locations in load errors
		{199, 196, 63}:  93, // the yellow change header
	} {
		if got := Closest16(c); got != want {
			t.Errorf("Closest16(%v) = %d, want %d", c, got, want)
		}
	}
}

func TestCiede2000Vectors(t *testing.T) {
	var vectors []struct {
		A    [3]uint8 `json:"a"`
		B    [3]uint8 `json:"b"`
		Diff string   `json:"diff"`
	}
	yamlgolden.ReadJSON(t, "testdata/golden/ciede2000.json", &vectors)
	for _, v := range vectors {
		want, err := strconv.ParseFloat(v.Diff, 64)
		if err != nil {
			t.Fatal(err)
		}
		got := ciede2000(RGB{v.A[0], v.A[1], v.A[2]}, RGB{v.B[0], v.B[1], v.B[2]})
		if math.Abs(got-want) > 1e-9 {
			t.Errorf("ciede2000(%v, %v) = %v, want %v", v.A, v.B, got, want)
		}
	}
}

func TestSixteenColorRoundTrip(t *testing.T) {
	in := []int{30, 31, 32, 33, 34, 35, 36, 37, 90, 91, 92, 93, 94, 95, 96, 97}
	out := []int{30, 91, 32, 33, 94, 35, 36, 37, 37, 91, 92, 33, 34, 95, 96, 97}
	for i, code := range in {
		got, err := Mode{Color: true}.Style("\x1b[" + strconv.Itoa(code) + "mx")
		want := "\x1b[" + strconv.Itoa(out[i]) + "mx\x1b[0m"
		if err != nil || got != want {
			t.Errorf("re-rendering code %d gave %q, %v; want %q", code, got, err, want)
		}
	}
}
