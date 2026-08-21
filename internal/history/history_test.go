package history

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestTrack(t *testing.T) {
	Convey("Track", t, func() {
		Convey("a single LOAD step gives every path a one-entry history", func() {
			steps := []StepState{
				{Label: "base.yml", Phase: PhaseLoad, Data: map[string]interface{}{
					"database": map[string]interface{}{"host": "localhost"},
				}},
			}
			result, err := Track(steps)
			So(err, ShouldBeNil)
			So(len(result), ShouldEqual, 1)
			So(result[0].Path, ShouldEqual, "database.host")
			So(len(result[0].Entries), ShouldEqual, 1)
			So(result[0].Entries[0].Index, ShouldEqual, 0)
			So(result[0].Entries[0].Source, ShouldEqual, "base.yml")
			So(result[0].Entries[0].Phase, ShouldEqual, PhaseLoad)
			So(result[0].Entries[0].Value, ShouldEqual, "localhost")
			So(result[0].Final, ShouldEqual, "localhost")
			So(result[0].FinalOK, ShouldBeTrue)
		})

		Convey("a later MERGE step overwriting a path records a second entry and updates Final", func() {
			steps := []StepState{
				{Label: "base.yml", Phase: PhaseLoad, Data: map[string]interface{}{
					"database": map[string]interface{}{"host": "localhost", "timeout": 30},
				}},
				{Label: "env.yml", Phase: PhaseMerge, Data: map[string]interface{}{
					"database": map[string]interface{}{"host": "db.prod.example.com", "timeout": 30},
				}},
			}
			result, err := Track(steps)
			So(err, ShouldBeNil)

			byPath := indexByPath(result)
			host := byPath["database.host"]
			So(len(host.Entries), ShouldEqual, 2)
			So(host.Entries[0].Source, ShouldEqual, "base.yml")
			So(host.Entries[0].Value, ShouldEqual, "localhost")
			So(host.Entries[1].Source, ShouldEqual, "env.yml")
			So(host.Entries[1].Value, ShouldEqual, "db.prod.example.com")
			So(host.Final, ShouldEqual, "db.prod.example.com")

			timeout := byPath["database.timeout"]
			// Unchanged across the second step: only its LOAD entry.
			So(len(timeout.Entries), ShouldEqual, 1)
			So(timeout.Final, ShouldEqual, 30)
		})

		Convey("a path added only in a later step records a single entry at that step's index", func() {
			steps := []StepState{
				{Label: "base.yml", Phase: PhaseLoad, Data: map[string]interface{}{
					"database": map[string]interface{}{"host": "localhost"},
				}},
				{Label: "env.yml", Phase: PhaseMerge, Data: map[string]interface{}{
					"database": map[string]interface{}{"host": "localhost", "ssl": true},
				}},
			}
			result, err := Track(steps)
			So(err, ShouldBeNil)
			byPath := indexByPath(result)
			ssl := byPath["database.ssl"]
			So(len(ssl.Entries), ShouldEqual, 1)
			So(ssl.Entries[0].Index, ShouldEqual, 1)
			So(ssl.Entries[0].Source, ShouldEqual, "env.yml")
			So(ssl.Entries[0].Value, ShouldEqual, true)
			So(ssl.Final, ShouldEqual, true)
		})

		Convey("an EVAL step resolving an operator records an EVAL-phase entry and updates Final", func() {
			steps := []StepState{
				{Label: "base.yml", Phase: PhaseLoad, Data: map[string]interface{}{
					"database": map[string]interface{}{"password": `(( vault "secret/db:password" ))`},
				}},
				{Label: "<evaluated>", Phase: PhaseEval, Data: map[string]interface{}{
					"database": map[string]interface{}{"password": "resolved-secret"},
				}},
			}
			result, err := Track(steps)
			So(err, ShouldBeNil)
			byPath := indexByPath(result)
			pw := byPath["database.password"]
			So(len(pw.Entries), ShouldEqual, 2)
			So(pw.Entries[1].Source, ShouldEqual, "<evaluated>")
			So(pw.Entries[1].Phase, ShouldEqual, PhaseEval)
			So(pw.Entries[1].Value, ShouldEqual, "resolved-secret")
			So(pw.Final, ShouldEqual, "resolved-secret")
		})

		Convey("a POST step removing a path (prune) records a POST-phase removal entry and marks Final unavailable", func() {
			steps := []StepState{
				{Label: "base.yml", Phase: PhaseLoad, Data: map[string]interface{}{
					"meta":     map[string]interface{}{"internal": true},
					"database": map[string]interface{}{"host": "localhost"},
				}},
				{Label: "<pruned>", Phase: PhasePost, Data: map[string]interface{}{
					"database": map[string]interface{}{"host": "localhost"},
				}},
			}
			result, err := Track(steps)
			So(err, ShouldBeNil)
			byPath := indexByPath(result)
			internal := byPath["meta.internal"]
			So(len(internal.Entries), ShouldEqual, 2)
			So(internal.Entries[1].Source, ShouldEqual, "<pruned>")
			So(internal.Entries[1].Phase, ShouldEqual, PhasePost)
			So(internal.Entries[1].Value, ShouldBeNil)
			So(internal.FinalOK, ShouldBeFalse)

			host := byPath["database.host"]
			So(host.FinalOK, ShouldBeTrue)
			So(host.Final, ShouldEqual, "localhost")
		})

		Convey("results are sorted by path", func() {
			steps := []StepState{
				{Label: "base.yml", Phase: PhaseLoad, Data: map[string]interface{}{
					"zeta":  1,
					"alpha": 2,
				}},
			}
			result, err := Track(steps)
			So(err, ShouldBeNil)
			So(result[0].Path, ShouldEqual, "alpha")
			So(result[1].Path, ShouldEqual, "zeta")
		})

		Convey("no steps yields no history and no error", func() {
			result, err := Track(nil)
			So(err, ShouldBeNil)
			So(result, ShouldBeEmpty)
		})

		Convey("a literal top-level key containing a dot does not collide with an unrelated nested path of the same joined name (F8)", func() {
			steps := []StepState{
				// h1.yml: a literal top-level key "a.b" set to 1.
				{Label: "h1.yml", Phase: PhaseLoad, Data: map[string]interface{}{
					"a.b": 1,
				}},
				// h2.yml: an unrelated nested a -> b path set to 2. Does
				// NOT touch the literal "a.b" key at all.
				{Label: "h2.yml", Phase: PhaseMerge, Data: map[string]interface{}{
					"a.b": 1,
					"a":   map[string]interface{}{"b": 2},
				}},
			}
			result, err := Track(steps)
			So(err, ShouldBeNil)

			byPath := indexByPath(result)

			// The literal "a.b" key was never touched by h2.yml: one entry.
			literal, ok := byPath[`"a.b"`]
			So(ok, ShouldBeTrue)
			So(len(literal.Entries), ShouldEqual, 1)
			So(literal.Final, ShouldEqual, 1)

			// The nested a.b path was only added by h2.yml: one entry,
			// from h2.yml, distinct from the literal key above.
			nested, ok := byPath["a.b"]
			So(ok, ShouldBeTrue)
			So(len(nested.Entries), ShouldEqual, 1)
			So(nested.Entries[0].Source, ShouldEqual, "h2.yml")
			So(nested.Final, ShouldEqual, 2)
		})

		Convey("a nested key containing a dot one level down is disambiguated from a three-segment path", func() {
			steps := []StepState{
				{Label: "h.yml", Phase: PhaseLoad, Data: map[string]interface{}{
					"a": map[string]interface{}{"b.c": 1},
				}},
			}
			result, err := Track(steps)
			So(err, ShouldBeNil)
			So(len(result), ShouldEqual, 1)
			So(result[0].Path, ShouldEqual, `a."b.c"`)
			So(result[0].Final, ShouldEqual, 1)
		})

		Convey("an empty map value at a path is kept as its own leaf, not silently dropped", func() {
			steps := []StepState{
				{Label: "base.yml", Phase: PhaseLoad, Data: map[string]interface{}{
					"feature_flags": map[string]interface{}{},
				}},
			}
			result, err := Track(steps)
			So(err, ShouldBeNil)
			So(len(result), ShouldEqual, 1)
			So(result[0].Path, ShouldEqual, "feature_flags")
			So(result[0].Final, ShouldResemble, map[string]interface{}{})
		})

		Convey("a path removed by an operator (( prune )) during evaluation - not CLI --prune/--cherry-pick - is still marked Removed and Final is unavailable", func() {
			// No PhasePost step at all here: this is what buildMergeHistorySteps
			// produces for an operator (( prune )) with no --prune/--cherry-pick
			// CLI flag - the removal shows up as a PhaseEval step (the
			// evaluate() call that applies the operator's queued prune paths
			// before returning), not a synthetic PhasePost step.
			steps := []StepState{
				{Label: "base.yml", Phase: PhaseLoad, Data: map[string]interface{}{
					"secret":   "(( prune ))",
					"database": map[string]interface{}{"host": "localhost"},
				}},
				{Label: "<evaluated>", Phase: PhaseEval, Data: map[string]interface{}{
					"database": map[string]interface{}{"host": "localhost"},
				}},
			}
			result, err := Track(steps)
			So(err, ShouldBeNil)
			byPath := indexByPath(result)

			secret := byPath["secret"]
			So(len(secret.Entries), ShouldEqual, 2)
			So(secret.Entries[0].Value, ShouldEqual, "(( prune ))")
			last := secret.Entries[1]
			So(last.Phase, ShouldEqual, PhaseEval)
			So(last.Removed, ShouldBeTrue)
			So(last.Value, ShouldBeNil)
			// The entry's displayed source is rewritten to "<pruned>" (matching
			// the CLI-flag POST-step convention) rather than the generic
			// "<evaluated>" step label it technically came from, so a removal
			// this way reads identically to a --prune/--cherry-pick removal.
			So(last.Source, ShouldEqual, "<pruned>")
			So(secret.FinalOK, ShouldBeFalse)
			So(secret.Final, ShouldBeNil)
		})

		Convey("an explicit YAML null is never confused with a removal", func() {
			steps := []StepState{
				{Label: "base.yml", Phase: PhaseLoad, Data: map[string]interface{}{
					"optional": "set",
				}},
				{Label: "override.yml", Phase: PhaseMerge, Data: map[string]interface{}{
					"optional": nil,
				}},
			}
			result, err := Track(steps)
			So(err, ShouldBeNil)
			byPath := indexByPath(result)

			optional := byPath["optional"]
			last := optional.Entries[len(optional.Entries)-1]
			So(last.Removed, ShouldBeFalse)
			So(last.Value, ShouldBeNil)
			So(last.Source, ShouldEqual, "override.yml")
			So(optional.FinalOK, ShouldBeTrue)
			So(optional.Final, ShouldBeNil)
		})

		Convey("a step's explicit PrunedPaths marks a path Removed", func() {
			// A defensive/explicit signal: buildMergeHistorySteps attaches
			// the operator-queued prune paths it surfaced from the engine,
			// so Track does not have to rely solely on incidental Kind
			// classification to recognize a pruned path.
			steps := []StepState{
				{Label: "base.yml", Phase: PhaseLoad, Data: map[string]interface{}{
					"secret": "(( prune ))",
				}},
				{
					Label:       "<evaluated>",
					Phase:       PhaseEval,
					Data:        map[string]interface{}{},
					PrunedPaths: []string{"secret"},
				},
			}
			result, err := Track(steps)
			So(err, ShouldBeNil)
			byPath := indexByPath(result)
			secret := byPath["secret"]
			last := secret.Entries[len(secret.Entries)-1]
			So(last.Removed, ShouldBeTrue)
			So(secret.FinalOK, ShouldBeFalse)
		})
	})
}

func TestChangedPaths(t *testing.T) {
	Convey("ChangedPaths returns only paths with more than one entry", t, func() {
		steps := []StepState{
			{Label: "base.yml", Phase: PhaseLoad, Data: map[string]interface{}{
				"database": map[string]interface{}{"host": "localhost", "port": 5432},
			}},
			{Label: "env.yml", Phase: PhaseMerge, Data: map[string]interface{}{
				"database": map[string]interface{}{"host": "prod", "port": 5432},
			}},
		}
		result, err := Track(steps)
		So(err, ShouldBeNil)

		changed := ChangedPaths(result)
		So(len(changed), ShouldEqual, 1)
		So(changed[0].Path, ShouldEqual, "database.host")
	})
}

func indexByPath(result []PathHistory) map[string]PathHistory {
	m := make(map[string]PathHistory, len(result))
	for _, r := range result {
		m[r.Path] = r
	}
	return m
}
