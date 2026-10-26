package regex_test

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/fivetwenty-io/graft/pkg/regex"
)

func ExampleCompile() {
	p := regex.MustCompile(`(?<user>\w+)@(?<host>[\w.]+)`, regex.WithFlags(regex.IgnoreCase))
	m, _ := p.Find(context.Background(), "mail: Wayne@Example.org", 0)
	fmt.Println(m.Groups[1].Text("mail: Wayne@Example.org"), m.Groups[2].Name)
	// Output: Wayne host
}

func ExampleCompile_errors() {
	_, err := regex.Compile(`a(b`)
	var se *regex.SyntaxError
	fmt.Println(errors.As(err, &se), se.Pos)

	_, err = regex.Compile(`(?R)`)
	var ue *regex.UnsupportedError
	fmt.Println(errors.As(err, &ue), ue.Construct)

	_, err = regex.Compile(`(a{1000}){1000}`)
	fmt.Println(errors.Is(err, regex.ErrBudget))
	// Output:
	// true 1
	// true recursion (?R)
	// true
}

func ExampleMustCompile() {
	p := regex.MustCompile(`\d+`)
	m, _ := p.Find(context.Background(), "abc 123", 0)
	fmt.Println(m.Text("abc 123"))
	// Output: 123
}

func ExampleWithFlavor() {
	p := regex.MustCompile(`ss`, regex.WithFlavor(regex.Oniguruma), regex.WithFlags(regex.IgnoreCase))
	m, _ := p.Find(context.Background(), "Straße", 0)
	fmt.Println(m.Text("Straße"))
	// Output: ß
}

func ExampleWithFlags() {
	p := regex.MustCompile(`a.b`, regex.WithFlags(regex.DotAll|regex.IgnoreCase))
	m, _ := p.Find(context.Background(), "A\nB", 0)
	fmt.Printf("%q\n", m.Text("A\nB"))
	// Output: "A\nB"
}

func ExampleWithBudget() {
	p := regex.MustCompile(`(a+)+$`, regex.WithBudget(regex.Budget{Steps: 10000}))
	_, err := p.Find(context.Background(), strings.Repeat("a", 30)+"b", 0)
	fmt.Println(errors.Is(err, regex.ErrBudget))
	// Output: true
}

func ExamplePattern_Find() {
	p := regex.MustCompile(`é+`)
	subject := "café éé"
	m, _ := p.Find(context.Background(), subject, 5)
	fmt.Println(m.Start, m.End, m.RuneStart, m.RuneEnd)
	// Output: 6 10 5 7
}

func ExamplePattern_FindAll() {
	p := regex.MustCompile(`a*`)
	for m, err := range p.FindAll(context.Background(), "baab", regex.MatchOptions{Global: true}) {
		if err != nil {
			fmt.Println(err)
			return
		}
		fmt.Printf("%d %q\n", m.RuneStart, m.Text("baab"))
	}
	// Output:
	// 0 ""
	// 1 "aa"
	// 3 ""
	// 4 ""
}

func ExamplePattern_Replace() {
	p := regex.MustCompile(`(?<word>\w+)`)
	out, _ := p.Replace(context.Background(), "hello world", true, func(m *regex.Match) (string, error) {
		return strings.ToUpper(m.Groups[1].Text("hello world")), nil
	})
	fmt.Println(out)
	// Output: HELLO WORLD
}

func ExamplePattern_Split() {
	parts, _ := regex.MustCompile(`\s*,\s*`).Split(context.Background(), "a , b,c")
	fmt.Printf("%q\n", parts)
	// Output: ["a" "b" "c"]
}

func ExamplePattern_GroupNames() {
	p := regex.MustCompile(`(?<year>\d{4})-(\d{2})`)
	fmt.Printf("%q\n", p.GroupNames())
	// Output: ["" "year" ""]
}

func ExampleMatch_Text() {
	subject := "id: 42"
	m, _ := regex.MustCompile(`\d+`).Find(context.Background(), subject, 0)
	fmt.Println(m.Text(subject))
	// Output: 42
}

func ExampleGroup_Text() {
	subject := "b"
	m, _ := regex.MustCompile(`(a)|(b)`).Find(context.Background(), subject, 0)
	fmt.Printf("%v %q %v %q\n", m.Groups[1].Matched, m.Groups[1].Text(subject), m.Groups[2].Matched, m.Groups[2].Text(subject))
	// Output: false "" true "b"
}

func ExampleSyntaxError_Error() {
	_, err := regex.Compile(`[z-a]`)
	fmt.Println(err)
	// Output: regex: range out of order in character class at position 1
}

func ExampleUnsupportedError_Error() {
	_, err := regex.Compile(`(a)(?1)`)
	fmt.Println(err)
	// Output: regex: recursion (?1) is not supported
}

func ExampleFlavor() {
	for _, flavor := range []regex.Flavor{regex.PCRE, regex.Oniguruma} {
		m, _ := regex.MustCompile(`\d`, regex.WithFlavor(flavor)).Find(context.Background(), "٣", 0)
		fmt.Println(m != nil)
	}
	// Output:
	// false
	// true
}

func ExampleFlags() {
	p := regex.MustCompile(`
		(\d+)  # the hours
		:
		(\d+)  # the minutes
	`, regex.WithFlags(regex.Extended))
	m, _ := p.Find(context.Background(), "at 10:45", 0)
	fmt.Println(m.Text("at 10:45"))
	// Output: 10:45
}

func ExampleBudget() {
	subject := strings.Repeat("x", 40)
	for _, b := range []regex.Budget{{Steps: 1000}, {MaxStack: 16}} {
		_, err := regex.MustCompile(`(x+x+)+y`, regex.WithBudget(b)).Find(context.Background(), subject, 0)
		fmt.Println(err)
	}
	// Output:
	// regex: step budget exhausted
	// regex: backtrack stack exceeds the budget of 16 frames
}

func ExampleOption() {
	opts := []regex.Option{regex.WithFlavor(regex.Oniguruma), regex.WithFlags(regex.IgnoreCase)}
	m, _ := regex.MustCompile(`STRASSE`, opts...).Find(context.Background(), "straße", 0)
	fmt.Println(m.Text("straße"))
	// Output: straße
}

func ExamplePattern() {
	p := regex.MustCompile(`\w+`)
	for _, subject := range []string{"one", "two three"} {
		m, _ := p.Find(context.Background(), subject, 0)
		fmt.Println(m.Text(subject))
	}
	// Output:
	// one
	// two
}

func ExampleMatchOptions() {
	p := regex.MustCompile(`a*`)
	for _, opts := range []regex.MatchOptions{{Global: true}, {Global: true, SkipEmpty: true}} {
		var found []string
		for m, err := range p.FindAll(context.Background(), "ab", opts) {
			if err != nil {
				fmt.Println(err)
				return
			}
			found = append(found, m.Text("ab"))
		}
		fmt.Printf("%q\n", found)
	}
	// Output:
	// ["a" "" ""]
	// ["a"]
}

func ExampleMatch() {
	m, _ := regex.MustCompile(`ü+`).Find(context.Background(), "grüüß", 0)
	fmt.Println(m.Start, m.End, m.RuneStart, m.RuneEnd)
	// Output: 2 6 2 4
}

func ExampleGroup() {
	subject := "key=value"
	m, _ := regex.MustCompile(`(?<key>\w+)=(?<value>\w+)(;)?`).Find(context.Background(), subject, 0)
	for _, g := range m.Groups[1:] {
		fmt.Printf("%q %v %q\n", g.Name, g.Matched, g.Text(subject))
	}
	// Output:
	// "key" true "key"
	// "value" true "value"
	// "" false ""
}

func ExampleSyntaxError() {
	_, err := regex.Compile(`(a|b`)
	var se *regex.SyntaxError
	if errors.As(err, &se) {
		fmt.Println(se.Pos, se.Msg)
	}
	// Output: 0 missing closing parenthesis
}

func ExampleUnsupportedError() {
	_, err := regex.Compile(`\Gx`)
	var ue *regex.UnsupportedError
	if errors.As(err, &ue) {
		fmt.Println(ue.Construct)
	}
	// Output: assertion \G
}
