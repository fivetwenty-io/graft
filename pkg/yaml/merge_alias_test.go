package yaml_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/pkg/yaml"
)

// TestUnmarshalRefusesAnUnknownAliasAMergeDrops checks that every route
// refuses an alias with no anchor inside a merged map whose pair loses
// to an earlier one, as the route that runs hooks already did, and that
// the error never names the alias.
func TestUnmarshalRefusesAnUnknownAliasAMergeDrops(t *testing.T) {
	t.Parallel()
	const src = "m:\n  <<: [{n: 1}, {n: *s3kr1t}]\n"
	var generic any
	var plain map[string]map[string]int
	var hooked map[string]struct {
		N dupHook `yaml:"n"`
	}
	for _, v := range []any{&generic, &plain, &hooked} {
		err := yaml.Unmarshal([]byte(src), v)
		var de *yaml.DecodeError
		if !errors.As(err, &de) || de.Kind != yaml.ErrorKindSyntax || de.Line != 2 {
			t.Fatalf("%T: err = %v, want a syntax error on line 2", v, err)
		}
		if de.Msg != "could not find alias" {
			t.Errorf("%T: Msg = %q", v, de.Msg)
		}
		if strings.Contains(err.Error(), "s3kr1t") {
			t.Errorf("%T: error names the alias: %q", v, err)
		}
	}
}
