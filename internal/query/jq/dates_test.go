package jq_test

import (
	"testing"
	"time"
)

// pinLocal makes the local zone the named one for the rest of the test,
// through TZ and time.Local both, so nothing here depends on the machine's
// zone.
func pinLocal(t *testing.T, name string) {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Skipf("zone %s is unavailable: %v", name, err)
	}
	t.Setenv("TZ", name)
	old := time.Local
	time.Local = loc
	t.Cleanup(func() { time.Local = old })
}

func TestDates(t *testing.T) {
	pinLocal(t, "UTC")
	wantOutputs(t, `gmtime`, `1425599621`, `[2015,2,5,23,53,41,4,63]`)
	wantOutputs(t, `gmtime | mktime`, `1425599621`, `1425599621`)
	wantOutputs(t, `todate, (todate | fromdate)`, `1425599621`, `"2015-03-05T23:53:41Z"`, `1425599621`)
	wantOutputs(t, `strptime("%Y-%m-%dT%H:%M:%SZ")`, `"2015-03-05T23:53:41Z"`, `[2015,2,5,23,53,41,4,63]`)
	wantOutputs(t, `strftime("%A, %B %d, %Y")`, `1425599621`, `"Thursday, March 05, 2015"`)
	wantOutputs(t, `gmtime | strftime("%j %e %Z %s")`, `1425599621`, `"064  5 UTC 1425599621"`)
	wantError(t, `fromdateiso8601`, `"2015-03-05T23:53:41.123Z"`, `date "2015-03-05T23:53:41.123Z" does not match format "%Y-%m-%dT%H:%M:%SZ"`)
	wantError(t, `fromdate`, `"nope"`, `date "nope" does not match format "%Y-%m-%dT%H:%M:%SZ"`)
	wantOutputs(t, `now | type`, `null`, `"number"`)
}

func TestDatesAliases(t *testing.T) {
	pinLocal(t, "UTC")
	wantOutputs(t, `todateiso8601`, `1425599621`, `"2015-03-05T23:53:41Z"`)
	wantOutputs(t, `fromdateiso8601`, `"2015-03-05T23:53:41Z"`, `1425599621`)
	wantOutputs(t, `-1 | todate`, `null`, `"1969-12-31T23:59:59Z"`)
	wantOutputs(t, `todate`, `1e12`, `"33658-09-27T01:46:40Z"`)
	wantOutputs(t, `todate`, `1425599621.678`, `"2015-03-05T23:53:41Z"`)
}

// jq 1.8.2 dropped date, dateadd, and datesub, so they are undefined.
func TestDatesDroppedFromJQ(t *testing.T) {
	wantError(t, `date`, `1425599621`, `date/0 is not defined`)
	wantError(t, `dateadd("seconds"; 10)`, `1425599621`, `dateadd/2 is not defined`)
	wantError(t, `datesub("seconds"; 10)`, `1425599621`, `datesub/2 is not defined`)
}

func TestGmtimeFraction(t *testing.T) {
	pinLocal(t, "UTC")
	wantOutputs(t, `gmtime`, `1425599621.678`, `[2015,2,5,23,53,41.678,4,63]`)
	wantOutputs(t, `gmtime`, `-1.5`, `[1969,11,31,23,59,59.5,3,364]`)
	wantOutputsFloat(t, `gmtime`, `1425599621.678`, `[2015,2,5,23,53,41.67799997329712,4,63]`)
}

func TestDateErrors(t *testing.T) {
	pinLocal(t, "UTC")
	wantError(t, `gmtime`, `"x"`, `gmtime() requires numeric inputs`)
	wantError(t, `localtime`, `"x"`, `localtime() requires numeric inputs`)
	wantError(t, `gmtime`, `1e18`, `error converting number of seconds since epoch to datetime`)
	wantError(t, `mktime`, `"x"`, `mktime requires array inputs`)
	wantError(t, `mktime`, `["a",2,5,1,1,1]`, `mktime requires parsed datetime inputs`)
	wantError(t, `mktime`, `[]`, `invalid gmtime representation`)
	wantError(t, `strftime("%Y")`, `"a"`, `strftime/1 requires parsed datetime inputs`)
	wantError(t, `strftime("%Y")`, `["a",1,1,1,1,1,1,1]`, `strftime/1 requires parsed datetime inputs`)
	wantError(t, `strftime(1)`, `1`, `strftime/1 requires a string format`)
	wantError(t, `strflocaltime("%Y")`, `"x"`, `strflocaltime/1 requires parsed datetime inputs`)
	wantError(t, `strflocaltime(1)`, `1`, `strflocaltime/1 requires a string format`)
	wantError(t, `strptime("%Y")`, `1`, `strptime/1 requires string inputs and arguments`)
	wantError(t, `strptime(1)`, `"2015"`, `strptime/1 requires string inputs and arguments`)
	wantError(t, `strptime("%Y")`, `"x"`, `date "x" does not match format "%Y"`)
	wantError(t, `strptime("%Y")`, `"10000"`, `date "10000" does not match format "%Y"`)
}

func TestMktime(t *testing.T) {
	pinLocal(t, "UTC")
	wantOutputs(t, `mktime`, `[2015,2,5]`, `1425513600`)
	wantOutputs(t, `mktime`, `[2015]`, `1419984000`)
	wantOutputs(t, `mktime`, `[2015,13,40,25,61,61]`, `1457748121`)
	wantOutputs(t, `mktime`, `[2015.9,2.9,5.5,1,1,1]`, `1425517261`)
	wantOutputs(t, `mktime`, `[2015,2,5,23,53,41,4,63,9]`, `1425599621`)
	wantOutputs(t, `mktime`, `[2015,2,5,23,53,41.7,4,63]`, `1425599621`)
	wantOutputs(t, `mktime`, `[2147483647,2,5,1,1,1]`, `67767976207443661`)
	wantOutputs(t, `mktime`, `[2015,2,5,1,1,1e12]`, `3573000907`)
	wantError(t, `mktime`, `[1899,0,1,0,0,0]`, `invalid gmtime representation`)
	wantError(t, `mktime`, `[1900,0,0,0,0,0]`, `invalid gmtime representation`)
}

func TestStrftimeDirectives(t *testing.T) {
	pinLocal(t, "UTC")
	wantOutputs(t,
		`strftime("%a %b %e %y %C %p %u %w %U %T %D %F %c %x %X %% %z %Z %s %I %l %k %h %n %t %R %r %G %g %V %W")`,
		`1425599621`,
		`"Thu Mar  5 15 20 PM 4 4 09 23:53:41 03/05/15 2015-03-05 Thu Mar  5 23:53:41 2015 03/05/2015 23:53:41 % +0000 UTC 1425599621 11 11 23 Mar \n \t 23:53 11:53:41 PM 2015 15 10 09"`)
	wantOutputs(t, `strftime("%E %Q %5Y %-d %_d %^a")`, `1425599621`, `" Q 5Y 5  5 ^a"`)
	wantOutputs(t, `strftime("")`, `1425599621`, `""`)
	wantOutputs(t, `strftime("%a %j %A")`, `[2015,2,5,23,53,41,0,0]`, `"Thu 064 Thursday"`)
	wantOutputs(t, `strftime("%a %j")`, `[2015,2,5,23,53,41]`, `"Thu 064"`)
	wantOutputs(t, `strftime("%Y")`, `[1]`, `"0001"`)
	wantOutputs(t, `strftime("%Y")`, `[]`, `"1900"`)
	wantOutputs(t, `strftime("%Y %m")`, `[1,2]`, `"0001 03"`)
}

func TestStrptimeDirectives(t *testing.T) {
	pinLocal(t, "UTC")
	cases := []struct{ format, in, want string }{
		{"%I:%M %p %b %d %Y", "10:20 PM Mar 5 2015", `[2015,2,5,22,20,0,4,63]`},
		{"%A %Y %j", "Thursday 2015 64", `[2015,2,5,0,0,0,4,63]`},
		{"%Y-%m-%d %z", "2015-03-05 +0100", `[2015,2,4,23,0,0,3,62]`},
		{"%Y-%m-%d %Z", "2015-03-05 UTC", `[2015,2,5,0,0,0,4,63]`},
		{"%Y", "2015 x", `[2015,0,0,0,0,0,3,-1," x"]`},
		{"%Y-%m-%d", "2015-3-5", `[2015,2,5,0,0,0,4,63]`},
		{"%y", "15", `[2015,0,0,0,0,0,3,-1]`},
		{"%y", "69", `[1969,0,0,0,0,0,2,-1]`},
		{"%y", "68", `[2068,0,0,0,0,0,6,-1]`},
		{"%R", "23:53", `[1900,0,0,23,53,0,6,-1]`},
		{"%D", "03/05/15", `[2015,2,5,0,0,0,4,63]`},
		{"%FT%T", "2015-03-05T23:53:41", `[2015,2,5,23,53,41,4,63]`},
		{"%a", "Thu", `[1900,0,0,0,0,0,6,-1]`},
		{"%Y-%m-%d", "2015-02-30", `[2015,1,30,0,0,0,1,60]`},
		{"%Y-%m-%d %I %p", "2015-03-05 11 PM", `[2015,2,5,23,0,0,4,63]`},
		{"%I %p", "12 AM", `[1900,0,0,0,0,0,6,-1]`},
		{"%Y-%m-%d ", "2015-03-05", `[2015,2,5,0,0,0,4,63]`},
		{"%Y %%", "2015 %", `[2015,0,0,0,0,0,3,-1]`},
		{"%B", "march", `[1900,2,0,0,0,0,3,58]`},
		{"%b", "Sep", `[1900,8,0,0,0,0,5,242]`},
		{"%s", "1425599621", `[2015,2,5,23,53,41,4,63]`},
		{"%Y", "0", `[0,0,0,0,0,0,5,-1]`},
	}
	for _, c := range cases {
		wantOutputs(t, `strptime("`+c.format+`")`, `"`+c.in+`"`, c.want)
	}
	wantError(t, `strptime("%b")`, `"sept"`, `date "sept" does not match format "%b"`)
	wantError(t, `strptime("%s")`, `"1425599621.5"`, `date "1425599621.5" does not match format "%s"`)
	wantError(t, `strptime("%Y")`, `"999999"`, `date "999999" does not match format "%Y"`)
}

func TestLocalDates(t *testing.T) {
	pinLocal(t, "America/New_York")
	wantOutputs(t, `localtime`, `1425599621`, `[2015,2,5,18,53,41,4,63]`)
	wantOutputs(t, `strflocaltime("%Y-%m-%d %H:%M:%S %Z %z %s")`, `1425599621`, `"2015-03-05 18:53:41 EST -0500 1425599621"`)
	wantOutputs(t, `strflocaltime("%H %Z %z %s")`, `[2015,2,5,23,53,41,4,63]`, `"23 EST -0500 1425617621"`)
	wantOutputs(t, `strflocaltime("%H %Z %z %s")`, `[2015,6,5,23,53,41,4,63]`, `"23 EDT -0400 1436154821"`)
	wantOutputs(t, `strftime("%H %Z %z %s")`, `[2015,2,5,23,53,41,4,63]`, `"23 UTC +0000 1425599621"`)
	wantOutputs(t, `localtime | mktime`, `1436140800`, `1436126400`)
	wantOutputs(t, `gmtime | strflocaltime("%H %Z")`, `1425599621`, `"23 EST"`)
	wantOutputs(t, `localtime`, `1425599621.678`, `[2015,2,5,18,53,41.678,4,63]`)
}

// TZ is read when the builtin runs, as the C library reads it, so a zone
// set after the process started still counts.
func TestLocalDatesFollowTZ(t *testing.T) {
	pinLocal(t, "America/New_York")
	t.Setenv("TZ", "UTC")
	wantOutputs(t, `localtime, (localtime | mktime)`, `1425599621`, `[2015,2,5,23,53,41,4,63]`, `1425599621`)
	wantOutputs(t, `strflocaltime("%H %Z %z")`, `1425599621`, `"23 UTC +0000"`)
	wantOutputs(t, `strflocaltime("%H %Z %z")`, `[2015,6,5,23,53,41,4,63]`, `"23 UTC +0000"`)
	t.Setenv("TZ", ":Asia/Tokyo")
	wantOutputs(t, `strflocaltime("%H %Z %z")`, `1425599621`, `"08 JST +0900"`)
	t.Setenv("TZ", "")
	wantOutputs(t, `strflocaltime("%H %Z")`, `1425599621`, `"23 UTC"`)
	t.Setenv("TZ", "No/Such_Zone")
	wantOutputs(t, `strflocaltime("%H %Z")`, `1425599621`, `"23 UTC"`)
}
