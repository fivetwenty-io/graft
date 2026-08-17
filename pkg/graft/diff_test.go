package graft

import (
	"bufio"
	"os"
	"regexp"
	"strings"
	"testing"
	"unicode"

	yamlv3 "github.com/goccy/go-yaml"
	. "github.com/smartystreets/goconvey/convey"

	"github.com/fivetwenty-io/graft/internal/utils/ansi"
)

func TestDiff(t *testing.T) {
	// Disable ANSI colors for testing
	ansi.Color(false)

	YAML := func(s string) map[string]interface{} {
		data := make(map[string]interface{})
		err := yamlv3.Unmarshal(QuoteInjectKeys([]byte(s)), &data)
		So(err, ShouldBeNil)
		return NormalizeMap(data)
	}

	trim := func(s string) string {
		/* find the shortest prefix of space characters */
		shortest := regexp.MustCompile(`^ +`).FindString(s)
		s = strings.TrimRightFunc(s, unicode.IsSpace)
		return regexp.MustCompile(`(?m)^`+shortest).ReplaceAllString(s, "")
	}

	var test, a, b, diff string
	var current *string
	runtest := func() {
		if test == "" {
			return
		}
		Convey(test, t, func() {
			d, err := Diff(YAML(a), YAML(b))
			So(err, ShouldBeNil)
			So(trim(d.String("$")), ShouldEqual, trim(diff))
		})
	}

	testPat := regexp.MustCompile(`^###+\s+(.*)\s*$`)
	in, err := os.Open("../../tests/diff")
	if err != nil {
		panic(err)
	}

	s := bufio.NewScanner(in)
	for s.Scan() {
		if testPat.MatchString(s.Text()) {
			m := testPat.FindStringSubmatch(s.Text())
			runtest()
			test, a, b, diff = m[1], "", "", ""
			continue
		}

		if s.Text() == "---" {
			switch {
			case a == "":
				current = &a
			case b == "":
				current = &b
			default:
				current = &diff
			}
			continue
		}

		if current != nil {
			*current = *current + s.Text() + "\n"
		}
	}
	runtest()
}
