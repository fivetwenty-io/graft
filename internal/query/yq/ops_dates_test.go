package yq

import (
	"sync"
	"testing"
	"time"

	"github.com/fivetwenty-io/graft/internal/query/load"
	"github.com/fivetwenty-io/graft/pkg/query/value"
)

// inLocal runs the rest of the test with t as the local time zone, which
// from_unix, now, and tz("Local") read.
func inLocal(t *testing.T, zone string) {
	t.Helper()
	loc, err := time.LoadLocation(zone)
	if err != nil {
		t.Fatal(err)
	}
	old := time.Local
	time.Local = loc
	t.Cleanup(func() { time.Local = old })
}

func TestYQDatesAndMisc(t *testing.T) {
	inLocal(t, "UTC")
	wantYQ(t, `with_dtf("2006-01-02"; . + "24h")`, `"2021-01-01"`, "2021-01-02\n")
	wantYQ(t, `format_datetime("Monday, 02-Jan-06")`, `"2021-01-01T00:00:00Z"`, "Friday, 01-Jan-21\n")
	wantYQ(t, `tz("Australia/Sydney")`, `"2021-01-01T00:00:00Z"`, "2021-01-01T11:00:00+11:00\n")
	wantYQ(t, `from_unix, (from_unix | to_unix)`, `1609459200`, "2021-01-01T00:00:00Z\n1609459200\n")
}

func TestYQTz(t *testing.T) {
	inLocal(t, "America/New_York")
	in := "a: 2021-01-01T00:00:00Z\n"
	wantYQ(t, `.a | tz("Australia/Sydney")`, in, "2021-01-01T11:00:00+11:00\n")
	wantYQ(t, `.a | tz("America/New_York")`, "a: 2021-01-01T00:00:00+05:00\n", "2020-12-31T14:00:00-05:00\n")
	wantYQ(t, `.a | tz("UTC")`, "a: 2021-01-01T00:00:00+05:00\n", "2020-12-31T19:00:00Z\n")
	wantYQ(t, `.a | tz("")`, in, "2021-01-01T00:00:00Z\n")
	wantYQ(t, `.a | tz("Local")`, in, "2020-12-31T19:00:00-05:00\n")
	wantYQ(t, `.a | tz("Asia/Kolkata")`, "a: 2021-01-01\n", "2021-01-01T05:30:00+05:30\n")
	wantYQ(t, `.a | tz("Asia/Kolkata")`, "a: \"2021-01-01T00:00:00Z\"\n", "2021-01-01T05:30:00+05:30\n")
	wantYQ(t, `.a | tz("Asia/Kolkata") | tag`, in, "!!timestamp\n")
	wantYQ(t, `.a |= tz("Asia/Kolkata")`, "a: &x 2021-03-04T05:06:07Z # hi\n", "a: &x 2021-03-04T10:36:07+05:30 # hi\n")
	wantYQ(t, `.a | tz("UTC", "Asia/Kolkata")`, in, "2021-01-01T00:00:00Z\n")
}

func TestYQTzErrors(t *testing.T) {
	inLocal(t, "UTC")
	layout := "[2006-01-02T15:04:05Z07:00]"
	wantYQError(t, `.a | tz("Foo/Bar")`, "a: 2021-01-01T00:00:00Z\n", "could not load tz [Foo/Bar]: unknown time zone Foo/Bar")
	wantYQError(t, `.a | tz("UTC")`, "a: not a date\n", `could not parse datetime of [a] using layout `+layout+`: parsing time "not a date" as "2006-01-02": cannot parse "not a date" as "2006"`)
	wantYQError(t, `.a | tz("UTC")`, "a: 5\n", `could not parse datetime of [a] using layout `+layout+`: parsing time "5" as "2006-01-02": cannot parse "5" as "2006"`)
	wantYQError(t, `.a | tz("UTC")`, "a: [1]\n", `could not parse datetime of [a] using layout `+layout+`: parsing time "" as "2006-01-02": cannot parse "" as "2006"`)
	wantYQError(t, `tz("UTC")`, "foo\n", `could not parse datetime of [] using layout `+layout+`: parsing time "foo" as "2006-01-02": cannot parse "foo" as "2006"`)
	wantYQError(t, `.a | tz(.b)`, "a: 2021-01-01T00:00:00Z\n", "could not find timezone for format_time")
}

func TestYQFormatDatetime(t *testing.T) {
	inLocal(t, "UTC")
	in := "a: 2021-03-04T05:06:07Z\n"
	wantYQ(t, `.a | format_datetime("Monday")`, in, "Thursday\n")
	wantYQ(t, `.a | format_datetime("15:04")`, in, "05:06\n")
	wantYQ(t, `.a | format_datetime("Jan 2")`, "a: \"2021-01-01\"\n", "Jan 1\n")
	wantYQ(t, `.a | format_datetime(5)`, in, "7\n")
	wantYQ(t, `.a | format_datetime("2006", "Jan")`, in, "2021\n")
	// The result is the type its text reads as.
	wantYQ(t, `.a | format_datetime("2006") | tag`, in, "!!int\n")
	wantYQ(t, `.a | format_datetime("2006-01-02") | tag`, in, "!!timestamp\n")
	wantYQ(t, `.a | format_datetime("Jan") | tag`, in, "!!str\n")
	wantYQ(t, `.a | format_datetime("1.5") | tag`, in, "!!float\n")
	wantYQ(t, `.a | format_datetime("true") | tag`, in, "!!bool\n")
	wantYQ(t, `.c = (.a | format_datetime("2006"))`, in, "a: 2021-03-04T05:06:07Z\nc: 2021\n")
	wantYQ(t, `.c = (.a | format_datetime("2006-01-02"))`, in, "a: 2021-03-04T05:06:07Z\nc: 2021-03-04\n")
	wantYQ(t, `.c = (.a | format_datetime("Jan"))`, in, "a: 2021-03-04T05:06:07Z\nc: Mar\n")
	wantYQ(t, `.c = (.a | format_datetime("1")) | .c + 1`, in, "4\n")
	wantYQ(t, `.a |= format_datetime("2006")`, "a: &x 2021-03-04T05:06:07Z # hi\n", "a: &x 2021 # hi\n")
}

func TestYQFormatDatetimeErrors(t *testing.T) {
	inLocal(t, "UTC")
	wantYQError(t, `.a | format_datetime("2006")`, "a: foo\n", `could not parse datetime of [a]: parsing time "foo" as "2006-01-02": cannot parse "foo" as "2006"`)
	wantYQError(t, `.a | format_datetime("2006")`, "a: 5\n", `could not parse datetime of [a]: parsing time "5" as "2006-01-02": cannot parse "5" as "2006"`)
	wantYQError(t, `.a | format_datetime("2006")`, "a: [1]\n", `could not parse datetime of [a]: parsing time "" as "2006-01-02": cannot parse "" as "2006"`)
	wantYQError(t, `.a | format_datetime("2006")`, "a: 2021-01-01T00:00:00\n", `could not parse datetime of [a]: parsing time "2021-01-01T00:00:00": extra text: "T00:00:00"`)
	wantYQError(t, `.a | format_datetime("2006")`, "a: 2021-1-1\n", `could not parse datetime of [a]: parsing time "2021-1-1" as "2006-01-02": cannot parse "1-1" as "01"`)
	wantYQError(t, `.a | format_datetime(.b)`, "a: 2021-01-01T00:00:00Z\n", "could not find format for format_time")
}

func TestYQFromUnix(t *testing.T) {
	inLocal(t, "UTC")
	wantYQ(t, `.a | from_unix`, "a: 0\n", "1970-01-01T00:00:00Z\n")
	wantYQ(t, `.a | from_unix`, "a: -1\n", "1969-12-31T23:59:59Z\n")
	wantYQ(t, `.a | from_unix`, "a: 1609459200.5\n", "2021-01-01T00:00:00Z\n")
	wantYQ(t, `.a | from_unix`, "a: 1e9\n", "2001-09-09T01:46:40Z\n")
	wantYQ(t, `.a | from_unix | tag`, "a: 1\n", "!!timestamp\n")
	wantYQ(t, `.a[] | from_unix`, "a: [1609459200, 0]\n", "2021-01-01T00:00:00Z\n1970-01-01T00:00:00Z\n")
	wantYQ(t, `.a | from_unix | to_unix`, "a: 1.9\n", "1\n")
	wantYQ(t, `.b = (.a | from_unix)`, "a: &x 1609459200\n", "a: &x 1609459200\nb: 2021-01-01T00:00:00Z\n")
	wantYQ(t, `.a |= from_unix`, "a: &x 1609459200 # hi\n", "a: &x 2021-01-01T00:00:00Z # hi\n")
	wantYQ(t, `.a.b | from_unix`, "a: 1\n", "")
	wantYQError(t, `.a | from_unix`, "a: \"1609459200\"\n", "from_unix only works on numbers, found !!str instead")
	wantYQError(t, `.a | from_unix`, "a: foo\n", "from_unix only works on numbers, found !!str instead")
	wantYQError(t, `.a | from_unix`, "a:\n", "from_unix only works on numbers, found !!null instead")
	wantYQError(t, `.a | from_unix`, "a: [1]\n", "from_unix only works on numbers, found !!seq instead")
	wantYQError(t, `.a | from_unix`, "a: {b: 1}\n", "from_unix only works on numbers, found !!map instead")
	wantYQError(t, `.a | from_unix`, "a: true\n", "from_unix only works on numbers, found !!bool instead")
	wantYQError(t, `.a | from_unix`, "a: 0x10\n", `strconv.ParseFloat: parsing "0x10": invalid syntax`)
	wantYQError(t, `.a | from_unix`, "a: .inf\n", `strconv.ParseFloat: parsing ".inf": invalid syntax`)
}

func TestYQFromUnixReadsTheLocalZone(t *testing.T) {
	inLocal(t, "America/New_York")
	wantYQ(t, `.a | from_unix`, "a: 1609459200\n", "2020-12-31T19:00:00-05:00\n")
}

func TestYQToUnix(t *testing.T) {
	inLocal(t, "UTC")
	wantYQ(t, `.a | to_unix`, "a: 2021-01-01T00:00:00Z\n", "1609459200\n")
	wantYQ(t, `.a | to_unix`, "a: \"2021-01-01T00:00:00Z\"\n", "1609459200\n")
	wantYQ(t, `.a | to_unix`, "a: 2021-01-01\n", "1609459200\n")
	wantYQ(t, `.a | to_unix`, "a: 2021-01-01T00:00:00.9Z\n", "1609459200\n")
	wantYQ(t, `.a | to_unix`, "a: 2021-03-04T05:06:07+05:30\n", "1614814567\n")
	wantYQ(t, `.a | to_unix | tag`, "a: 2021-01-01T00:00:00Z\n", "!!int\n")
	wantYQ(t, `.a |= to_unix`, "a: &x 2021-03-04T05:06:07Z # hi\n", "a: &x 1614834367 # hi\n")
	wantYQ(t, `.b = (.a | to_unix)`, "a: &x 2021-01-01T00:00:00Z\n", "a: &x 2021-01-01T00:00:00Z\nb: 1609459200\n")
	wantYQ(t, `.a.b | to_unix`, "a: 1\n", "")
	layout := "[2006-01-02T15:04:05Z07:00]"
	wantYQError(t, `.a | to_unix`, "a: 5\n", `could not parse datetime of [a] using layout `+layout+`: parsing time "5" as "2006-01-02": cannot parse "5" as "2006"`)
	wantYQError(t, `.a | to_unix`, "a: foo\n", `could not parse datetime of [a] using layout `+layout+`: parsing time "foo" as "2006-01-02": cannot parse "foo" as "2006"`)
	wantYQError(t, `.a | to_unix`, "a:\n", `could not parse datetime of [a] using layout `+layout+`: parsing time "" as "2006-01-02": cannot parse "" as "2006"`)
}

func TestYQNow(t *testing.T) {
	inLocal(t, "UTC")
	wantYQ(t, `now | tag`, "a: 1\n", "!!timestamp\n")
	wantYQ(t, `now | to_unix | . > 1700000000`, "a: 1\n", "true\n")
	wantYQ(t, `now | length`, "a: 1\n", "20\n")
	wantYQ(t, `now | tz("Asia/Kolkata") | to_unix | . > 1700000000`, "a: 1\n", "true\n")
}

func TestYQWithDtf(t *testing.T) {
	inLocal(t, "UTC")
	wantYQ(t, `with_dtf("2006-01-02"; .a - "24h")`, "a: \"2021-01-01\"\n", "2020-12-31\n")
	wantYQ(t, `with_dtf("2006-01-02"; (. + "24h") | tag)`, "\"2021-01-01\"\n", "!!str\n")
	wantYQ(t, `with_dtf("2006-01-02"; .a |= . + "24h")`, "a: \"2021-01-01\"\n", "a: \"2021-01-02\"\n")
	wantYQ(t, `with_dtf("2006-01-02"; .a |= . + "24h")`, "a: 2021-01-01\n", "a: 2021-01-02\n")
	wantYQ(t, `with_dtf("2006-01-02"; .a | tz("Asia/Kolkata"))`, "a: \"2021-01-01\"\n", "2021-01-01\n")
	wantYQ(t, `with_dtf("2006-01-02"; .a | to_unix)`, "a: \"2021-01-01\"\n", "1609459200\n")
	// now and from_unix write the default layout whatever with_dtf sets.
	wantYQ(t, `with_dtf("2006-01-02"; .a | from_unix)`, "a: 1609459200\n", "2021-01-01T00:00:00Z\n")
	wantYQ(t, `with_dtf("2006-01-02"; now | length)`, "a: 1\n", "20\n")
	wantYQ(t, `with_dtf("Jan 2 06"; .a | format_datetime("2006-01-02"))`, "a: \"Jan 3 21\"\n", "2021-01-03\n")
	wantYQ(t, `with_dtf("Jan 2 2006"; .a | tz("UTC"))`, "a: \"Mar 4 2021\"\n", "Mar 4 2021\n")
	wantYQ(t, `with_dtf("2006-01-02"; .a, .b)`, "a: 2021-01-05\nb: 2\n", "2021-01-05\n2\n")
	wantYQ(t, `with_dtf("2006-01-02"; .a + "24h" + "24h")`, "a: \"2021-01-05\"\n", "2021-01-07\n")
	wantYQ(t, `with_dtf("2006-01-02"; (.a, .b) + "24h")`, "a: \"2021-01-05\"\nb: \"2021-02-05\"\n", "2021-01-06\n2021-02-06\n")
	wantYQ(t, `with_dtf("2006-01-02"; .a + .b)`, "a: \"2021-01-05\"\nb: 24h\n", "2021-01-06\n")
	wantYQ(t, `with_dtf("2006-01-02"; .a + "24h" | with_dtf("2006"; .))`, "a: \"2021-01-05\"\n", "2021-01-06\n")
	wantYQ(t, `with_dtf(5; .a)`, "a: \"2021-01-05\"\n", "2021-01-05\n")
	// The layout ends with the block, so what follows reads the default.
	wantYQ(t, `with_dtf("2006-01-02"; .a) | . + "24h"`, "a: \"2021-01-05\"\n", "2021-01-0524h\n")
	wantYQ(t, `(with_dtf("2006-01-02"; .a) | tz("UTC")), (.a | tz("UTC"))`, "a: 2021-01-05T00:00:00Z\n", "2021-01-05T00:00:00Z\n2021-01-05T00:00:00Z\n")
	// A time that does not read in the layout is a string.
	wantYQ(t, `with_dtf("2006-01-02"; .a + "24h")`, "a: \"2021-01-05T10:00:00Z\"\n", "2021-01-05T10:00:00Z24h\n")
	wantYQ(t, `with_dtf("2006-01-02"; .a + "24h")`, "a: foo\n", "foo24h\n")
}

func TestYQWithDtfErrors(t *testing.T) {
	inLocal(t, "UTC")
	wantYQError(t, `with_dtf("2006-01-02")`, "a: 1\n", `must provide a date time format string and an expression, e.g. with_dtf("Monday, 02-Jan-06 at 3:04PM MST"; <exp>)`)
	wantYQError(t, `with_dtf("x"; .a + "1h")`, "a: 2021-01-05T10:00:00Z\n", `parsing time "2021-01-05T10:00:00Z" as "x": cannot parse "2021-01-05T10:00:00Z" as "x"`)
	wantYQError(t, `with_dtf("2006-01-02"; .a + "24h")`, "a: 2021-01-05T10:00:00Z\n", `parsing time "2021-01-05T10:00:00Z": extra text: "T10:00:00Z"`)
	wantYQError(t, `.a | with_dtf(.z; .)`, "a: 2021-01-05T10:00:00Z\n", "could not get date time format: could not find layout for format_time")
}

func TestYQDateArithmetic(t *testing.T) {
	inLocal(t, "UTC")
	wantYQ(t, `.a + "24h"`, "a: 2021-01-01T00:00:00Z\n", "2021-01-02T00:00:00Z\n")
	wantYQ(t, `.a - "24h"`, "a: 2021-01-01T00:00:00Z\n", "2020-12-31T00:00:00Z\n")
	wantYQ(t, `.a + "1h30m"`, "a: 2021-01-01T00:00:00+05:00\n", "2021-01-01T01:30:00+05:00\n")
	wantYQ(t, `.a + "24h"`, "a: 2021-01-01\n", "2021-01-02T00:00:00Z\n")
	wantYQ(t, `.a + "24h" | tag`, "a: 2021-01-01T00:00:00Z\n", "!!timestamp\n")
	wantYQ(t, `.a += "24h"`, "a: &x 2021-01-01T00:00:00Z # hi\n", "a: &x 2021-01-02T00:00:00Z # hi\n")
	wantYQ(t, `.a + .b`, "a: 2021-01-01T00:00:00Z\nb: 24h\n", "2021-01-02T00:00:00Z\n")
	// Text that is no timestamp joins as it always did.
	wantYQ(t, `.a + "24h"`, "a: \"2021-01-01T00:00:00Z\"\n", "2021-01-01T00:00:00Z24h\n")
	wantYQ(t, `.a + "24h"`, "a: foo\n", "foo24h\n")
	wantYQError(t, `.a + "bogus"`, "a: 2021-01-01T00:00:00Z\n", `unable to parse duration [bogus]: time: invalid duration "bogus"`)
	wantYQError(t, `.a + 5`, "a: 2021-01-01T00:00:00Z\n", `unable to parse duration [5]: time: missing unit in duration "5"`)
	wantYQError(t, `.a - .b`, "a: 2021-01-02T00:00:00Z\nb: 2021-01-01T00:00:00Z\n", `unable to parse duration [2021-01-01T00:00:00Z]: time: unknown unit "-" in duration "-2021-01-01T00:00:00Z"`)
}

func TestYQTagOfTimestamps(t *testing.T) {
	inLocal(t, "UTC")
	wantYQ(t, `.t | tag`, "t: 2021-01-01T00:00:00Z\n", "!!timestamp\n")
	wantYQ(t, `.t | tag`, "t: 2021-01-01\n", "!!timestamp\n")
	wantYQ(t, `.t | type`, "t: 2021-01-01T00:00:00Z\n", "!!timestamp\n")
	wantYQ(t, `.t | tag`, "t: \"2021-01-01\"\n", "!!str\n")
	wantYQ(t, `.t | tag`, "t: !!timestamp 2021-01-01\n", "!!timestamp\n")
	wantYQ(t, `.t | tz("UTC") | tag`, "t: 2021-01-01T00:00:00Z\n", "!!timestamp\n")
	wantYQ(t, `"2021-01-01" | tag`, "t: 1\n", "!!str\n")
}

func TestYQFromUnixRange(t *testing.T) {
	inLocal(t, "UTC")
	wantYQ(t, `.a | from_unix`, "a: 253402300800\n", "10000-01-01T00:00:00Z\n")
	wantYQError(t, `.a | from_unix`, "a: 1e30\n", "from_unix: 1e30 is outside the range of int64 seconds")
	wantYQError(t, `.a | from_unix`, "a: -1e30\n", "from_unix: -1e30 is outside the range of int64 seconds")
	wantYQError(t, `.a | from_unix`, "a: 1e19\n", "from_unix: 1e19 is outside the range of int64 seconds")
	wantYQError(t, `.a | from_unix`, "a: .nan\n", `strconv.ParseFloat: parsing ".nan": invalid syntax`)
	wantYQError(t, `.a | from_unix`, "a: .inf\n", `strconv.ParseFloat: parsing ".inf": invalid syntax`)
}

func TestYQWithDtfConcurrentRuns(t *testing.T) {
	d := New(testOpts)
	var wg sync.WaitGroup
	for i := range 8 {
		layout, in, want := "2006-01-02", `"2021-01-01"`, "2021-01-02"
		if i%2 == 1 {
			layout, in, want = "Jan 2 2006", `"Jan 1 2021"`, "Jan 2 2021"
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 50 {
				var got string
				err := Eval(d, `with_dtf("`+layout+`"; . + "24h")`, mustLoad(t, in)[0], func(_ load.Document, v *value.Value) error {
					got = v.Str()
					return nil
				})
				if err != nil || got != want {
					t.Errorf("layout %q: got %q, %v, want %q", layout, got, err, want)
					return
				}
			}
		}()
	}
	wg.Wait()
}
