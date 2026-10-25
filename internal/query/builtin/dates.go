package builtin

import (
	"math"
	"time"

	"github.com/fivetwenty-io/graft/internal/query/runtime"
	"github.com/fivetwenty-io/graft/pkg/decimal"
	"github.com/fivetwenty-io/graft/pkg/query/value"
)

// nowFunc is the clock behind now, which a test swaps out.
var nowFunc = time.Now

// maxYearOffset is how far from 1900 a year may be before the conversion
// fails, which is the range of a C int in a broken-down time.
const maxYearOffset = math.MaxInt32

func registerDates(r *runtime.Registry) {
	r.RegisterSingle("now", 0, func(_ *runtime.Ctx, _ *value.Value, _ []runtime.Thunk, emit runtime.Emit) error {
		return emit(value.Num(float64(nowFunc().UnixNano())/1e9), nil)
	})
	unary(r, "gmtime", func(ctx *runtime.Ctx, in *value.Value) (*value.Value, error) {
		return brokenDown(ctx, in, "gmtime()", fieldsAt)
	})
	unary(r, "localtime", func(ctx *runtime.Ctx, in *value.Value) (*value.Value, error) {
		return brokenDown(ctx, in, "localtime()", localFields)
	})
	unary(r, "mktime", mktime)
	binary(r, "strftime", func(ctx *runtime.Ctx, in, layout *value.Value) (*value.Value, error) {
		return format(ctx, in, layout, "strftime/1", false)
	})
	binary(r, "strflocaltime", func(ctx *runtime.Ctx, in, layout *value.Value) (*value.Value, error) {
		return format(ctx, in, layout, "strflocaltime/1", true)
	})
	binary(r, "strptime", func(_ *runtime.Ctx, in, layout *value.Value) (*value.Value, error) {
		if in.Kind() != value.KindString || layout.Kind() != value.KindString {
			return nil, fail("strptime/1 requires string inputs and arguments")
		}
		bt, rest, err := strptime(layout.Str(), in.Str())
		if err != nil {
			return nil, fail("date %q does not match format %q", in.Str(), layout.Str())
		}
		elems := make([]*value.Value, 0, 9)
		for _, f := range bt {
			elems = append(elems, value.Num(f))
		}
		if rest != "" {
			elems = append(elems, value.Str(rest))
		}
		return value.Arr(elems...), nil
	})
}

// localFields is the broken-down local time of a number of seconds.
func localFields(sec int64) brokenTime {
	t := time.Unix(sec, 0).In(time.Local)
	return brokenTime{
		float64(t.Year()), float64(t.Month() - 1), float64(t.Day()),
		float64(t.Hour()), float64(t.Minute()), float64(t.Second()),
		float64(t.Weekday()), float64(t.YearDay() - 1),
	}
}

// epochSeconds splits a number of seconds since the epoch into whole
// seconds, truncated toward zero, and a fraction measured up from the
// floor, which is zero for a whole number. jq adds the two even when they
// disagree for a negative number, and so do we. The fraction is exact
// unless --float-numbers asked for doubles.
func epochSeconds(ctx *runtime.Ctx, in *value.Value) (int64, decimal.Decimal, error) {
	bad := fail("error converting number of seconds since epoch to datetime")
	d, err := ctx.Number(in)
	if err != nil {
		return 0, decimal.Decimal{}, err
	}
	if d.IsNaN() {
		return 0, decimal.Decimal{}, bad
	}
	if inf, _ := d.IsInf(); inf {
		return 0, decimal.Decimal{}, bad
	}
	if ctx.Numbers == runtime.NumbersFloat {
		f := d.Float64()
		if math.Abs(f) > 1<<62 {
			return 0, decimal.Decimal{}, bad
		}
		return int64(f), decimal.FromFloat64(f - math.Floor(f)), nil
	}
	sec, ok := d.Trunc().Int64()
	if !ok {
		return 0, decimal.Decimal{}, bad
	}
	frac, err := d.Sub(d.Floor())
	if err != nil {
		return 0, decimal.Decimal{}, bad
	}
	return sec, frac, nil
}

// inRange reports whether the year of a number of seconds fits a
// broken-down time.
func inRange(sec int64) bool {
	y := fieldsAt(sec)[btYear]
	return math.Abs(y-1900) <= maxYearOffset
}

// brokenDown is gmtime and localtime: the array for a number of seconds,
// with the fraction of a second kept in the seconds element.
func brokenDown(ctx *runtime.Ctx, in *value.Value, what string, fields func(int64) brokenTime) (*value.Value, error) {
	if in.Kind() != value.KindNumber {
		return nil, fail("%s requires numeric inputs", what)
	}
	sec, frac, err := epochSeconds(ctx, in)
	if err != nil {
		return nil, err
	}
	if !inRange(sec) {
		return nil, fail("error converting number of seconds since epoch to datetime")
	}
	bt := fields(sec)
	elems := make([]*value.Value, len(bt))
	for i, f := range bt {
		elems[i] = value.Num(f)
	}
	if !frac.IsZero() {
		whole := decimal.FromInt64(int64(bt[btSec]))
		if ctx.Numbers == runtime.NumbersFloat {
			elems[btSec] = value.Num(bt[btSec] + frac.Float64())
		} else if total, err := whole.Add(frac); err == nil {
			elems[btSec] = value.NumDec(total)
		}
	}
	return value.Arr(elems...), nil
}

// parsedTime reads an array of numbers as a broken-down time. Elements
// past the eighth are ignored and missing ones are zero.
func parsedTime(in *value.Value) (brokenTime, bool) {
	var bt brokenTime
	if in.Kind() != value.KindArray {
		return bt, false
	}
	for i, e := range in.Elems() {
		if e.Kind() != value.KindNumber {
			return bt, false
		}
		if i < len(bt) {
			bt[i] = toCInt(e.Float())
		}
	}
	return bt, true
}

// toCInt truncates a double the way a cast to a 32-bit int does on the
// machines jq's behavior was pinned on, saturating at the ends.
func toCInt(f float64) float64 {
	switch {
	case math.IsNaN(f):
		return 0
	case f >= math.MaxInt32:
		return math.MaxInt32
	case f <= math.MinInt32:
		return math.MinInt32
	}
	return math.Trunc(f)
}

// firstEpoch is 1900-01-01, before which timegm fails.
const firstEpoch = -2208988800

func mktime(_ *runtime.Ctx, in *value.Value) (*value.Value, error) {
	if in.Kind() != value.KindArray {
		return nil, fail("mktime requires array inputs")
	}
	bt, ok := parsedTime(in)
	if !ok {
		return nil, fail("mktime requires parsed datetime inputs")
	}
	if in.Len() == 0 {
		return nil, fail("invalid gmtime representation")
	}
	epoch := epochOf(bt)
	if epoch < firstEpoch {
		return nil, fail("invalid gmtime representation")
	}
	return value.NumDec(decimal.FromInt64(epoch)), nil
}

// format is strftime and strflocaltime. A number is first turned into a
// broken-down time, in the local zone for the second.
func format(ctx *runtime.Ctx, in, layout *value.Value, what string, local bool) (*value.Value, error) {
	var bt brokenTime
	var z zoneInfo
	switch in.Kind() {
	case value.KindNumber:
		sec, _, err := epochSeconds(ctx, in)
		if err != nil {
			return nil, err
		}
		if !inRange(sec) {
			return nil, fail("error converting number of seconds since epoch to datetime")
		}
		if local {
			bt = localFields(sec)
			name, off := time.Unix(sec, 0).In(time.Local).Zone()
			z = zoneInfo{name: name, offset: off, epoch: sec}
		} else {
			bt = fieldsAt(sec)
			z = zoneInfo{name: utc, epoch: sec}
		}
	case value.KindArray:
		var ok bool
		if bt, ok = parsedTime(in); !ok {
			return nil, fail("%s requires parsed datetime inputs", what)
		}
		if in.Len() == 0 {
			bt[btYear] = 1900
		}
		bt = bt.withCalendarDays()
		z = zoneInfo{name: utc, epoch: epochOf(bt)}
		if local {
			t := time.Date(int(bt[btYear]), time.Month(bt[btMon]+1), int(bt[btMday]),
				int(bt[btHour]), int(bt[btMin]), int(bt[btSec]), 0, time.Local)
			name, off := t.Zone()
			z = zoneInfo{name: name, offset: off, epoch: t.Unix()}
		}
	default:
		return nil, fail("%s requires parsed datetime inputs", what)
	}
	if layout.Kind() != value.KindString {
		return nil, fail("%s requires a string format", what)
	}
	var s string
	var err error
	if local {
		s, err = formatTime(layout.Str(), bt, z)
	} else {
		s, err = strftime(layout.Str(), bt)
	}
	if err != nil {
		return nil, err
	}
	return value.Str(s), nil
}
