package jq

import (
	"errors"
	"io"
	"strings"

	"github.com/fivetwenty-io/graft/internal/query/runtime"
	"github.com/fivetwenty-io/graft/pkg/query/value"
	"github.com/fivetwenty-io/graft/pkg/regex"
)

// Option configures a program as Compile builds its Query. An option that
// cannot apply returns an error, and Compile returns that error.
type Option func(*config) error

// config is what the options collect. The zero config runs a program the
// way jq does with no flags.
type config struct {
	named       []namedArg
	positional  []*value.Value
	env         map[string]string
	modulePaths []string
	origin      string
	inputs      func() (*value.Value, error)
	filename    func() string
	stderr      io.Writer
	stderrSet   bool
	budget      regex.Budget
	depth       int
	numbers     runtime.NumberMode
}

type namedArg struct {
	name string
	val  *value.Value
}

// WithArg binds $name to v and adds it to $ARGS.named, as --arg and
// --argjson do. When a name is bound twice the first binding stands, as it
// does in jq. The names ARGS and ENV appear only under $ARGS.named, so
// $ARGS and $ENV keep their usual values.
func WithArg(name string, v *value.Value) Option {
	return func(c *config) error {
		for _, a := range c.named {
			if a.name == name {
				return nil
			}
		}
		c.named = append(c.named, namedArg{name: name, val: v})
		return nil
	}
}

// WithPositional appends vals to $ARGS.positional, as --args and
// --jsonargs do.
func WithPositional(vals []*value.Value) Option {
	return func(c *config) error {
		c.positional = append(c.positional, vals...)
		return nil
	}
}

// WithEnv replaces the process environment that $ENV and env read with
// env, whose entries are KEY=VALUE as os.Environ returns them. An entry
// with no = is skipped.
func WithEnv(env []string) Option {
	return func(c *config) error {
		c.env = make(map[string]string, len(env))
		for _, kv := range env {
			if k, v, ok := strings.Cut(kv, "="); ok {
				c.env[k] = v
			}
		}
		return nil
	}
}

// WithModulePaths sets the directories that import and include search, as
// -L does, and that get_search_list reports. A module name is looked up as
// NAME.jq, or as NAME/NAME.jq, in each directory in turn, and a data
// import looks for .json files the same way. An entry may start with ~/
// or $ORIGIN/, and one that is relative is taken from the directory of
// the module that imports. Without this option the directories are jq's
// defaults, which are ~/.jq, $ORIGIN/../lib/jq, and $ORIGIN/../lib.
func WithModulePaths(paths []string) Option {
	return func(c *config) error {
		c.modulePaths = append([]string(nil), paths...)
		return nil
	}
}

// WithProgramOrigin sets the directory that the main program's relative
// module paths resolve against, which jq takes from the program file given
// with -f and otherwise from the current directory. It is also what
// get_prog_origin reports.
func WithProgramOrigin(dir string) Option {
	return func(c *config) error {
		c.origin = dir
		return nil
	}
}

// WithInputs supplies the values that input and inputs read. next returns
// io.EOF when there are no more, and any other error ends the run with
// that error. A Decoder's Next is such a function.
func WithInputs(next func() (*value.Value, error)) Option {
	return func(c *config) error {
		c.inputs = next
		return nil
	}
}

// WithInputFilename supplies the name input_filename reports, which name
// returns each time the program asks. Without it input_filename is null.
func WithInputFilename(name func() string) Option {
	return func(c *config) error {
		c.filename = name
		return nil
	}
}

// WithStderr sends what debug and stderr write to w instead of os.Stderr.
// A nil w discards it.
func WithStderr(w io.Writer) Option {
	return func(c *config) error {
		c.stderr, c.stderrSet = w, true
		return nil
	}
}

// WithRegexBudget bounds every regular expression the program compiles and
// matches. A zero field keeps pkg/regex's default for that bound.
func WithRegexBudget(b regex.Budget) Option {
	return func(c *config) error {
		c.budget = b
		return nil
	}
}

// WithDepthLimit bounds how deeply the program's calls may nest. Zero keeps
// the default of 10,000, which is jq's, and a negative limit is an error.
func WithDepthLimit(n int) Option {
	return func(c *config) error {
		if n < 0 {
			return errors.New("the depth limit cannot be negative")
		}
		c.depth = n
		return nil
	}
}

// WithFloatNumbers computes with IEEE doubles, as jq 1.8.2 does, instead of
// exact decimals. It is what --float-numbers selects. Pair it with
// EncodeOptions.FloatNumbers to print results the way jq prints them.
func WithFloatNumbers() Option {
	return func(c *config) error {
		c.numbers = runtime.NumbersFloat
		return nil
	}
}
