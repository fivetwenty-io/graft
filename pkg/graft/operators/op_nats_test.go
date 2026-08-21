package operators

import (
	"context"
	"testing"

	. "github.com/smartystreets/goconvey/convey"

	natsbackend "github.com/fivetwenty-io/graft/internal/backends/nats"
	"github.com/fivetwenty-io/graft/pkg/graft"
)

func TestParseNatsPath(t *testing.T) {
	Convey("parseNatsPath", t, func() {
		Convey("valid kv path", func() {
			storeType, storePath, err := natsbackend.ParsePath("kv:mybucket/mykey")
			So(err, ShouldBeNil)
			So(storeType, ShouldEqual, "kv")
			So(storePath, ShouldEqual, "mybucket/mykey")
		})

		Convey("valid obj path", func() {
			storeType, storePath, err := natsbackend.ParsePath("obj:mybucket/myobject")
			So(err, ShouldBeNil)
			So(storeType, ShouldEqual, "obj")
			So(storePath, ShouldEqual, "mybucket/myobject")
		})

		Convey("kv with uppercase is normalized", func() {
			storeType, storePath, err := natsbackend.ParsePath("KV:store/key")
			So(err, ShouldBeNil)
			So(storeType, ShouldEqual, "kv")
			So(storePath, ShouldEqual, "store/key")
		})

		Convey("obj with uppercase is normalized", func() {
			storeType, storePath, err := natsbackend.ParsePath("OBJ:bucket/object")
			So(err, ShouldBeNil)
			So(storeType, ShouldEqual, "obj")
			So(storePath, ShouldEqual, "bucket/object")
		})

		Convey("invalid format without colon", func() {
			_, _, err := natsbackend.ParsePath("mybucket/mykey")
			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "invalid NATS path format")
		})

		Convey("invalid store type", func() {
			_, _, err := natsbackend.ParsePath("invalid:bucket/key")
			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "invalid store type")
		})

		Convey("empty path after store type", func() {
			_, _, err := natsbackend.ParsePath("kv:")
			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "empty path after store type")
		})

		Convey("path with nested keys", func() {
			storeType, storePath, err := natsbackend.ParsePath("kv:config/app/settings/database")
			So(err, ShouldBeNil)
			So(storeType, ShouldEqual, "kv")
			So(storePath, ShouldEqual, "config/app/settings/database")
		})
	})
}

// TestNatsOperatorSkipMode pins both of WithSkipNats(true)'s two
// behaviors (plans/dennis-feedback-gaps.md's Item 3): by itself (the
// --skip-nats CLI flag's own default), it defers - leaves the operator's
// own "(( ... ))" expression intact; paired with WithRedact(true)
// (REDACT=1, or vaultinfo-style internal skips), it keeps graft's
// original "return the literal REDACTED sentinel" behavior instead.
func TestNatsOperatorSkipMode(t *testing.T) {
	Convey("NATS Operator Skip Mode", t, func() {
		Convey("when SkipNats is true via engine option alone (defer, the default)", func() {
			Convey("nats defers with its own expression intact", func() {
				engine, err := graft.NewEngine(graft.WithSkipNats(true))
				So(err, ShouldBeNil)

				yaml := []byte(`
config: (( nats "kv:mybucket/mykey" ))
`)
				doc, err := engine.ParseYAML(yaml)
				So(err, ShouldBeNil)

				result, err := engine.Evaluate(context.TODO(), doc)
				So(err, ShouldBeNil)

				config, err := result.Get("config")
				So(err, ShouldBeNil)
				So(config, ShouldEqual, `(( nats "kv:mybucket/mykey" ))`)
			})
		})

		Convey("when SkipNats and Redact are both true (REDACT=1 / vaultinfo-style)", func() {
			Convey("nats should return REDACTED", func() {
				engine, err := graft.NewEngine(graft.WithSkipNats(true), graft.WithRedact(true))
				So(err, ShouldBeNil)

				yaml := []byte(`
config: (( nats "kv:mybucket/mykey" ))
`)
				doc, err := engine.ParseYAML(yaml)
				So(err, ShouldBeNil)

				result, err := engine.Evaluate(context.TODO(), doc)
				So(err, ShouldBeNil)

				config, err := result.Get("config")
				So(err, ShouldBeNil)
				So(config, ShouldEqual, "REDACTED")
			})
		})
	})
}

func TestNatsOperatorPhase(t *testing.T) {
	Convey("NATS Operator Phase", t, func() {
		Convey("NatsOperator should be EvalPhase", func() {
			op := &NatsOperator{}
			So(op.Phase(), ShouldEqual, graft.EvalPhase)
		})
	})
}

func TestNatsOperatorSetup(t *testing.T) {
	Convey("NATS Operator Setup", t, func() {
		Convey("NatsOperator Setup should succeed", func() {
			op := &NatsOperator{}
			err := op.Setup()
			So(err, ShouldBeNil)
		})
	})
}

func TestNatsClientPoolExists(t *testing.T) {
	Convey("NATS Global Client Pool", t, func() {
		Convey("DefaultPool should be initialized", func() {
			// The global pool is initialized at package load time
			So(natsbackend.DefaultPool, ShouldNotBeNil)
		})
	})
}

// TestParseNatsConfig_DefaultAuthEnvVars proves parseNatsConfig's default
// (no-target) path reads each of NATS_TOKEN/USER/PASSWORD/NKEY/CREDS
// individually. One subtest per variable, asserting only that variable's
// field is set and every other auth field stays empty, so deleting any
// single env-var read (the adversarial review's first two mutations) or
// all five at once (its third mutation) fails a subtest instead of the
// whole table passing on partial coverage.
func TestParseNatsConfig_DefaultAuthEnvVars(t *testing.T) {
	pathArg := &graft.Expr{Type: graft.Literal, Literal: "kv:store/key"}

	cases := []struct {
		name   string
		envVar string
		value  string
		get    func(c *natsbackend.Config) string
	}{
		{"token", "NATS_TOKEN", "tok_xyz", func(c *natsbackend.Config) string { return c.Token }},
		{"user", "NATS_USER", "bob", func(c *natsbackend.Config) string { return c.User }},
		{"password", "NATS_PASSWORD", "hunter2", func(c *natsbackend.Config) string { return c.Password }},
		{"nkey", "NATS_NKEY", "/path/to/seed.nk", func(c *natsbackend.Config) string { return c.NkeySeedFile }},
		{"creds", "NATS_CREDS", "/path/to/user.creds", func(c *natsbackend.Config) string { return c.CredsFile }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(tc.envVar, tc.value)

			cfg, err := parseNatsConfig(nil, []*graft.Expr{pathArg})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if got := tc.get(cfg); got != tc.value {
				t.Errorf("%s: got %q, want %q (env var %s not read)", tc.name, got, tc.value, tc.envVar)
			}

			// Every other auth field must stay empty: proves this env var
			// maps to exactly its own field, not a neighboring one.
			for _, other := range cases {
				if other.name == tc.name {
					continue
				}
				if got := other.get(cfg); got != "" {
					t.Errorf("%s: expected %s to stay empty, got %q", tc.name, other.name, got)
				}
			}
		})
	}
}

// TestParseNatsConfig_DefaultAuthEnvVars_AllUnset confirms the default path
// leaves every auth field empty (anonymous connection) when none of the
// five variables are set - the baseline the table above diffs against.
func TestParseNatsConfig_DefaultAuthEnvVars_AllUnset(t *testing.T) {
	pathArg := &graft.Expr{Type: graft.Literal, Literal: "kv:store/key"}

	cfg, err := parseNatsConfig(nil, []*graft.Expr{pathArg})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Token != "" || cfg.User != "" || cfg.Password != "" || cfg.NkeySeedFile != "" || cfg.CredsFile != "" {
		t.Errorf("expected all auth fields empty, got Token=%q User=%q Password=%q NkeySeedFile=%q CredsFile=%q",
			cfg.Token, cfg.User, cfg.Password, cfg.NkeySeedFile, cfg.CredsFile)
	}
}

// TestParseNatsConfig_ConfigMapAuthKeys proves the inline second-argument
// config map's token/user/password/nkey_seed_file/creds_file keys are each
// read individually, mirroring TestParseNatsConfig_DefaultAuthEnvVars for
// the config-map branches (the review's fourth and fifth mutations:
// deleting the creds_file branch alone, and deleting all five at once).
func TestParseNatsConfig_ConfigMapAuthKeys(t *testing.T) {
	pathArg := &graft.Expr{Type: graft.Literal, Literal: "kv:store/key"}

	cases := []struct {
		name   string
		mapKey string
		value  string
		get    func(c *natsbackend.Config) string
	}{
		{"token", "token", "tok_xyz", func(c *natsbackend.Config) string { return c.Token }},
		{"user", "user", "bob", func(c *natsbackend.Config) string { return c.User }},
		{"password", "password", "hunter2", func(c *natsbackend.Config) string { return c.Password }},
		{"nkey_seed_file", "nkey_seed_file", "/path/to/seed.nk", func(c *natsbackend.Config) string { return c.NkeySeedFile }},
		{"creds_file", "creds_file", "/path/to/user.creds", func(c *natsbackend.Config) string { return c.CredsFile }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			configArg := &graft.Expr{
				Type:    graft.Literal,
				Literal: map[string]interface{}{tc.mapKey: tc.value},
			}

			cfg, err := parseNatsConfig(nil, []*graft.Expr{pathArg, configArg})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if got := tc.get(cfg); got != tc.value {
				t.Errorf("%s: got %q, want %q (config-map key %q not read)", tc.name, got, tc.value, tc.mapKey)
			}

			for _, other := range cases {
				if other.name == tc.name {
					continue
				}
				if got := other.get(cfg); got != "" {
					t.Errorf("%s: expected %s to stay empty, got %q", tc.name, other.name, got)
				}
			}
		})
	}
}

// TestParseNatsConfig_ConfigMapAuthKeys_AllUnset confirms an auth-free
// config map leaves every auth field empty, the baseline the table above
// diffs against.
func TestParseNatsConfig_ConfigMapAuthKeys_AllUnset(t *testing.T) {
	pathArg := &graft.Expr{Type: graft.Literal, Literal: "kv:store/key"}
	configArg := &graft.Expr{Type: graft.Literal, Literal: map[string]interface{}{"tls": true}}

	cfg, err := parseNatsConfig(nil, []*graft.Expr{pathArg, configArg})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Token != "" || cfg.User != "" || cfg.Password != "" || cfg.NkeySeedFile != "" || cfg.CredsFile != "" {
		t.Errorf("expected all auth fields empty, got Token=%q User=%q Password=%q NkeySeedFile=%q CredsFile=%q",
			cfg.Token, cfg.User, cfg.Password, cfg.NkeySeedFile, cfg.CredsFile)
	}
}
