package yaml_test

import (
	"testing"

	goccy "github.com/goccy/go-yaml"
)

// Once goccy decodes an ,inline struct, it zeroes each field of that
// struct whose key a non-inline field of the struct that inlines it
// reads, and an ,inline field's key is its tag name or its Go name in
// lower case. So a non-inline field can zero a whole ,inline struct, and
// a hook below it must not run, or its result lands where goccy wiped.
// Each hooked shape below has a plain twin with string for the hook,
// and goccy's decode of the twin is the oracle.

type ZDeep struct {
	H dupHook `yaml:"h"`
}

type ZPlainDeep struct {
	H string `yaml:"h"`
}

type ZMid struct {
	ZDeep `yaml:"zdeep,inline"`
}

type ZPlainMid struct {
	ZPlainDeep `yaml:"zdeep,inline"`
}

// zOuter's field zdeep zeroes ZMid's ,inline ZDeep.
type zOuter struct {
	D    string `yaml:"zdeep"`
	ZMid `yaml:",inline"`
}

type zPlainOuter struct {
	D         string `yaml:"zdeep"`
	ZPlainMid `yaml:",inline"`
}

type ZMidPointer struct {
	*ZDeep `yaml:"zdeep,inline"`
}

type ZPlainMidPointer struct {
	*ZPlainDeep `yaml:"zdeep,inline"`
}

// zOuterPointer zeroes the pointer, so goccy leaves it nil.
type zOuterPointer struct {
	D           string `yaml:"zdeep"`
	ZMidPointer `yaml:",inline"`
}

type zPlainOuterPointer struct {
	D                string `yaml:"zdeep"`
	ZPlainMidPointer `yaml:",inline"`
}

type ZMidTagged struct {
	ZDeep `yaml:"foo,inline"`
}

type ZPlainMidTagged struct {
	ZPlainDeep `yaml:"foo,inline"`
}

// zOuterTagged zeroes the struct inlined under the tag name foo.
type zOuterTagged struct {
	F          int `yaml:"foo"`
	ZMidTagged `yaml:",inline"`
}

type zPlainOuterTagged struct {
	F               int `yaml:"foo"`
	ZPlainMidTagged `yaml:",inline"`
}

type ZMid2 struct {
	ZMid `yaml:",inline"`
}

type ZPlainMid2 struct {
	ZPlainMid `yaml:"zmid,inline"`
}

// zOuter2 zeroes ZMid two ,inline levels above the hook.
type zOuter2 struct {
	M     string `yaml:"zmid"`
	ZMid2 `yaml:",inline"`
}

type zPlainOuter2 struct {
	M          string `yaml:"zmid"`
	ZPlainMid2 `yaml:",inline"`
}

// ZTwin reads h as ZDeep does, inlined under the key ztwin.
type ZTwin struct {
	H dupHook `yaml:"h"`
}

type ZMidTwins struct {
	ZDeep `yaml:"zdeep,inline"`
	ZTwin `yaml:"ztwin,inline"`
}

type ZPlainMidTwins struct {
	ZPlainDeep `yaml:"zdeep,inline"`
	ZPlainTwin `yaml:"ztwin,inline"`
}

type ZPlainTwin struct {
	H string `yaml:"h"`
}

// zOuterTwins zeroes both structs that read h, so no field keeps it.
type zOuterTwins struct {
	D         string `yaml:"zdeep"`
	T         string `yaml:"ztwin"`
	ZMidTwins `yaml:",inline"`
}

type zPlainOuterTwins struct {
	D              string `yaml:"zdeep"`
	T              string `yaml:"ztwin"`
	ZPlainMidTwins `yaml:",inline"`
}

// oracle decodes src into a new plain T with goccy.
func oracle[T any](t *testing.T, src string) T {
	t.Helper()
	var got T
	if err := goccy.Unmarshal([]byte(src), &got); err != nil {
		t.Fatalf("goccy: %v", err)
	}
	return got
}

func TestHookInAStructGoccyZeroesNeverRuns(t *testing.T) {
	t.Parallel()
	for _, src := range []string{"h: x\n", "h: x\nzdeep: y\n"} {
		if got := oracle[zPlainOuter](t, src); got.H != "" {
			t.Fatalf("goccy kept h: %+v", got)
		}
		want := zOuter{D: oracle[zPlainOuter](t, src).D}
		wantDecoded(t, src, want)
	}
}

func TestHookInAPointerGoccyZeroesNeverRuns(t *testing.T) {
	t.Parallel()
	if got := oracle[zPlainOuterPointer](t, "h: x\n"); got.ZPlainDeep != nil {
		t.Fatalf("goccy kept the pointer: %+v", got)
	}
	wantDecoded(t, "h: x\n", zOuterPointer{})
}

func TestHookInATagNamedStructGoccyZeroesNeverRuns(t *testing.T) {
	t.Parallel()
	if got := oracle[zPlainOuterTagged](t, "h: x\n"); got.H != "" {
		t.Fatalf("goccy kept h: %+v", got)
	}
	wantDecoded(t, "h: x\n", zOuterTagged{})
}

func TestHookTwoLevelsBelowAZeroedStructNeverRuns(t *testing.T) {
	t.Parallel()
	if got := oracle[zPlainOuter2](t, "h: x\n"); got.H != "" {
		t.Fatalf("goccy kept h: %+v", got)
	}
	wantDecoded(t, "h: x\n", zOuter2{})
}

func TestHookKeyWhoseReadersAreAllZeroedNeverRuns(t *testing.T) {
	t.Parallel()
	if got := oracle[zPlainOuterTwins](t, "h: x\n"); got.ZPlainDeep.H != "" || got.ZPlainTwin.H != "" {
		t.Fatalf("goccy kept h: %+v", got)
	}
	wantDecoded(t, "h: x\n", zOuterTwins{})
}
