package yq

import (
	"errors"
	"io"
	"strings"

	"github.com/fivetwenty-io/graft/internal/query/runtime"
	"github.com/fivetwenty-io/graft/pkg/regex"
)

// config is what the options collect. Compile starts from yq's own
// defaults, which are environment and file operations on, the system
// operator off, string interpolation on, and header preprocessing on.
type config struct {
	env         map[string]string
	envSet      bool
	caps        runtime.Capabilities
	numbers     runtime.NumberMode
	interpolate bool
	headerPre   bool
	mergeFix    bool
	stderr      io.Writer
	stderrSet   bool
	regexBudget regex.Budget
	resolve     bool
	depth       int
}

// defaultConfig is the configuration of a bare yq run with no flags.
func defaultConfig() config {
	return config{
		caps:        runtime.Capabilities{Env: true, Files: true},
		numbers:     runtime.NumbersExact,
		interpolate: true,
		headerPre:   true,
	}
}

// Option configures an expression as Compile builds its Evaluator. An
// option that cannot apply returns an error, and Compile returns that
// error. The type it acts on is private, so the options this package
// provides are the only ones.
type Option func(*config) error

// WithEnv replaces the process environment that env, strenv, and envsubst
// read with env, whose entries are KEY=VALUE as os.Environ returns them.
// An entry with no = is skipped. A nil or empty env gives an empty
// environment, not the process environment, so omit the option to keep the
// process environment.
func WithEnv(env []string) Option {
	return func(c *config) error {
		c.env = make(map[string]string, len(env))
		for _, kv := range env {
			if k, v, ok := strings.Cut(kv, "="); ok {
				c.env[k] = v
			}
		}
		c.envSet = true
		return nil
	}
}

// WithEnvOps turns the operators that read the environment on or off.
// They are on by default, and WithEnvOps(false) is yq's
// --security-disable-env-ops. With them off, env, strenv, and envsubst
// fail with a *query.RuntimeError, and so does a nested evaluation.
func WithEnvOps(enabled bool) Option {
	return func(c *config) error {
		c.caps.Env = enabled
		return nil
	}
}

// WithFileOps turns the operators that read files on or off. They are on by
// default, and WithFileOps(false) is yq's --security-disable-file-ops.
// With them off, load, load_str, strload, and load_base64 fail with a
// *query.RuntimeError, and so does a file that one of them loads, because
// a nested evaluation never regains a capability its parent lacks.
func WithFileOps(enabled bool) Option {
	return func(c *config) error {
		c.caps.Files = enabled
		return nil
	}
}

// WithSystemOps records whether the system operator is enabled, as yq's
// --security-enable-system-operator does. It is off by default. This
// engine has no system operator, so the setting changes nothing. The option
// exists so that a program written for yq, which passes the flag, ports
// without a change, and so that the setting has a place to land should the
// operator ever be added.
func WithSystemOps(enabled bool) Option {
	return func(c *config) error {
		c.caps.System = enabled
		return nil
	}
}

// WithFloatNumbers computes with IEEE doubles instead of exact decimals,
// as --float-numbers does. Pair it with the encoder's number printing to
// get the doubles yq prints.
func WithFloatNumbers() Option {
	return func(c *config) error {
		c.numbers = runtime.NumbersFloat
		return nil
	}
}

// WithStringInterpolation turns string interpolation, the "\(expr)" form,
// on or off. It is on by default, and WithStringInterpolation(false) is
// yq's --string-interpolation=false, which leaves "\(" in a string as the
// two characters it is written with.
func WithStringInterpolation(enabled bool) Option {
	return func(c *config) error {
		c.interpolate = enabled
		return nil
	}
}

// WithHeaderPreprocess sets whether the comments before a document's first
// node are kept apart from it. It is on by default, and
// WithHeaderPreprocess(false) is yq's --header-preprocess=false, which makes
// from_yaml leave those comments on the node. It does not change how a
// Decoder reads a source, which takes DecodeOptions.NoHeaderPreprocess.
func WithHeaderPreprocess(enabled bool) Option {
	return func(c *config) error {
		c.headerPre = enabled
		return nil
	}
}

// WithMergeAnchorFix makes a merge key follow the YAML merge-key
// specification, as yq's --yaml-fix-merge-anchor-to-spec does. It is off
// by default.
func WithMergeAnchorFix(enabled bool) Option {
	return func(c *config) error {
		c.mergeFix = enabled
		return nil
	}
}

// WithStderr sends what debug writes to w instead of os.Stderr. A nil w
// discards it.
func WithStderr(w io.Writer) Option {
	return func(c *config) error {
		c.stderr, c.stderrSet = w, true
		return nil
	}
}

// WithResolvedAliases makes every output read as it does in a format with
// no aliases. Each alias is replaced by the node its anchor marks, and
// each merge key is folded into its map. It is what JSON output needs.
func WithResolvedAliases() Option {
	return func(c *config) error {
		c.resolve = true
		return nil
	}
}

// WithRegexBudget bounds every regular expression the expression compiles
// and matches. A zero field keeps pkg/regex's default for that bound.
func WithRegexBudget(b regex.Budget) Option {
	return func(c *config) error {
		c.regexBudget = b
		return nil
	}
}

// WithDepthLimit bounds how deeply the expression's calls may nest. Zero
// keeps the default of 10,000, and a negative limit is an error.
func WithDepthLimit(n int) Option {
	return func(c *config) error {
		if n < 0 {
			return errors.New("the depth limit cannot be negative")
		}
		c.depth = n
		return nil
	}
}
