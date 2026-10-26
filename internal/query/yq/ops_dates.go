package yq

import (
	"errors"
	"strconv"
	"sync"
	"time"
	_ "time/tzdata" // zone names resolve on a host with no zoneinfo

	"github.com/fivetwenty-io/graft/internal/query/runtime"
	"github.com/fivetwenty-io/graft/internal/yamlnode"
	"github.com/fivetwenty-io/graft/pkg/decimal"
	"github.com/fivetwenty-io/graft/pkg/query"
	"github.com/fivetwenty-io/graft/pkg/query/value"
)

// timestampTag is the tag of a time the date operators write.
const timestampTag = "!!timestamp"

// dateOnly is the layout parseDateTime tries when a time does not read in
// the layout in force.
const dateOnly = "2006-01-02"

// errFirst ends an argument at its first output.
var errFirst = errors.New("first output")

// layouts holds the layout with_dtf sets for the run it is called in,
// keyed by the run's context. A run with no entry reads time.RFC3339.
var layouts sync.Map

// layoutOf is the date time layout in force for the run of ctx.
func layoutOf(ctx *runtime.Ctx) string {
	if l, ok := layouts.Load(ctx); ok {
		if s, ok := l.(string); ok {
			return s
		}
	}
	return time.RFC3339
}

// registerDates adds yq's date and time operators: now, tz,
// format_datetime, with_dtf, from_unix, and to_unix.
func registerDates(r *runtime.Registry) {
	r.RegisterSingle("now", 0, func(_ *runtime.Ctx, _ *value.Value, _ []runtime.Thunk, emit runtime.Emit) error {
		return emit(timestamp(time.Now().Format(time.RFC3339)), nil)
	})
	r.RegisterSingle("tz", 1, tz)
	r.RegisterSingle("format_datetime", 1, formatDatetime)
	r.RegisterSingle("from_unix", 0, fromUnix)
	r.RegisterSingle("to_unix", 0, toUnix)
	r.Register("with_dtf", 2, withDtf)
	r.Register("with_dtf", 1, func(*runtime.Ctx, *value.Value, []runtime.Thunk, runtime.Emit) error {
		return &query.RuntimeError{Msg: `must provide a date time format string and an expression, e.g. with_dtf("Monday, 02-Jan-06 at 3:04PM MST"; <exp>)`}
	})
}

// timestamp is the text of a time as a node tagged !!timestamp.
func timestamp(text string) *value.Value {
	return value.Str(text).WithMeta(&value.Meta{Tag: timestampTag, Text: text})
}

// timeNode is the text of a formatted time as the scalar it reads as,
// which is a timestamp only when the layout wrote one.
func timeNode(text string) *value.Value {
	v, err := scalarOf(text)
	switch {
	case err != nil || (!scalarKind(v)):
		return value.Str(text).WithMeta(&value.Meta{Text: text})
	case v.Kind() == value.KindString && yamlnode.ResolvePlainTag(text) == yamlnode.TagTimestamp:
		return timestamp(text)
	default:
		return v
	}
}

// scalarKind reports whether v is a scalar.
func scalarKind(v *value.Value) bool {
	return v.Kind() != value.KindArray && v.Kind() != value.KindObject
}

// isTimestamp reports whether yq reads v as a timestamp, which is a node
// tagged so or an unquoted scalar written as one.
func isTimestamp(v *value.Value) bool {
	if v.Kind() != value.KindString {
		return false
	}
	m := v.Meta()
	if m != nil && m.Tag != "" {
		return m.Tag == timestampTag
	}
	if m != nil && m.Style&(value.SingleQuoted|value.DoubleQuoted|value.Literal|value.Folded) != 0 {
		return false
	}
	return yamlnode.ResolvePlainTag(v.Str()) == yamlnode.TagTimestamp
}

// parseDateTime reads s as a time in layout. Under the default layout it
// also reads a plain date, and reports the failure to read one.
func parseDateTime(layout, s string) (time.Time, error) {
	t, err := time.Parse(layout, s)
	if err == nil || layout != time.RFC3339 {
		return t, err
	}
	return time.Parse(dateOnly, s)
}

// argText is the text of the first value arg gives on in, and false
// when it gives none.
func argText(in *value.Value, arg runtime.Thunk) (string, bool, error) {
	var text string
	var found bool
	err := arg.Each(in, func(v *value.Value, _ value.Path) error {
		text, found = nodeText(v), true
		return errFirst
	})
	if err != nil && !errors.Is(err, errFirst) {
		return "", false, err
	}
	return text, found, nil
}

// tz writes a time in the zone its argument names.
func tz(ctx *runtime.Ctx, in *value.Value, args []runtime.Thunk, emit runtime.Emit) error {
	name, ok, err := argText(in, args[0])
	if err != nil {
		return err
	}
	if !ok {
		return &query.RuntimeError{Msg: "could not find timezone for format_time"}
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return &query.RuntimeError{Msg: "could not load tz [" + name + "]: " + err.Error()}
	}
	layout := layoutOf(ctx)
	t, err := parseDateTime(layout, dateText(in))
	if err != nil {
		return parseFailure(ctx, layout, err)
	}
	return emit(timeNode(t.In(loc).Format(layout)), nil)
}

// dateText is the text of a node a date operator reads, which is nothing
// for a null written empty.
func dateText(v *value.Value) string {
	if v.Kind() == value.KindNull {
		return metaOf(v).Text
	}
	return nodeText(v)
}

// parseFailure is the error yq gives for a time it cannot read in layout.
func parseFailure(ctx *runtime.Ctx, layout string, err error) error {
	return &query.RuntimeError{Msg: "could not parse datetime of " + cursorPath(ctx) + " using layout [" + layout + "]: " + err.Error()}
}

// formatDatetime writes a time in the layout its argument gives, as the
// scalar that text reads as.
func formatDatetime(ctx *runtime.Ctx, in *value.Value, args []runtime.Thunk, emit runtime.Emit) error {
	format, ok, err := argText(in, args[0])
	if err != nil {
		return err
	}
	if !ok {
		return &query.RuntimeError{Msg: "could not find format for format_time"}
	}
	t, err := parseDateTime(layoutOf(ctx), dateText(in))
	if err != nil {
		return &query.RuntimeError{Msg: "could not parse datetime of " + cursorPath(ctx) + ": " + err.Error()}
	}
	return emit(timeNode(t.Format(format)), nil)
}

// fromUnix writes the time a number of seconds since the epoch is, in the
// local zone. Like now, it writes the default layout whatever with_dtf sets.
func fromUnix(_ *runtime.Ctx, in *value.Value, _ []runtime.Thunk, emit runtime.Emit) error {
	if in.Kind() != value.KindNumber {
		return &query.RuntimeError{Msg: "from_unix only works on numbers, found " + tagOf(in) + " instead"}
	}
	f, err := strconv.ParseFloat(nodeText(in), 64)
	if err != nil {
		return &query.RuntimeError{Msg: err.Error()}
	}
	return emit(timestamp(time.Unix(int64(f), 0).Format(time.RFC3339)), nil)
}

// toUnix is a time's seconds since the epoch.
func toUnix(ctx *runtime.Ctx, in *value.Value, _ []runtime.Thunk, emit runtime.Emit) error {
	layout := layoutOf(ctx)
	t, err := parseDateTime(layout, dateText(in))
	if err != nil {
		return parseFailure(ctx, layout, err)
	}
	return emit(value.NumDec(decimal.FromInt64(t.Unix())), nil)
}

// withDtf runs its second argument with its first as the date time
// layout. The layout ends with the second argument, so what its outputs
// flow into reads the layout it had before.
func withDtf(ctx *runtime.Ctx, in *value.Value, args []runtime.Thunk, emit runtime.Emit) error {
	layout, ok, err := argText(in, args[0])
	if err != nil {
		return err
	}
	if !ok {
		return &query.RuntimeError{Msg: "could not get date time format: could not find layout for format_time"}
	}
	type output struct {
		v *value.Value
		p value.Path
	}
	var outs []output
	prev, had := layouts.Load(ctx)
	layouts.Store(ctx, layout)
	err = args[1].Each(in, func(v *value.Value, p value.Path) error {
		outs = append(outs, output{v, p})
		return nil
	})
	if had {
		layouts.Store(ctx, prev)
	} else {
		layouts.Delete(ctx)
	}
	if err != nil {
		return err
	}
	for _, o := range outs {
		if err := emit(o.v, o.p); err != nil {
			return err
		}
	}
	return nil
}

// dateArith is l plus r, or minus when negate is set, when l is a time
// and r a duration, and false when l is no time and the operator is the
// ordinary one. A timestamp that does not read in the layout in force is
// an error, where a string that does not read in a layout of with_dtf's
// is no time at all.
func dateArith(ctx *runtime.Ctx, l, r *value.Value, negate bool) (*value.Value, bool, error) {
	if l.Kind() != value.KindString || collection(r) {
		return nil, false, nil
	}
	layout := layoutOf(ctx)
	ts := isTimestamp(l)
	if !ts && layout == time.RFC3339 {
		return nil, false, nil
	}
	t, err := parseDateTime(layout, l.Str())
	if err != nil && !ts {
		return nil, false, nil
	}
	text := nodeText(r)
	if r.Kind() == value.KindNull && text == "" {
		text = "null"
	}
	signed := text
	if negate {
		signed = "-" + text
	}
	dur, derr := time.ParseDuration(signed)
	if derr != nil {
		return nil, true, &query.RuntimeError{Msg: "unable to parse duration [" + text + "]: " + derr.Error()}
	}
	if err != nil {
		return nil, true, &query.RuntimeError{Msg: err.Error()}
	}
	m := metaOf(l)
	m.Text = t.Add(dur).Format(layout)
	if ts {
		m.Tag = timestampTag
	}
	return withMeta(value.Str(m.Text), m), true, nil
}
