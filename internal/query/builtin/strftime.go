package builtin

import (
	"fmt"
	"strconv"
	"strings"
)

// brokenTime is jq's broken-down time, in the order of its arrays: year,
// month from zero, day of the month, hour, minute, seconds, day of the
// week from Sunday, and day of the year from zero.
type brokenTime [8]float64

const (
	btYear = iota
	btMon
	btMday
	btHour
	btMin
	btSec
	btWday
	btYday
)

// zoneInfo is what the zone directives of strftime print.
type zoneInfo struct {
	name   string
	offset int
	epoch  int64
}

var (
	weekdayNames = [7]string{"Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"}
	monthNames   = [12]string{"January", "February", "March", "April", "May", "June", "July", "August", "September", "October", "November", "December"}
)

// floorDiv and floorMod round toward negative infinity.
func floorDiv(a, b int64) int64 {
	q := a / b
	if a%b != 0 && (a < 0) != (b < 0) {
		q--
	}
	return q
}

func floorMod(a, b int64) int64 { return a - floorDiv(a, b)*b }

func isLeap(y int64) bool { return (y%4 == 0 && y%100 != 0) || y%400 == 0 }

// tmWday is the day of the week jq computes from the year, month, and day
// of the month alone, which for a day of the month of zero is the day
// before the first.
func tmWday(year, mon, mday int64) int64 {
	century := year / 100
	y := year % 100
	if mon < 2 {
		y--
	}
	m := mon - 1
	if m < 1 {
		m += 12
	}
	w := (mday + (26*m-2)/10 + y + floorDiv(y, 4) + floorDiv(century, 4) - 2*century) % 7
	if w < 0 {
		w += 7
	}
	return w
}

// tmYday is the day of the year jq computes from the same three fields.
func tmYday(year, mon, mday int64) int64 {
	days := [12]int64{0, 31, 59, 90, 120, 151, 181, 212, 243, 273, 304, 334}
	leap := int64(0)
	if mon > 1 && isLeap(year) {
		leap = 1
	}
	if mon < 0 {
		mon = -mon
	}
	if mon > 11 {
		mon %= 12
	}
	return days[mon] + leap + mday - 1
}

// withCalendarDays fills in the day of the week and of the year from the
// date fields, as jq does for a broken-down time it is handed.
func (bt brokenTime) withCalendarDays() brokenTime {
	y, m, d := int64(bt[btYear]), int64(bt[btMon]), int64(bt[btMday])
	bt[btWday] = float64(tmWday(y, m, d))
	bt[btYday] = float64(tmYday(y, m, d))
	return bt
}

// strftime formats a broken-down time as UTC.
func strftime(layout string, bt brokenTime) (string, error) {
	return formatTime(layout, bt, zoneInfo{name: utc, epoch: epochOf(bt)})
}

// isoWeek is the ISO 8601 week number and the year it belongs to.
func isoWeek(year, wday, yday int64) (int64, int64) {
	week := (yday - (wday+6)%7 + 10) / 7
	switch {
	case week < 1:
		year--
		week = weeksInYear(year)
	case week > weeksInYear(year):
		year++
		week = 1
	}
	return week, year
}

func weeksInYear(y int64) int64 {
	p := func(y int64) int64 { return floorMod(y+floorDiv(y, 4)-floorDiv(y, 100)+floorDiv(y, 400), 7) }
	if p(y) == 4 || p(y-1) == 3 {
		return 53
	}
	return 52
}

// utc names the zone of a time that was never given one.
const utc = "UTC"

// composites are the conversions that stand for other conversions.
var composites = map[byte]string{
	'c': "%a %b %e %H:%M:%S %Y",
	'D': "%m/%d/%y",
	'F': "%Y-%m-%d",
	'r': "%I:%M:%S %p",
	'R': "%H:%M",
	'T': "%H:%M:%S",
	'x': "%m/%d/%Y",
	'X': "%H:%M:%S",
}

// formatter expands the conversions of one layout.
type formatter struct {
	b    strings.Builder
	flag byte
	bt   brokenTime
	z    zoneInfo
}

func (f *formatter) year() int64 { return int64(f.bt[btYear]) }
func (f *formatter) mon() int64  { return int64(f.bt[btMon]) }
func (f *formatter) mday() int64 { return int64(f.bt[btMday]) }
func (f *formatter) hour() int64 { return int64(f.bt[btHour]) }
func (f *formatter) wday() int64 { return floorMod(int64(f.bt[btWday]), 7) }
func (f *formatter) yday() int64 { return int64(f.bt[btYday]) }

func (f *formatter) hour12() int64 {
	h := floorMod(f.hour(), 12)
	if h == 0 {
		h = 12
	}
	return h
}

// num prints v padded to width, as the flag allows.
func (f *formatter) num(v int64, width int, pad byte) {
	switch f.flag {
	case '-':
		f.b.WriteString(strconv.FormatInt(v, 10))
		return
	case '_':
		pad = ' '
	case '0':
		pad = '0'
	}
	s := strconv.FormatInt(v, 10)
	if len(s) < width {
		s = strings.Repeat(string(pad), width-len(s)) + s
	}
	f.b.WriteString(s)
}

// dateNumber prints the numeric conversions of the date, and reports
// whether d was one.
func (f *formatter) dateNumber(d byte) bool {
	switch d {
	case 'C':
		f.num(floorDiv(f.year(), 100), 2, '0')
	case 'd':
		f.num(f.mday(), 2, '0')
	case 'e':
		f.num(f.mday(), 2, ' ')
	case 'j':
		f.num(f.yday()+1, 3, '0')
	case 'm':
		f.num(f.mon()+1, 2, '0')
	case 'y':
		f.num(floorMod(f.year(), 100), 2, '0')
	case 'Y':
		f.num(f.year(), 4, '0')
	default:
		return f.weekNumber(d)
	}
	return true
}

// weekNumber prints the conversions that count weeks and weekdays.
func (f *formatter) weekNumber(d byte) bool {
	wday, yday := f.wday(), f.yday()
	week, isoYear := isoWeek(f.year(), wday, yday)
	switch d {
	case 'g':
		f.num(floorMod(isoYear, 100), 2, '0')
	case 'G':
		f.num(isoYear, 4, '0')
	case 'u':
		f.num((wday+6)%7+1, 1, '0')
	case 'U':
		f.num((yday+7-wday)/7, 2, '0')
	case 'V':
		f.num(week, 2, '0')
	case 'w':
		f.num(wday, 1, '0')
	case 'W':
		f.num((yday+7-(wday+6)%7)/7, 2, '0')
	default:
		return false
	}
	return true
}

// clockNumber prints the numeric conversions of the time of day, and
// reports whether d was one.
func (f *formatter) clockNumber(d byte) bool {
	switch d {
	case 'H':
		f.num(f.hour(), 2, '0')
	case 'I':
		f.num(f.hour12(), 2, '0')
	case 'k':
		f.num(f.hour(), 2, ' ')
	case 'l':
		f.num(f.hour12(), 2, ' ')
	case 'M':
		f.num(int64(f.bt[btMin]), 2, '0')
	case 'S':
		f.num(int64(f.bt[btSec]), 2, '0')
	case 's':
		f.num(f.z.epoch, 1, '0')
	default:
		return false
	}
	return true
}

// text prints the conversions that write words and symbols, and reports
// whether d was one.
func (f *formatter) text(d byte) bool {
	switch d {
	case 'a':
		f.b.WriteString(weekdayNames[f.wday()][:3])
	case 'A':
		f.b.WriteString(weekdayNames[f.wday()])
	case 'b', 'h':
		f.b.WriteString(monthNames[floorMod(f.mon(), 12)][:3])
	case 'B':
		f.b.WriteString(monthNames[floorMod(f.mon(), 12)])
	case 'n':
		f.b.WriteByte('\n')
	case 't':
		f.b.WriteByte('\t')
	case 'p':
		f.b.WriteString(meridiem(f.hour()))
	case 'z':
		off, sign := f.z.offset, byte('+')
		if off < 0 {
			off, sign = -off, '-'
		}
		fmt.Fprintf(&f.b, "%c%02d%02d", sign, off/3600, off%3600/60)
	case 'Z':
		f.b.WriteString(f.z.name)
	default:
		return false
	}
	return true
}

func meridiem(hour int64) string {
	if floorMod(hour, 24) >= 12 {
		return "PM"
	}
	return "AM"
}

// conversion expands the conversion d. One it does not know prints as
// its own character.
func (f *formatter) conversion(d byte) {
	if sub, ok := composites[d]; ok {
		s, _ := formatTime(sub, f.bt, f.z)
		f.b.WriteString(s)
		return
	}
	if f.dateNumber(d) || f.clockNumber(d) || f.text(d) {
		return
	}
	f.b.WriteByte(d)
}

// formatTime expands the conversions in layout. The flags -, _, and 0
// choose the padding of numeric ones.
func formatTime(layout string, bt brokenTime, z zoneInfo) (string, error) {
	f := &formatter{bt: bt, z: z}
	for i := 0; i < len(layout); i++ {
		if layout[i] != '%' {
			f.b.WriteByte(layout[i])
			continue
		}
		i++
		f.flag = 0
		for i < len(layout) && (layout[i] == '-' || layout[i] == '_' || layout[i] == '0') {
			f.flag = layout[i]
			i++
		}
		for i < len(layout) && (layout[i] == 'E' || layout[i] == 'O') {
			i++
		}
		if i >= len(layout) {
			f.b.WriteByte('%')
			break
		}
		f.conversion(layout[i])
	}
	return f.b.String(), nil
}

// daysFromCivil is the number of days from 1970-01-01 to the proleptic
// Gregorian date, for a month counted from one.
func daysFromCivil(y, m, d int64) int64 {
	if m <= 2 {
		y--
	}
	era := floorDiv(y, 400)
	yoe := y - era*400
	mp := (m + 9) % 12
	doy := (153*mp+2)/5 + d - 1
	doe := yoe*365 + yoe/4 - yoe/100 + doy
	return era*146097 + doe - 719468
}

// civilFromDays is the inverse of daysFromCivil.
func civilFromDays(z int64) (y, m, d int64) {
	z += 719468
	era := floorDiv(z, 146097)
	doe := z - era*146097
	yoe := (doe - doe/1460 + doe/36524 - doe/146096) / 365
	y = yoe + era*400
	doy := doe - (365*yoe + yoe/4 - yoe/100)
	mp := (5*doy + 2) / 153
	d = doy - (153*mp+2)/5 + 1
	m = mp + 3
	if m > 12 {
		m -= 12
	}
	if m <= 2 {
		y++
	}
	return y, m, d
}

// epochOf is timegm: the seconds since the epoch of a broken-down time
// read as UTC, with out-of-range fields carried into the next ones.
func epochOf(bt brokenTime) int64 {
	year, mon := int64(bt[btYear]), int64(bt[btMon])
	year += floorDiv(mon, 12)
	mon = floorMod(mon, 12)
	days := daysFromCivil(year, mon+1, 1) + int64(bt[btMday]) - 1
	return days*86400 + int64(bt[btHour])*3600 + int64(bt[btMin])*60 + int64(bt[btSec])
}

// parser reads the fields a layout names.
type parser struct {
	s        string
	pos      int
	year     int64
	mon      int64
	mday     int64
	hour     int64
	min      int64
	sec      int64
	yday     int64
	haveYday bool
	haveMD   bool
	hour12   bool
	pm       bool
	offset   int64
	epoch    *int64
}

func (p *parser) skipSpace() {
	for p.pos < len(p.s) && isSpace(p.s[p.pos]) {
		p.pos++
	}
}

func isSpace(c byte) bool { return c == ' ' || (c >= '\t' && c <= '\r') }

// number reads up to digits digits, after any white space, within lo..hi.
func (p *parser) number(digits int, lo, hi int64) (int64, bool) {
	p.skipSpace()
	start := p.pos
	for p.pos < len(p.s) && p.pos-start < digits && p.s[p.pos] >= '0' && p.s[p.pos] <= '9' {
		p.pos++
	}
	if p.pos == start {
		return 0, false
	}
	n, _ := strconv.ParseInt(p.s[start:p.pos], 10, 64)
	if n < lo || n > hi {
		return 0, false
	}
	return n, true
}

// name reads one of names, by its full form or its three-letter one,
// ignoring case.
func (p *parser) name(names []string) (int64, bool) {
	rest := strings.ToLower(p.s[p.pos:])
	for i, n := range names {
		if l := strings.ToLower(n); strings.HasPrefix(rest, l) {
			p.pos += len(l)
			return int64(i), true
		}
	}
	for i, n := range names {
		if l := strings.ToLower(n[:3]); strings.HasPrefix(rest, l) {
			p.pos += 3
			return int64(i), true
		}
	}
	return 0, false
}

// read stores the number the conversion reads, after any white space, in
// dst when it falls within lo and hi.
func (p *parser) read(dst *int64, digits int, lo, hi int64) bool {
	n, ok := p.number(digits, lo, hi)
	if ok {
		*dst = n
	}
	return ok
}

// dateConversion reads the conversions that set the date, and reports
// whether d was one along with whether the input matched.
func (p *parser) dateConversion(d byte) (matched, known bool) {
	switch d {
	case 'Y':
		return p.read(&p.year, 4, 0, 9999), true
	case 'm':
		p.haveMD = true
		if !p.read(&p.mon, 2, 1, 12) {
			return false, true
		}
		p.mon--
		return true, true
	case 'd', 'e':
		p.haveMD = true
		return p.read(&p.mday, 2, 1, 31), true
	case 'j':
		if !p.read(&p.yday, 3, 1, 366) {
			return false, true
		}
		p.yday--
		p.haveYday = true
		return true, true
	case 'y':
		var y int64
		if !p.read(&y, 2, 0, 99) {
			return false, true
		}
		p.year = 2000 + y
		if y >= 69 {
			p.year = 1900 + y
		}
		return true, true
	case 'C', 'U', 'W', 'u', 'w', 'g':
		var skip int64
		return p.read(&skip, 2, 0, 99), true
	case 'G':
		var skip int64
		return p.read(&skip, 4, 0, 9999), true
	}
	return false, false
}

// clockConversion reads the conversions that set the time of day.
func (p *parser) clockConversion(d byte) (matched, known bool) {
	switch d {
	case 'H', 'k':
		return p.read(&p.hour, 2, 0, 23), true
	case 'I', 'l':
		p.hour12 = true
		if !p.read(&p.hour, 2, 1, 12) {
			return false, true
		}
		p.hour %= 12
		return true, true
	case 'M':
		return p.read(&p.min, 2, 0, 59), true
	case 'S':
		return p.read(&p.sec, 2, 0, 61), true
	case 'p':
		return p.meridiem(), true
	case 's':
		return p.epochSeconds(), true
	}
	return false, false
}

// meridiem reads AM or PM.
func (p *parser) meridiem() bool {
	rest := strings.ToUpper(p.s[p.pos:])
	switch {
	case strings.HasPrefix(rest, "AM"):
	case strings.HasPrefix(rest, "PM"):
		p.pm = true
	default:
		return false
	}
	p.pos += 2
	return true
}

// epochSeconds reads a whole number of seconds since the epoch.
func (p *parser) epochSeconds() bool {
	start := p.pos
	if p.pos < len(p.s) && p.s[p.pos] == '-' {
		p.pos++
	}
	digits := p.pos
	for p.pos < len(p.s) && p.s[p.pos] >= '0' && p.s[p.pos] <= '9' {
		p.pos++
	}
	if p.pos == digits {
		return false
	}
	n, err := strconv.ParseInt(p.s[start:p.pos], 10, 64)
	if err != nil {
		return false
	}
	p.epoch = &n
	return true
}

// zoneName reads a run of letters, which names a zone and changes
// nothing.
func (p *parser) zoneName() bool {
	start := p.pos
	for p.pos < len(p.s) && (p.s[p.pos] >= 'A' && p.s[p.pos] <= 'Z' || p.s[p.pos] >= 'a' && p.s[p.pos] <= 'z') {
		p.pos++
	}
	return p.pos > start
}

// conversion reads one conversion and reports whether the input matched.
func (p *parser) conversion(d byte) bool {
	if d == 'x' {
		return p.run("%m/%d/%y")
	}
	if sub, ok := composites[d]; ok {
		return p.run(sub)
	}
	if matched, known := p.dateConversion(d); known {
		return matched
	}
	if matched, known := p.clockConversion(d); known {
		return matched
	}
	var ok bool
	switch d {
	case 'a', 'A':
		_, ok = p.name(weekdayNames[:])
	case 'b', 'B', 'h':
		if p.mon, ok = p.name(monthNames[:]); ok {
			p.haveMD = true
		}
	case 'z':
		ok = p.zone()
	case 'Z':
		ok = p.zoneName()
	case 'n', 't':
		p.skipSpace()
		ok = true
	case '%':
		ok = p.pos < len(p.s) && p.s[p.pos] == '%'
		if ok {
			p.pos++
		}
	}
	return ok
}

// zone reads a numeric UTC offset, or a Z for none.
func (p *parser) zone() bool {
	p.skipSpace()
	if p.pos < len(p.s) && p.s[p.pos] == 'Z' {
		p.pos++
		return true
	}
	if p.pos >= len(p.s) || (p.s[p.pos] != '+' && p.s[p.pos] != '-') {
		return false
	}
	sign := int64(1)
	if p.s[p.pos] == '-' {
		sign = -1
	}
	p.pos++
	digits := func(n int) (int64, bool) {
		if p.pos+n > len(p.s) {
			return 0, false
		}
		v, err := strconv.ParseInt(p.s[p.pos:p.pos+n], 10, 64)
		if err != nil || v < 0 {
			return 0, false
		}
		p.pos += n
		return v, true
	}
	h, ok := digits(2)
	if !ok {
		return false
	}
	if p.pos < len(p.s) && p.s[p.pos] == ':' {
		p.pos++
	}
	m, ok := digits(2)
	if !ok {
		m = 0
	}
	p.offset = sign * (h*3600 + m*60)
	return true
}

// run matches layout against the input from the current position.
func (p *parser) run(layout string) bool {
	for i := 0; i < len(layout); i++ {
		c := layout[i]
		switch {
		case isSpace(c):
			p.skipSpace()
		case c != '%':
			if p.pos >= len(p.s) || p.s[p.pos] != c {
				return false
			}
			p.pos++
		default:
			i++
			for i < len(layout) && (layout[i] == 'E' || layout[i] == 'O' || layout[i] == '-' || layout[i] == '_' || layout[i] == '0') {
				i++
			}
			if i >= len(layout) || !p.conversion(layout[i]) {
				return false
			}
		}
	}
	return true
}

// strptime parses s by layout and returns the part of s it did not read,
// which is empty or starts with white space.
func strptime(layout, s string) (brokenTime, string, error) {
	p := &parser{s: s, year: 1900}
	if !p.run(layout) {
		return brokenTime{}, "", errNoMatch
	}
	rest := s[p.pos:]
	if rest != "" && !isSpace(rest[0]) {
		return brokenTime{}, "", errNoMatch
	}
	if p.epoch != nil {
		return epochTime(*p.epoch), rest, nil
	}
	if p.hour12 && p.pm {
		p.hour += 12
	}
	if p.haveYday && !p.haveMD {
		p.mon, p.mday = dayOfYear(p.year, p.yday)
	}
	bt := brokenTime{float64(p.year), float64(p.mon), float64(p.mday), float64(p.hour), float64(p.min), float64(p.sec)}
	if p.offset != 0 {
		bt = shiftSeconds(bt, -p.offset)
	}
	return bt.withCalendarDays(), rest, nil
}

var errNoMatch = fmt.Errorf("no match")

// dayOfYear is the month from zero and day of the month of a day of the
// year counted from zero.
func dayOfYear(year, yday int64) (int64, int64) {
	_, m, d := civilFromDays(daysFromCivil(year, 1, 1) + yday)
	return m - 1, d
}

// shiftSeconds moves a broken-down time by a number of seconds.
func shiftSeconds(bt brokenTime, delta int64) brokenTime {
	out := fieldsAt(epochOf(bt) + delta)
	out[btSec] = float64(int64(out[btSec]))
	return out
}

// epochTime is the broken-down local time of a number of seconds since the
// epoch.
func epochTime(sec int64) brokenTime {
	return localFields(sec)
}

// fieldsAt is the broken-down UTC time of a number of seconds.
func fieldsAt(sec int64) brokenTime {
	days := floorDiv(sec, 86400)
	rem := sec - days*86400
	y, m, d := civilFromDays(days)
	return brokenTime{
		float64(y), float64(m - 1), float64(d),
		float64(rem / 3600), float64(rem % 3600 / 60), float64(rem % 60),
		float64(floorMod(days+4, 7)), float64(days - daysFromCivil(y, 1, 1)),
	}
}
