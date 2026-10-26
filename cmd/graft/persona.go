package main

import (
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/fivetwenty-io/graft/log"
)

// persona is the tool a graft invocation speaks for. The graft persona
// is graft's own command line, and the others are the tools graft
// stands in for, whose argument rules and messages it follows when it
// runs under their names.
type persona int

// The personas.
const (
	personaGraft persona = iota
	personaSpruce
	personaJQ
	personaYQ
)

// personaFromArgv0 names the persona a binary runs under from the name it
// was invoked by. It takes the base name, lower-cases it, strips one
// trailing .exe, and compares the result with the exact names spruce, jq,
// and yq. Anything else, including an empty argv0, is the graft persona,
// so a versioned name such as jq-1.7 never claims to be an upstream
// build.
func personaFromArgv0(argv0 string) persona {
	base := strings.ToLower(filepath.Base(argv0))
	switch strings.TrimSuffix(base, ".exe") {
	case "spruce":
		return personaSpruce
	case "jq":
		return personaJQ
	case "yq":
		return personaYQ
	}
	return personaGraft
}

// queryInvocation reports whether a run is a jq or yq query run and which
// persona it speaks for. It is true under the jq and yq names, and under
// the graft name when the first argument that is not one of graft's own
// root flags is jq or yq. A spruce run is never a query run, because
// spruce has no such commands.
func queryInvocation(argv []string) (persona, bool) {
	if len(argv) == 0 {
		return personaGraft, false
	}
	p := personaFromArgv0(argv[0])
	if p == personaJQ || p == personaYQ {
		return p, true
	}
	if p != personaGraft {
		return personaGraft, false
	}
	rest := argv[1:]
	for len(rest) > 0 {
		verb := rest[0]
		if verb == "jq" || verb == "yq" {
			return p, true
		}
		n, ok := rootFlagWidth(verb)
		if !ok || n > len(rest) {
			break
		}
		rest = rest[n:]
	}
	return personaGraft, false
}

// rootFlagWidth reports how many arguments a leading graft root flag
// takes, counting the flag itself, and false when the argument is not one.
// A bare --color takes no value, because pflag reads one only after =.
func rootFlagWidth(arg string) (int, bool) {
	if name, ok := strings.CutPrefix(arg, "--"); ok {
		key, _, hasVal := strings.Cut(name, "=")
		takesValue, known := rootFlagTakesValue[key]
		if !known {
			return 0, false
		}
		if takesValue && !hasVal {
			return 2, true
		}
		return 1, true
	}
	if len(arg) > 1 && arg[0] == '-' && strings.Trim(arg[1:], "DTv") == "" {
		return 1, true
	}
	return 0, false
}

// newPersonaRoot builds the root command for a run under an alias name.
// The query command itself is the root, with the alias's usage line and
// version, and none of graft's persistent flags, which jq and yq
// scripts have no reason to know and which would collide with their own
// flags. It returns the same two values as newRootCmd. A persona with no
// query command yet gets graft's own root.
func newPersonaRoot(p persona) (*cobra.Command, *bool) {
	if p != personaJQ {
		return newRootCmd()
	}
	var aborted bool
	root := newJQCmdFor(personaJQ)
	root.Use = "jq [options] <jq filter> [file...]"
	root.Short = "jq - commandline JSON processor [graft " + Version + ", jq " + jqCompatVersion + " compatible]"
	root.Long = root.Short
	root.Version = "jq-" + jqCompatVersion
	root.SilenceUsage = true
	root.SilenceErrors = true
	// The engine options --merge needs come from the same config and
	// feature flags the graft root loads, so a bad GRAFT_* value fails
	// here as it does there.
	root.PersistentPreRunE = func(cmd *cobra.Command, _ []string) error {
		cfg, err := resolveStartupConfig("")
		if err != nil {
			aborted = true
			log.PrintStdErrf("%s\n", err.Error())
			exit(1)
			return err
		}
		keepStartupEngine(cmd, configEngineOpts(cfg, resolveStartupFeatureFlags()))
		// The theme is resolved from GRAFT_THEME and the config file
		// alone, and a bad value warns as it does under graft.
		_, _, warnings := resolveTheme(root, false, themeNameAuto)
		for _, w := range warnings {
			log.PrintStdErrf("%s\n", w)
		}
		return nil
	}
	return root, &aborted
}
