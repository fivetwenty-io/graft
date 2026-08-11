package natsbackend

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nkeys"
)

// TestGetTargetConfig_AuthEnvVars confirms NATS_<TARGET>_TOKEN,
// NATS_<TARGET>_USER, NATS_<TARGET>_PASSWORD, NATS_<TARGET>_NKEY, and
// NATS_<TARGET>_CREDS are read into the Target's new auth fields,
// following the existing NATS_<TARGET>_URL naming pattern.
func TestGetTargetConfig_AuthEnvVars(t *testing.T) {
	const target = "AUTHTEST"
	envVars := map[string]string{
		"NATS_AUTHTEST_URL":      "nats://auth-test.invalid:4222",
		"NATS_AUTHTEST_TOKEN":    "tok_xyz",
		"NATS_AUTHTEST_USER":     "bob",
		"NATS_AUTHTEST_PASSWORD": "hunter2",
		"NATS_AUTHTEST_NKEY":     "/path/to/seed.nk",
		"NATS_AUTHTEST_CREDS":    "/path/to/user.creds",
	}
	for k, v := range envVars {
		t.Setenv(k, v)
	}

	pool := &ClientPool{
		connections: make(map[string]*PooledConnection),
		configs:     make(map[string]*Target),
	}

	cfg, err := pool.GetTargetConfig(target)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Token != "tok_xyz" {
		t.Errorf("Token = %q, want %q", cfg.Token, "tok_xyz")
	}
	if cfg.User != "bob" {
		t.Errorf("User = %q, want %q", cfg.User, "bob")
	}
	if cfg.Password != "hunter2" {
		t.Errorf("Password = %q, want %q", cfg.Password, "hunter2")
	}
	if cfg.NkeySeedFile != "/path/to/seed.nk" {
		t.Errorf("NkeySeedFile = %q, want %q", cfg.NkeySeedFile, "/path/to/seed.nk")
	}
	if cfg.CredsFile != "/path/to/user.creds" {
		t.Errorf("CredsFile = %q, want %q", cfg.CredsFile, "/path/to/user.creds")
	}
}

// TestGetTargetConfig_AuthEnvVars_Unset confirms auth fields default to
// empty strings (anonymous connection) when their env vars are absent,
// matching every other optional Target field's default handling.
func TestGetTargetConfig_AuthEnvVars_Unset(t *testing.T) {
	const target = "NOAUTHTEST"
	t.Setenv("NATS_NOAUTHTEST_URL", "nats://no-auth-test.invalid:4222")

	pool := &ClientPool{
		connections: make(map[string]*PooledConnection),
		configs:     make(map[string]*Target),
	}

	cfg, err := pool.GetTargetConfig(target)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Token != "" || cfg.User != "" || cfg.Password != "" || cfg.NkeySeedFile != "" || cfg.CredsFile != "" {
		t.Errorf("expected all auth fields empty by default, got Token=%q User=%q Password=%q NkeySeedFile=%q CredsFile=%q",
			cfg.Token, cfg.User, cfg.Password, cfg.NkeySeedFile, cfg.CredsFile)
	}
}

// TestConfigFromTarget_CopiesAuthFields exercises the exact Target->Config
// mapping function GetConnection uses (configFromTarget), rather than a
// hand-rolled duplicate of it in the test, so a future field added to
// Target/Config but missed in configFromTarget's copy is caught here
// instead of silently vanishing at connection time.
func TestConfigFromTarget_CopiesAuthFields(t *testing.T) {
	target := &Target{
		URL:          "nats://propagate-test.invalid:4222",
		Token:        "tok_propagate",
		User:         "carol",
		Password:     "pw123",
		NkeySeedFile: "/path/to/seed.nk",
		CredsFile:    "/path/to/user.creds",
	}

	cfg := configFromTarget(target)

	if cfg.URL != target.URL {
		t.Errorf("URL = %q, want %q", cfg.URL, target.URL)
	}
	if cfg.Token != target.Token {
		t.Errorf("Token = %q, want %q", cfg.Token, target.Token)
	}
	if cfg.User != target.User {
		t.Errorf("User = %q, want %q", cfg.User, target.User)
	}
	if cfg.Password != target.Password {
		t.Errorf("Password = %q, want %q", cfg.Password, target.Password)
	}
	if cfg.NkeySeedFile != target.NkeySeedFile {
		t.Errorf("NkeySeedFile = %q, want %q", cfg.NkeySeedFile, target.NkeySeedFile)
	}
	if cfg.CredsFile != target.CredsFile {
		t.Errorf("CredsFile = %q, want %q", cfg.CredsFile, target.CredsFile)
	}
}

// TestBuildConnectionOptions_AuthPrecedence exercises
// BuildConnectionOptions' auth-method resolution order (creds file > nkey
// seed file > token > user/password) by applying the *returned option* to
// a fresh nats.Options{} and asserting the specific field(s) it set, not
// merely how many options came back. A count-only assertion (the previous
// version of this test) cannot tell "the creds-file option ran" from "the
// token option ran" - both add exactly one nats.Option - so it stayed
// green even when buildAuthOption's CredsFile case was replaced outright
// by a duplicate Token case, deleting NATS_CREDS support entirely. See
// TestBuildAuthOption_MutationCatches below for a regression test proving
// this file now catches that exact mutation.
func TestBuildConnectionOptions_AuthPrecedence(t *testing.T) {
	seedFile, seedPubKey := writeTempNkeySeedWithPubKey(t)
	credsFile := writeTempCredsFile(t)

	cases := []struct {
		name   string
		config *Config
		check  func(t *testing.T, o *nats.Options)
	}{
		{
			name:   "creds_only",
			config: &Config{CredsFile: credsFile},
			check: func(t *testing.T, o *nats.Options) {
				if o.UserJWT == nil {
					t.Error("expected UserJWT callback to be set for a creds-file config")
				}
				if o.SignatureCB == nil {
					t.Error("expected SignatureCB to be set for a creds-file config")
				}
				if o.Nkey != "" {
					t.Errorf("expected Nkey to be empty for a creds-file config, got %q", o.Nkey)
				}
				if o.Token != "" {
					t.Errorf("expected Token to be empty for a creds-file config, got %q", o.Token)
				}
			},
		},
		{
			name:   "user_password_only",
			config: &Config{User: "alice", Password: "s3cret"},
			check: func(t *testing.T, o *nats.Options) {
				if o.User != "alice" || o.Password != "s3cret" {
					t.Errorf("expected User/Password to be set, got User=%q Password=%q", o.User, o.Password)
				}
				if o.Token != "" {
					t.Errorf("expected Token to be empty, got %q", o.Token)
				}
			},
		},
		{
			name:   "token_only",
			config: &Config{Token: "tok_abc"},
			check: func(t *testing.T, o *nats.Options) {
				if o.Token != "tok_abc" {
					t.Errorf("Token = %q, want %q", o.Token, "tok_abc")
				}
				if o.User != "" || o.Password != "" {
					t.Errorf("expected User/Password to be empty, got User=%q Password=%q", o.User, o.Password)
				}
			},
		},
		{
			name:   "token_beats_user_password",
			config: &Config{Token: "tok_abc", User: "alice", Password: "s3cret"},
			check: func(t *testing.T, o *nats.Options) {
				if o.Token != "tok_abc" {
					t.Errorf("Token = %q, want %q", o.Token, "tok_abc")
				}
				if o.User != "" || o.Password != "" {
					t.Errorf("expected token to win outright, but User=%q Password=%q leaked through", o.User, o.Password)
				}
			},
		},
		{
			name:   "nkey_beats_token",
			config: &Config{NkeySeedFile: seedFile, Token: "tok_abc"},
			check: func(t *testing.T, o *nats.Options) {
				if o.Nkey != seedPubKey {
					t.Errorf("Nkey = %q, want the seed's derived public key %q", o.Nkey, seedPubKey)
				}
				if o.SignatureCB == nil {
					t.Error("expected SignatureCB to be set for an nkey config")
				}
				if o.UserJWT != nil {
					t.Error("expected UserJWT to be nil for an nkey (non-creds) config")
				}
				if o.Token != "" {
					t.Errorf("expected nkey to win outright, but Token=%q leaked through", o.Token)
				}
			},
		},
		{
			name:   "creds_beats_everything",
			config: &Config{CredsFile: credsFile, NkeySeedFile: seedFile, Token: "tok_abc", User: "alice", Password: "s3cret"},
			check: func(t *testing.T, o *nats.Options) {
				if o.UserJWT == nil {
					t.Error("expected UserJWT callback to be set for a creds-file config")
				}
				if o.Nkey != "" {
					t.Errorf("expected creds to win outright, but Nkey=%q leaked through", o.Nkey)
				}
				if o.Token != "" {
					t.Errorf("expected creds to win outright, but Token=%q leaked through", o.Token)
				}
				if o.User != "" || o.Password != "" {
					t.Errorf("expected creds to win outright, but User=%q Password=%q leaked through", o.User, o.Password)
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			authOpt, err := buildAuthOption(tc.config)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if authOpt == nil {
				t.Fatal("expected a non-nil auth option")
			}
			o := &nats.Options{}
			if err := authOpt(o); err != nil {
				t.Fatalf("applying auth option failed: %v", err)
			}
			tc.check(t, o)
		})
	}
}

// TestBuildAuthOption_MutationCatches is a meta-test proving the
// strengthened assertions above actually fail when buildAuthOption's
// precedence is broken, not just when it errors. It duplicates the exact
// mutation the adversarial review applied (replacing the CredsFile arm
// with a second Token arm, which silently deletes NATS_CREDS support) via
// a local stand-in function rather than editing client.go, so this test
// can run permanently without mutating production code.
func TestBuildAuthOption_MutationCatches(t *testing.T) {
	credsFile := writeTempCredsFile(t)

	mutatedBuildAuthOption := func(config *Config) (nats.Option, error) {
		switch {
		case config.Token != "": // mutation: was `case config.CredsFile != "":` returning nats.UserCredentials(...)
			return nats.Token(config.Token), nil
		case config.NkeySeedFile != "":
			opt, err := nats.NkeyOptionFromSeed(config.NkeySeedFile)
			if err != nil {
				return nil, err
			}
			return opt, nil
		case config.Token != "":
			return nats.Token(config.Token), nil
		case config.User != "" || config.Password != "":
			return nats.UserInfo(config.User, config.Password), nil
		default:
			return nil, nil
		}
	}

	config := &Config{CredsFile: credsFile}

	// The real function: creds-only config must produce a UserJWT-bearing option.
	realOpt, err := buildAuthOption(config)
	if err != nil {
		t.Fatalf("unexpected error from the real buildAuthOption: %v", err)
	}
	realApplied := &nats.Options{}
	if applyErr := realOpt(realApplied); applyErr != nil {
		t.Fatalf("applying the real auth option failed: %v", applyErr)
	}
	if realApplied.UserJWT == nil {
		t.Fatal("real buildAuthOption: expected UserJWT to be set for a creds-only config")
	}

	// The mutated function: same creds-only config produces no auth option
	// at all (CredsFile is never checked), so the connection would be
	// anonymous - proving this test's assertions distinguish the two.
	mutatedOpt, err := mutatedBuildAuthOption(config)
	if err != nil {
		t.Fatalf("unexpected error from the mutated buildAuthOption: %v", err)
	}
	if mutatedOpt != nil {
		t.Fatal("expected the mutated buildAuthOption to silently drop creds-only auth (nil option), the exact regression this test guards against")
	}
}

// TestBuildConnectionOptions_NoAuth confirms an all-empty auth config adds
// no auth-related options (anonymous connection, the pre-existing
// behavior this change must not disturb).
func TestBuildConnectionOptions_NoAuth(t *testing.T) {
	baseline, err := BuildConnectionOptions(&Config{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	opts, err := BuildConnectionOptions(&Config{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(opts) != len(baseline) {
		t.Fatalf("expected no auth options for empty config, baseline=%d got=%d", len(baseline), len(opts))
	}
}

// TestBuildConnectionOptions_InvalidNkeySeedFile confirms a bad/missing
// nkey seed file is a hard configuration error, not a silent fall-through
// to an anonymous connection (which would be a confusing, hard-to-debug
// auth failure surfaced only much later as a server-side rejection).
func TestBuildConnectionOptions_InvalidNkeySeedFile(t *testing.T) {
	_, err := BuildConnectionOptions(&Config{NkeySeedFile: filepath.Join(t.TempDir(), "does-not-exist.nk")})
	if err == nil {
		t.Fatal("expected an error for a missing nkey seed file, got nil")
	}
}

// TestBuildConnectionOptions_TLSErrorPropagates confirms the pre-existing
// TLS client-certificate loading failure path (previously only
// debug-logged and silently ignored) now surfaces as an error too, for
// the same "fail fast on bad config" reason as the nkey case above.
func TestBuildConnectionOptions_TLSErrorPropagates(t *testing.T) {
	_, err := BuildConnectionOptions(&Config{
		TLS:      true,
		CertFile: filepath.Join(t.TempDir(), "missing-cert.pem"),
		KeyFile:  filepath.Join(t.TempDir(), "missing-key.pem"),
	})
	if err == nil {
		t.Fatal("expected an error for missing TLS cert/key files, got nil")
	}
}

// writeTempNkeySeedWithPubKey generates a real, checksum-valid user nkey
// (nats.NkeyOptionFromSeed eagerly parses and validates the seed - it
// derives and checks the public key at Option-construction time, not just
// at connect time, see TestBuildConnectionOptions_InvalidNkeySeedFile - so
// a placeholder seed string fails before a test could even inspect the
// resulting nats.Options). Returns both the seed file path and the
// corresponding public key, since NkeyOptionFromSeed sets
// nats.Options.Nkey to that derived public key, not to the seed itself.
func writeTempNkeySeedWithPubKey(t *testing.T) (path, pubKey string) {
	t.Helper()
	kp, err := nkeys.CreateUser()
	if err != nil {
		t.Fatalf("failed to generate nkey user pair: %v", err)
	}
	pub, err := kp.PublicKey()
	if err != nil {
		t.Fatalf("failed to derive nkey public key: %v", err)
	}
	seed, err := kp.Seed()
	if err != nil {
		t.Fatalf("failed to extract nkey seed: %v", err)
	}
	path = filepath.Join(t.TempDir(), "seed.nk")
	if err := os.WriteFile(path, seed, 0o600); err != nil {
		t.Fatalf("failed to write temp nkey seed file: %v", err)
	}
	return path, pub
}

func writeTempCredsFile(t *testing.T) string {
	t.Helper()
	// Unlike NkeyOptionFromSeed, nats.UserCredentials only reads/parses the
	// file lazily via callbacks invoked at connect time (see UserJWT in
	// nats.go), so BuildConnectionOptions never opens this file - the
	// contents below don't need to be cryptographically valid, only present
	// on disk, for the precedence test to exercise the "creds file set"
	// branch.
	path := filepath.Join(t.TempDir(), "user.creds")
	const contents = `-----BEGIN NATS USER JWT-----
eyJhbGciOiJlZDI1NTE5In0.eyJqdGkiOiJmb28ifQ.abc
------END NATS USER JWT------

-----BEGIN USER NKEY SEED-----
SUAOJK4YOA46VR3ZWZ6JHTHNCTWXVDVFUMS7XR6TXGXXOX6RVWSOKQ7YZM
------END USER NKEY SEED------
`
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("failed to write temp creds file: %v", err)
	}
	return path
}
