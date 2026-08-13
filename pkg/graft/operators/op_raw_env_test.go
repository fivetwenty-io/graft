package operators_test

import (
	"context"
	"testing"

	. "github.com/smartystreets/goconvey/convey"

	"github.com/fivetwenty-io/graft/pkg/graft"
)

// TestRawEnvOperator pins spruce's (( raw_env ... )) contract: a single
// $ENVVAR argument resolves to the variable's raw string value with no YAML
// type coercion, empty string included (spruce uses os.LookupEnv, so set-but-
// empty is a valid value, unlike (( grab $VAR ))). Non-env fallback branches
// after || DO get normal coercing evaluation - that asymmetry is spruce's
// contract - and errors are plain fmt strings without ansi markup (a
// deliberate spruce quirk, kept for byte parity).
func TestRawEnvOperator(t *testing.T) {
	evalDoc := func(yaml string) (graft.Document, error) {
		engine, err := graft.NewEngine()
		So(err, ShouldBeNil)
		doc, err := engine.ParseYAML([]byte(yaml))
		So(err, ShouldBeNil)
		return engine.Evaluate(context.Background(), doc)
	}

	Convey("(( raw_env ... ))", t, func() {
		Convey("returns the raw string value without YAML coercion", func() {
			t.Setenv("GRAFT_RAW_ENV_TEST_PORT", "8080")
			result, err := evalDoc("port: (( raw_env $GRAFT_RAW_ENV_TEST_PORT ))\n")
			So(err, ShouldBeNil)

			v, err := result.Get("port")
			So(err, ShouldBeNil)
			So(v, ShouldEqual, "8080") // string, not int
		})

		Convey("keeps boolean-looking and structured-looking values as raw strings", func() {
			t.Setenv("GRAFT_RAW_ENV_TEST_BOOL", "true")
			t.Setenv("GRAFT_RAW_ENV_TEST_MAP", "{a: 1}")

			result, err := evalDoc("b: (( raw_env $GRAFT_RAW_ENV_TEST_BOOL ))\nm: (( raw_env $GRAFT_RAW_ENV_TEST_MAP ))\n")
			So(err, ShouldBeNil)

			b, err := result.Get("b")
			So(err, ShouldBeNil)
			So(b, ShouldEqual, "true")

			m, err := result.Get("m")
			So(err, ShouldBeNil)
			So(m, ShouldEqual, "{a: 1}")
		})

		Convey("treats a set-but-empty variable as a valid empty string", func() {
			t.Setenv("GRAFT_RAW_ENV_TEST_EMPTY", "")
			result, err := evalDoc("v: (( raw_env $GRAFT_RAW_ENV_TEST_EMPTY ))\n")
			So(err, ShouldBeNil)

			v, err := result.Get("v")
			So(err, ShouldBeNil)
			So(v, ShouldEqual, "")
		})

		Convey("errors when the variable is not set", func() {
			_, err := evalDoc("v: (( raw_env $GRAFT_RAW_ENV_TEST_UNSET_XYZ ))\n")
			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring,
				"environment variable $GRAFT_RAW_ENV_TEST_UNSET_XYZ is not set")
		})

		Convey("falls back through || to another env var, still raw", func() {
			t.Setenv("GRAFT_RAW_ENV_TEST_SECOND", "42")
			result, err := evalDoc("v: (( raw_env $GRAFT_RAW_ENV_TEST_UNSET_XYZ || $GRAFT_RAW_ENV_TEST_SECOND ))\n")
			So(err, ShouldBeNil)

			v, err := result.Get("v")
			So(err, ShouldBeNil)
			So(v, ShouldEqual, "42") // raw string via the env-var branch
		})

		Convey("falls back through || to a literal, which IS coerced", func() {
			result, err := evalDoc("v: (( raw_env $GRAFT_RAW_ENV_TEST_UNSET_XYZ || 42 ))\n")
			So(err, ShouldBeNil)

			v, err := result.Get("v")
			So(err, ShouldBeNil)
			So(v, ShouldEqual, 42) // int: literal branch uses normal coercion
		})

		Convey("falls back through || to a reference, resolved normally", func() {
			result, err := evalDoc("defaults:\n  port: 5432\nv: (( raw_env $GRAFT_RAW_ENV_TEST_UNSET_XYZ || defaults.port ))\n")
			So(err, ShouldBeNil)

			v, err := result.Get("v")
			So(err, ShouldBeNil)
			So(v, ShouldEqual, 5432)
		})

		Convey("errors when the argument is not an environment variable", func() {
			_, err := evalDoc("other: value\nv: (( raw_env other ))\n")
			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring,
				"raw_env operator only accepts environment variable arguments")
		})

		Convey("errors when given more or fewer than one argument", func() {
			t.Setenv("GRAFT_RAW_ENV_TEST_A", "a")
			t.Setenv("GRAFT_RAW_ENV_TEST_B", "b")

			_, err := evalDoc("v: (( raw_env $GRAFT_RAW_ENV_TEST_A $GRAFT_RAW_ENV_TEST_B ))\n")
			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring,
				"raw_env operator requires exactly one argument")

			_, err = evalDoc("v: (( raw_env ))\n")
			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring,
				"raw_env operator requires exactly one argument")
		})
	})
}
