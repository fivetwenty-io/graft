package main

import (
	"encoding/json"
	"fmt"
	"image/color"
	"math"
	"math/rand"
	"regexp"
	"strconv"
	"strings"

	"github.com/gonvenience/bunt"
	"github.com/gonvenience/neat"
	"github.com/gonvenience/text"
	"github.com/homeport/dyff/pkg/dyff"
	"github.com/lucasb-eyer/go-colorful"
	ciede2000 "github.com/mattn/go-ciede2000"
	"github.com/texttheater/golang-levenshtein/levenshtein"
	yaml "go.yaml.in/yaml/v3"
)

// renderModes are the three bunt settings the renderer can run under.
// bunt's state type is unexported, so each mode carries a setter.
var renderModes = []struct {
	name string
	set  func()
}{
	{"off", func() { bunt.SetColorSettings(bunt.OFF, bunt.OFF) }},
	{"truecolor", func() { bunt.SetColorSettings(bunt.ON, bunt.ON) }},
	{"ansi16", func() { bunt.SetColorSettings(bunt.ON, bunt.OFF) }},
}

func renderVectors(root string, out outputs) error {
	out.json("internal/termstyle/testdata/golden/style.json", styleVectors())
	out.json("internal/termstyle/testdata/golden/closest16.json", closest16Vectors())
	out.json(ciedePath, ciedeVectors())
	out.json("internal/humanreport/testdata/golden/plural.json", pluralVectors())
	out.json("internal/humanreport/testdata/golden/table.json", tableVectors())
	out.json("internal/humanreport/testdata/golden/minor.json", minorVectors())
	neatVecs, err := neatVectors()
	if err != nil {
		return err
	}
	out.json("internal/humanreport/testdata/golden/neat.json", neatVecs)
	bunt.SetColorSettings(bunt.OFF, bunt.OFF)
	return nil
}

// ---- style ---------------------------------------------------------------

var styleTexts = []string{
	"k:", "± value change", "a\nb", "a\nb\n", "\n", "", "日本語", "tab\tx",
	"red\x1b[31mtext\x1b[0m", "x\x1b[1my", "\x1b[38;5;201mz\nw", "p\x1b[41mq",
	"\x1b[38;2;255;0;255mm\nn", "a\x1b[mb", "a\x1b[Kb", "\x1b[?1hx", "a\x1b=b\x1b>c",
	"\x1b[1;31mbold red\x1b[0m", "\x1b[3;93mitalic\x1b[0;93m plain",
	"\x1b[38;2;178;34;34mk:\x1b[0m", "\x1b[31mk:\x1b[0m", "\x1b[32mk:\x1b[0m",
	"\x1b[48;5;9mbg", "\x1b[300mwrap", "\x1b[38m", "\x1b[48;7m", "\x1b[0;1;38;2;1m",
	// The \x1b[m rewrite runs before the deletions, codes inside one
	// sequence OR together from zero, and turning off only a color emits
	// no reset.
	"a\x1b[\x1b[Kmb", "\x1b[31;32mx", "\x1b[1;0;31mx", "\x1b[99999999999999999999;31mx",
	"\x1b[1;31ma\x1b[1mb",
}

var styleOptionSets = [][]string{
	{}, {"bold"}, {"italic"}, {"bold", "italic"}, {"underline", "fg:100,149,237"},
	{"eachline", "fg:185,49,27"}, {"eachline", "fg:88,191,56"}, {"eachline", "fg:199,196,63"},
	{"fg:178,34,34"}, {"fg:178,34,34", "bold"}, {"bold", "eachline", "fg:185,49,27"},
}

type styleVector struct {
	Mode  string   `json:"mode"`
	Text  string   `json:"text"`
	Opts  []string `json:"opts"`
	Out   string   `json:"out,omitempty"`
	Panic string   `json:"panic,omitempty"`
}

func buntOptions(specs []string) []bunt.StyleOption {
	var opts []bunt.StyleOption
	for _, s := range specs {
		switch {
		case s == "bold":
			opts = append(opts, bunt.Bold())
		case s == "italic":
			opts = append(opts, bunt.Italic())
		case s == "underline":
			opts = append(opts, bunt.Underline())
		case s == "eachline":
			opts = append(opts, bunt.EachLine())
		case strings.HasPrefix(s, "fg:"):
			var r, g, b uint8
			if _, err := fmt.Sscanf(s, "fg:%d,%d,%d", &r, &g, &b); err != nil {
				panic(err)
			}
			opts = append(opts, bunt.Foreground(colorful.Color{R: float64(r) / 255, G: float64(g) / 255, B: float64(b) / 255}))
		}
	}
	return opts
}

func styleOnce(text string, specs []string) (out, panicText string) {
	defer func() {
		if r := recover(); r != nil {
			panicText = fmt.Sprint(r)
		}
	}()
	return bunt.Style(text, buntOptions(specs)...), ""
}

func styleVectors() []styleVector {
	var result []styleVector
	for _, m := range renderModes {
		m.set()
		for _, text := range styleTexts {
			for _, specs := range styleOptionSets {
				o, p := styleOnce(text, specs)
				result = append(result, styleVector{Mode: m.name, Text: text, Opts: specs, Out: o, Panic: p})
			}
		}
	}
	return result
}

// ---- 16-color fallback ---------------------------------------------------

var sgrCode = regexp.MustCompile(`^\x1b\[(\d+)mx\x1b\[0m$`)

type closestVector struct {
	RGB  [3]uint8 `json:"rgb"`
	Code uint8    `json:"code"`
}

// tieMargin is the gap below which two candidates count as tied, so bunt's
// map iteration may pick either and the color is left out. The gap's last
// bits differ between arm64 and amd64, so a margin this close to the
// threshold would let the two architectures keep different colors.
const tieMargin = 1e-6

var ansi16Palette = []color.RGBA{
	{0x00, 0x00, 0x00, 0xFF}, {0xAA, 0x00, 0x00, 0xFF}, {0x00, 0xAA, 0x00, 0xFF}, {0xFF, 0xFF, 0x00, 0xFF},
	{0x00, 0x00, 0xAA, 0xFF}, {0xAA, 0x00, 0xAA, 0xFF}, {0x00, 0xAA, 0xAA, 0xFF}, {0xAA, 0xAA, 0xAA, 0xFF},
	{0x55, 0x55, 0x55, 0xFF}, {0xFF, 0x55, 0x55, 0xFF}, {0x55, 0xFF, 0x55, 0xFF}, {0xFF, 0xFF, 0x55, 0xFF},
	{0x55, 0x55, 0xFF, 0xFF}, {0xFF, 0x55, 0xFF, 0xFF}, {0x55, 0xFF, 0xFF, 0xFF}, {0xFF, 0xFF, 0xFF, 0xFF},
}

// margin is the gap between the best and second-best ciede2000 distance.
func margin(r, g, b uint8) float64 {
	target := &color.RGBA{r, g, b, 0xFF}
	best, second := math.MaxFloat64, math.MaxFloat64
	for i := range ansi16Palette {
		d := ciede2000.Diff(target, &ansi16Palette[i])
		switch {
		case d < best:
			best, second = d, best
		case d < second:
			second = d
		}
	}
	return second - best
}

func closest16Vectors() []closestVector {
	bunt.SetColorSettings(bunt.ON, bunt.OFF)
	var colors [][3]uint8
	for r := 0; r <= 255; r += 17 {
		for g := 0; g <= 255; g += 17 {
			for b := 0; b <= 255; b += 17 {
				colors = append(colors, [3]uint8{uint8(r), uint8(g), uint8(b)})
			}
		}
	}
	colors = append(colors,
		[3]uint8{255, 0, 255}, [3]uint8{222, 56, 43}, [3]uint8{57, 181, 74}, [3]uint8{199, 196, 63},
		[3]uint8{185, 49, 27}, [3]uint8{88, 191, 56}, [3]uint8{255, 160, 122}, [3]uint8{144, 238, 144},
		[3]uint8{176, 196, 222}, [3]uint8{105, 105, 105}, [3]uint8{178, 34, 34}, [3]uint8{51, 0, 0},
		[3]uint8{240, 128, 128}, [3]uint8{233, 150, 122}, [3]uint8{250, 128, 114}, [3]uint8{0, 128, 0},
		[3]uint8{0, 51, 0}, [3]uint8{50, 205, 50}, [3]uint8{107, 142, 35}, [3]uint8{128, 128, 0},
		[3]uint8{85, 107, 47}, [3]uint8{100, 149, 237},
	)
	var result []closestVector
	for _, c := range colors {
		m := margin(c[0], c[1], c[2])
		if math.Abs(m-tieMargin) < 1e-9 {
			panic(fmt.Sprintf("margin %v for %v is too close to the tie threshold", m, c))
		}
		if m < tieMargin {
			continue
		}
		s := bunt.Style("x", bunt.Foreground(colorful.Color{R: float64(c[0]) / 255, G: float64(c[1]) / 255, B: float64(c[2]) / 255}))
		match := sgrCode.FindStringSubmatch(s)
		if match == nil {
			panic(fmt.Sprintf("unexpected bunt output %q", s))
		}
		code, _ := strconv.Atoi(match[1])
		result = append(result, closestVector{RGB: c, Code: uint8(code)})
	}
	return result
}

const ciedePath = "internal/termstyle/testdata/golden/ciede2000.json"

// ciedeVector's Diff is written with ten decimals. Go fuses multiply-adds
// on arm64 but not on amd64, so the two architectures disagree in a
// distance's last bits, and a value near a rounding boundary can still
// round differently. -check therefore compares this file with sameCiede.
type ciedeVector struct {
	A    [3]uint8 `json:"a"`
	B    [3]uint8 `json:"b"`
	Diff string   `json:"diff"`
}

func ciedeVectors() []ciedeVector {
	rng := rand.New(rand.NewSource(1))
	var result []ciedeVector
	for i := 0; i < 300; i++ {
		a := [3]uint8{uint8(rng.Intn(256)), uint8(rng.Intn(256)), uint8(rng.Intn(256))}
		b := [3]uint8{uint8(rng.Intn(256)), uint8(rng.Intn(256)), uint8(rng.Intn(256))}
		if i < 20 {
			a = [3]uint8{uint8(i), uint8(i / 2), uint8(i / 3)} // near-black, the 7.787 branch
		}
		d := ciede2000.Diff(&color.RGBA{a[0], a[1], a[2], 0xFF}, &color.RGBA{b[0], b[1], b[2], 0xFF})
		result = append(result, ciedeVector{A: a, B: b, Diff: strconv.FormatFloat(d, 'f', 10, 64)})
	}
	return result
}

// sameCiede reports whether the ciede2000.json on disk holds the same
// color pairs as the generated one, written with ten decimals, and with
// distances no more than 1e-9 apart, the tolerance graft's
// TestCiede2000Vectors uses.
func sameCiede(have, want []byte) bool {
	var h, w []ciedeVector
	if json.Unmarshal(have, &h) != nil || json.Unmarshal(want, &w) != nil || len(h) != len(w) {
		return false
	}
	for i := range h {
		if h[i].A != w[i].A || h[i].B != w[i].B {
			return false
		}
		hd, err := strconv.ParseFloat(h[i].Diff, 64)
		if err != nil || h[i].Diff != strconv.FormatFloat(hd, 'f', 10, 64) {
			return false
		}
		wd, err := strconv.ParseFloat(w[i].Diff, 64)
		if err != nil || math.Abs(hd-wd) > 1e-9 {
			return false
		}
	}
	return true
}

// ---- text helpers --------------------------------------------------------

type pluralVector struct {
	N     int      `json:"n"`
	Words []string `json:"words"`
	Out   string   `json:"out"`
}

func pluralVectors() []pluralVector {
	var result []pluralVector
	for _, words := range [][]string{{"difference"}, {"document"}, {"map entry", "map entries"}, {"list entry", "list entries"}} {
		for n := 0; n <= 25; n++ {
			result = append(result, pluralVector{N: n, Words: words, Out: text.Plural(n, words...)})
		}
	}
	return result
}

type tableVector struct {
	Separator string   `json:"separator"`
	Indent    int      `json:"indent"`
	Columns   []string `json:"columns"`
	Out       string   `json:"out"`
}

func tableVectors() []tableVector {
	inputs := []struct {
		sep     string
		indent  int
		columns []string
	}{
		{"   ", 0, []string{"a\nbb\nccc", "x\ny"}},
		{"   ", 0, []string{"empty: \n", "k: v\n"}},
		{" ", 2, []string{"- a, b", "+ b, a"}},
		{"   ", 0, []string{"\x1b[31m- one\x1b[0m\n\x1b[31m  two\x1b[0m", "\x1b[32m+ 日本\x1b[0m"}},
		{"   ", 0, []string{"only one column\n\n"}},
		{" ", 2, []string{"a\nb\nc", "1", "x\ny"}},
	}
	var result []tableVector
	for _, in := range inputs {
		result = append(result, tableVector{Separator: in.sep, Indent: in.indent, Columns: in.columns, Out: dyff.CreateTableStyleString(in.sep, in.indent, in.columns...)})
	}
	return result
}

type minorVector struct {
	A        string `json:"a"`
	B        string `json:"b"`
	Distance int    `json:"distance"`
}

func minorVectors() []minorVector {
	rng := rand.New(rand.NewSource(2))
	alphabet := []rune("ab日 \n")
	word := func() string {
		n := rng.Intn(10)
		rs := make([]rune, n)
		for i := range rs {
			rs[i] = alphabet[rng.Intn(len(alphabet))]
		}
		return string(rs)
	}
	pairs := [][2]string{{"8080", "8081"}, {"", ""}, {"abc", ""}, {"kitten", "sitting"}, {"hello world", "hello there world"}}
	for i := 0; i < 2000; i++ {
		pairs = append(pairs, [2]string{word(), word()})
	}
	var result []minorVector
	for _, p := range pairs {
		d := levenshtein.DistanceForStrings([]rune(p[0]), []rune(p[1]), levenshtein.DefaultOptions)
		result = append(result, minorVector{A: p[0], B: p[1], Distance: d})
	}
	return result
}

// ---- neat ----------------------------------------------------------------

var neatInputs = []string{
	"removed_block:\n  # comment on key\n  inner: &anc\n    deep: value   # line comment\n    other: ~\n  alias_ref: *anc\n  empty: \"\"\n  dash: -x\n  num_like: \"1.2.3\"\n  special: \"a:b\"\n  t: \"true\"\n  emptymap: {}\n  emptylist: []\n  seq_of_maps:\n  - name: s1\n    v: 1\n  - id: 2\n",
	"q:\n  uni: \"café ☃\"\n  tab: \"a\\tb\"\n  ctl: \"x\\u0001y\"\n  e: e\n  dot: \".\"\n  plus: \"+1\"\n  hash: \"a#b\"\n  star: \"*x\"\n  amp: \"&x\"\n  inf: \".inf\"\n  nul: \"null\"\n  yes: yes\n  date: 2001-12-14\n  hexv: 0x10\n  big: 12345678901234567890\n  tagged: !custom foo\n  multiq: \"a\\nb\\n\"\n",
	"# doc head\n\n# head of a\na: 1 # line of a\n# foot of a\n\nb:\n  # head of b.x\n  x: 2 # line x\n  y: 3\n  # foot of b.y (indented)\n# head of c\nc:\n  - one # line one\n  # head of two\n  - two\n  - k: v # line kv\n    j: w\n# trailing foot\n",
	"l:\n- a\n- - x\n  - y\n- {k: v}\n- &s scalar\n- *s\nm: |\n  line one\n  line two\nn: >-\n  folded\nbin: !!binary aGVsbG8=\nnull_forms: [~, null, Null]\nflt: 1.5\nflag: true\n",
}

type neatVector struct {
	In      string `json:"in"`
	Palette string `json:"palette"`
	Mode    string `json:"mode"`
	Out     string `json:"out"`
}

func neatVectors() ([]neatVector, error) {
	var result []neatVector
	for _, in := range neatInputs {
		for _, m := range renderModes {
			m.set()
			for _, palette := range []string{"greenish", "reddish", "plain"} {
				var doc yaml.Node
				if err := yaml.Unmarshal([]byte(in), &doc); err != nil {
					return nil, err
				}
				node := doc.Content[0]
				var s string
				var err error
				switch palette {
				case "greenish":
					s, err = (&dyff.BuntColorizer{}).YAMLInGreenishColors(node, false)
				case "reddish":
					s, err = (&dyff.BuntColorizer{}).YAMLInRedishColors(node, false)
				default:
					s, err = neat.NewOutputProcessorWithDefaults().UseIndentLines(false).BoldKeys(true).ToYAML(node)
				}
				if err != nil {
					return nil, err
				}
				result = append(result, neatVector{In: in, Palette: palette, Mode: m.name, Out: s})
			}
		}
	}
	return result, nil
}
