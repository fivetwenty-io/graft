package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// exampleCase describes one runnable scenario under examples/. Files are
// merged in order (matching how the corresponding README, if any,
// documents running them); Prune mirrors the --prune flag some fixtures
// need to hide scratch keys the merge itself never removes. Dir, when
// set, is a directory (relative to examples/) the merge runs from so
// that any (( load "relative/path" )) in the fixture resolves the same
// way it does when a person runs `graft merge` by hand from that
// directory.
type exampleCase struct {
	name  string
	dir   string // relative to examples/, defaults to the files' own directory
	files []string
	prune []string
	env   map[string]string // environment variables the fixture's $VAR references need set

	// allowMarkers permits specific dotted paths in this case's merged
	// output to still contain a literal, unevaluated "((" marker start
	// (documented, intentional passthroughs -- e.g. an unrecognized
	// bare token inside (( )) that graft deliberately leaves as literal
	// text). The map value is the exact string the path must equal for
	// the allowance to apply; anything else at that path, or a marker
	// anywhere not listed here, fails the case. Cases named with a
	// "defer/" prefix skip the unresolved-marker check entirely: defer
	// deliberately re-emits its (( ... )) marker text by design.
	allowMarkers map[string]string

	// check, when set, pins specific values in this case's merged
	// output. It runs after both the merge and the unresolved-marker
	// check succeed.
	check func(t *testing.T, merged map[string]interface{})
}

// runnableExamples lists every examples/*.yml scenario confirmed to
// merge cleanly against the current binary. Each entry's exact file
// list and order was verified by hand; see
// .agents/work/20260811-implementation/examples-repair-notes.md for the
// classification of every file in the corpus (this list, the
// credentialDependentDirs skip-list below, and intentionalErrorCases).
func runnableExamples() []exampleCase {
	return []exampleCase{
		// --- standalone single-file fixtures ---
		{name: "arithmetic/basic", files: []string{"arithmetic/basic.yml"}},
		{name: "arithmetic/calculations", files: []string{"arithmetic/calculations.yml"}},
		{name: "arithmetic/percentage-scaling", files: []string{"arithmetic/percentage-scaling.yml"}},
		{name: "arithmetic/resource-calculations", files: []string{"arithmetic/resource-calculations.yml"}},
		{name: "base64/basic", files: []string{"base64/basic.yml"}},
		{name: "base64/credentials", files: []string{"base64/credentials.yml"}},
		{name: "base64/kubernetes-secrets", files: []string{"base64/kubernetes-secrets.yml"}},
		{name: "base64-decode/data-processing", files: []string{"base64-decode/data-processing.yml"}},
		{name: "boolean/access-control", files: []string{"boolean/access-control.yml"}},
		{name: "boolean/basic", files: []string{"boolean/basic.yml"}, check: func(t *testing.T, merged map[string]interface{}) {
			simple := asMap(asMap(merged["or_operations"])["simple"])
			if simple["false_or_true"] != true {
				t.Errorf("or_operations.simple.false_or_true = %v, want true", simple["false_or_true"])
			}
			health := asMap(asMap(merged["error_handling"])["health"])
			if health["has_failures"] != true {
				t.Errorf("error_handling.health.has_failures = %v, want true", health["has_failures"])
			}
			if health["system_status"] != "degraded" {
				t.Errorf("error_handling.health.system_status = %v, want degraded", health["system_status"])
			}
		}},
		{name: "boolean/feature-combinations", files: []string{"boolean/feature-combinations.yml"}},
		{name: "boolean/validation-rules", files: []string{"boolean/validation-rules.yml"}, check: func(t *testing.T, merged map[string]interface{}) {
			uv := asMap(merged["user_validation"])
			if uv["is_valid"] != true {
				t.Errorf("user_validation.is_valid = %v, want true", uv["is_valid"])
			}
		}},
		{name: "cartesian-product/basic", files: []string{"cartesian-product/basic.yml"}},
		{name: "cartesian-product/test-matrix", files: []string{"cartesian-product/test-matrix.yml"}},
		{name: "comparison/basic", files: []string{"comparison/basic.yml"}},
		{name: "comparison/conditional-resources", files: []string{"comparison/conditional-resources.yml"}, check: func(t *testing.T, merged map[string]interface{}) {
			redis := asMap(asMap(merged["cache"])["redis"])
			if redis["cluster_enabled"] != true {
				t.Errorf("cache.redis.cluster_enabled = %v, want true", redis["cluster_enabled"])
			}
		}},
		{name: "comparison/threshold-checks", files: []string{"comparison/threshold-checks.yml"}, check: func(t *testing.T, merged map[string]interface{}) {
			calculated := asMap(asMap(merged["monitoring"])["calculated"])
			got, ok := asFloat64(calculated["disk_percent"])
			if !ok || got != 90.0 {
				t.Errorf("monitoring.calculated.disk_percent = %v, want 90.0", calculated["disk_percent"])
			}
		}},
		{name: "comparison/version-comparisons", files: []string{"comparison/version-comparisons.yml"}},
		{name: "complex_defaults/basic_defaults", files: []string{"complex_defaults/basic_defaults.yaml"}},
		{name: "complex_defaults/nested_defaults", files: []string{"complex_defaults/nested_defaults.yaml"}},
		{name: "concat/basic", files: []string{"concat/basic.yml"}},
		{name: "concat/building-urls", files: []string{"concat/building-urls.yml"}},
		{name: "concat/with-grabs", files: []string{"concat/with-grabs.yml"}},
		{name: "defer/basic", files: []string{"defer/basic.yml"}},
		{name: "defer/template-generation", files: []string{"defer/template-generation.yml"}},
		{name: "empty/basic", files: []string{"empty/basic.yml"}},
		{name: "empty/complex-structures", files: []string{"empty/complex-structures.yml"}},
		{name: "empty/conditionals", files: []string{"empty/conditionals.yml"}},
		{name: "empty/defaults", files: []string{"empty/defaults.yml"}},
		{name: "empty/validation", files: []string{"empty/validation.yml"}},
		{name: "expression-operators/arithmetic", files: []string{"expression-operators/arithmetic.yml"}},
		{name: "expression-operators/boolean-logic", files: []string{"expression-operators/boolean-logic.yml"}},
		{name: "expression-operators/comparisons", files: []string{"expression-operators/comparisons.yml"}},
		{name: "grab/basic", files: []string{"grab/basic.yml"}, check: func(t *testing.T, merged map[string]interface{}) {
			app := asMap(merged["application"])
			if app["name"] != "MyApp" {
				t.Errorf("application.name = %v, want MyApp", app["name"])
			}
			if app["version"] != "1.2.3" {
				t.Errorf("application.version = %v, want 1.2.3", app["version"])
			}
		}},
		{name: "grab/lists-and-maps", files: []string{"grab/lists-and-maps.yml"}},
		{name: "grab/nested", files: []string{"grab/nested.yml"}},
		{name: "grab/with-defaults", files: []string{"grab/with-defaults.yml"}},
		{name: "grab/with-env-vars", files: []string{"grab/with-env-vars.yml"}, env: map[string]string{"ENV": "production", "REGION": "us-east-1"}},
		{name: "ips/basic", files: []string{"ips/basic.yml"}, check: func(t *testing.T, merged map[string]interface{}) {
			single := asMap(merged["single_ips"])
			if single["plus_5"] != "192.168.1.105" {
				t.Errorf("single_ips.plus_5 = %v, want 192.168.1.105", single["plus_5"])
			}
		}},
		{name: "ips/network-planning", files: []string{"ips/network-planning.yml"}},
		{name: "ips/service-allocation", files: []string{"ips/service-allocation.yml"}},
		{name: "join-maps/example", files: []string{"join-maps/example.yml"}},
		{name: "keys/basic", files: []string{"keys/basic.yml"}},
		{name: "keys/with-nested-grab", files: []string{"keys/with-nested-grab.yml"}},
		{name: "negate/basic", files: []string{"negate/basic.yml"}},
		{name: "negate/feature-flags", files: []string{"negate/feature-flags.yml"}},
		{name: "negate/inverse-config", files: []string{"negate/inverse-config.yml"}},
		{name: "negate/with-conditionals", files: []string{"negate/with-conditionals.yml"}},
		{name: "null/basic", files: []string{"null/basic.yml"}},
		{name: "null/conditional-config", files: []string{"null/conditional-config.yml"}, check: func(t *testing.T, merged map[string]interface{}) {
			flags := asMap(asMap(merged["application"])["feature_flags"])
			if flags["has_cache_provider"] != false {
				t.Errorf("application.feature_flags.has_cache_provider = %v, want false", flags["has_cache_provider"])
			}
		}},
		{name: "null/default-values", files: []string{"null/default-values.yml"}},
		{name: "null/validation", files: []string{"null/validation.yml"}, check: func(t *testing.T, merged map[string]interface{}) {
			employment := asMap(asMap(asMap(merged["form_validation"])["rules"])["employment"])
			if employment["income_valid"] != true {
				t.Errorf("form_validation.rules.employment.income_valid = %v, want true", employment["income_valid"])
			}
		}},
		{name: "shuffle/availability-zones", files: []string{"shuffle/availability-zones.yml"}},
		{name: "shuffle/basic", files: []string{"shuffle/basic.yml"}},
		{name: "shuffle/load-balancing", files: []string{"shuffle/load-balancing.yml"}},
		{name: "stringify/basic", files: []string{"stringify/basic.yml"}},
		{name: "stringify/kubernetes-configmap", files: []string{"stringify/kubernetes-configmap.yml"}},
		{name: "stringify/multi-document", files: []string{"stringify/multi-document.yml"}, allowMarkers: map[string]string{
			// documented in-file (see the comment above
			// documentation.markdown_docs in the fixture): this is a
			// generated markdown doc whose own body text shows the
			// graft syntax a reader would use inside a ```yaml code
			// fence -- a YAML block scalar is a literal string, never
			// evaluated, so this is intentional, illustrative content.
			"documentation.markdown_docs.api_gateway_md": "# API Gateway Configuration\n\n## Overview\nThe API Gateway service routes incoming requests to appropriate microservices.\n\n## Configuration\n```yaml\n(( stringify services.api_service ))\n```\n\n## Endpoints\n- Health Check: `GET /health`\n- User Service: `/api/v1/users/*`\n- Order Service: `/api/v1/orders/*`\n- Product Service: `/api/v1/products/*`\n",
		}},
		{name: "stringify/nested-configs", files: []string{"stringify/nested-configs.yml"}},
		{name: "ternary/basic", files: []string{"ternary/basic.yml"}, check: func(t *testing.T, merged map[string]interface{}) {
			co := asMap(merged["combined_operators"])
			got, ok := asFloat64(co["final_price"])
			if !ok || got != 80.0 {
				t.Errorf("combined_operators.final_price = %v, want 80.0", co["final_price"])
			}
		}},
		{name: "ternary/environment-config", files: []string{"ternary/environment-config.yml"}},
		{name: "ternary/feature-flags", files: []string{"ternary/feature-flags.yml"}},
		{name: "ternary/resource-sizing", files: []string{"ternary/resource-sizing.yml"}},
		{name: "unified-parser/basic-expressions", files: []string{"unified-parser/basic-expressions.yml"}},
		{name: "unified-parser/edge-cases", files: []string{"unified-parser/edge-cases.yml"}, allowMarkers: map[string]string{
			// documented in-file (see the comment above the key): an
			// unrecognized bare token inside (( )) that matches no
			// operator name is deliberately left as literal,
			// unevaluated text, byte-for-byte (BOSH's placeholder
			// grammar allows no interior whitespace, so it must not
			// be re-rendered with normalized spacing).
			"backwards_compat.bosh_var": "((some-variable))",
		}},
		{name: "new-features-demo", files: []string{"new-features-demo.yml"}},

		// --- multi-file merges: base + overlay(s), same key names ---
		{name: "availability-zones", files: []string{"availability-zones/networks.yml", "availability-zones/jobs.yml", "availability-zones/properties.yml"}},
		{name: "static-ips", files: []string{"static-ips/networks.yml", "static-ips/jobs.yml", "static-ips/properties.yml"}},
		{name: "basic", files: []string{"basic/main.yml", "basic/merge.yml"}},
		{name: "calc", files: []string{"calc/jobs.yml", "calc/meta.yml"}, prune: []string{"meta"}, check: func(t *testing.T, merged map[string]interface{}) {
			jobs := asList(merged["jobs"])
			if len(jobs) != 2 {
				t.Fatalf("jobs has %d entries, want 2", len(jobs))
			}
			if got, ok := asFloat64(asMap(jobs[0])["instances"]); !ok || got != 4 {
				t.Errorf("jobs[0].instances = %v, want 4", asMap(jobs[0])["instances"])
			}
			if got, ok := asFloat64(asMap(jobs[1])["instances"]); !ok || got != 1 {
				t.Errorf("jobs[1].instances = %v, want 1", asMap(jobs[1])["instances"])
			}
		}},
		{name: "delete", files: []string{"delete/main.yml", "delete/addon.yml"}},
		{name: "inserting", files: []string{"inserting/main.yml", "inserting/addon.yml"}},
		{name: "inject", files: []string{"inject/templates.yml", "inject/green.yml"}, prune: []string{"meta"}},
		{name: "inject/all-in-one", files: []string{"inject/all-in-one.yml"}, prune: []string{"meta"}},
		{name: "joining", files: []string{"joining/base.yml", "joining/meta.yml"}},
		{name: "key-removal", files: []string{"key-removal/original.yml", "key-removal/things.yml"}},
		{name: "list-of-maps", files: []string{"list-of-maps/original.yml", "list-of-maps/new.yml"}},
		{name: "map-replacement", files: []string{"map-replacement/original.yml", "map-replacement/delete.yml", "map-replacement/insert.yml"}},
		{name: "params", files: []string{"params/global.yml", "params/local.yml"}},
		{name: "pruning", files: []string{"pruning/base.yml", "pruning/networks.yml", "pruning/jobs.yml"}},
		{name: "sort/basic", files: []string{"sort/basic.yml", "sort/sort-overrides.yml"}},
		{name: "sort/named-entries", files: []string{"sort/named-entries.yml", "sort/named-entries-overrides.yml"}},

		// --- fixtures whose (( load "relative/path" )) needs examples/load
		// as the working directory, matching how a person would run them ---
		{name: "load/basic", dir: "load", files: []string{"basic.yml"}},
		{name: "load/dynamic-loading", dir: "load", files: []string{"dynamic-loading.yml"}},
		{name: "load/environment-specific", dir: "load", files: []string{"environment-specific.yml"}},
		{name: "load/modular-config", dir: "load", files: []string{"modular-config.yml"}},
	}
}

// credentialDependentDirs are examples/ subdirectories whose fixtures
// require live network services (Vault, AWS, NATS) to fully evaluate.
// Every operator call reaches a real client and errors ("Vault client
// initialization", "NoCredentialProviders", "no servers available for
// connection", or a target-not-configured message) without one; those
// errors are expected in this environment and are not asserted here.
// Each directory was manually verified to contain no OTHER class of
// error (parse errors, missing references, etc.) at repair time.
var credentialDependentDirs = []string{
	"aws",
	"aws-targets",
	// base64-decode: certificates.yml and secrets.yml decode values
	// sourced from (( vault ... ))/(( awsparam ... )) calls; without
	// live credentials those calls error first, and the base64-decode
	// of the never-resolved value then errors too — both error lines
	// land in the same merge error, and the credential-related one is
	// enough to satisfy the substring check below. basic.yml and
	// data-processing.yml in this same directory need no credentials
	// and are covered directly by runnableExamples() instead.
	"base64-decode",
	// complex_defaults: production_config.yaml reads secrets via
	// (( vault ... )); basic_defaults.yaml and nested_defaults.yaml in
	// this same directory need no credentials and are covered directly
	// by runnableExamples() instead.
	"complex_defaults",
	"nats",
	"nats-targets",
	"targets",
	"vault",
	"vault-defaults",
	"vault-migration",
	"vault-targets",
	"vault-try",
}

// intentionalErrorExamples demonstrate real error output on purpose
// (their surrounding docs/README describe them as error-handling demos)
// and are expected to fail.
var intentionalErrorExamples = []string{
	"error-handling/syntax-errors.yml",
}

// nonYAMLExampleDirs hold example programs (Go source, not graft
// fixtures) or setup scripts rather than merge-ready YAML; they are out
// of scope for this merge-success gate.
var nonYAMLExampleDirs = []string{
	"basic-usage",
	"config-management",
	"import-validation",
	"document_memory",
	"split", // covered by TestSplitExamples in split_examples_test.go
}

// TestExamplesRunSuccessfully is the regression gate for the examples/
// fixture corpus: every case in runnableExamples() must merge without
// error using the exact file list and order given. This is what keeps
// the corpus honest — a fixture that regresses to invalid or
// unsupported syntax fails this test.
func TestExamplesRunSuccessfully(t *testing.T) {
	examplesDir := examplesRootDir(t)

	for _, tc := range runnableExamples() {
		t.Run(tc.name, func(t *testing.T) {
			base := examplesDir
			if tc.dir != "" {
				base = filepath.Join(examplesDir, tc.dir)
			}

			restore := chdir(t, base)
			defer restore()

			if len(tc.env) > 0 {
				restoreEnv := setEnv(t, tc.env)
				defer restoreEnv()
			}

			paths := make([]string, len(tc.files))
			for i, f := range tc.files {
				if tc.dir != "" {
					// files are already relative to `dir`
					paths[i] = f
				} else {
					paths[i] = filepath.Join(examplesDir, f)
				}
			}

			files, err := openFiles(paths)
			if err != nil {
				t.Fatalf("open files: %s", err)
			}
			defer closeAll(files)

			merged, _, err := mergeAllDocs(files, &mergeOpts{Prune: tc.prune})
			if err != nil {
				t.Fatalf("expected %v to merge successfully, got error: %s", tc.files, err)
			}

			// defer/* deliberately re-emits its (( ... )) marker text
			// (that is the whole point of the operator); every other
			// fixture must fully evaluate, modulo tc.allowMarkers.
			if !strings.HasPrefix(tc.name, "defer/") {
				if bad := findUnresolvedMarkers(merged, "", tc.allowMarkers); len(bad) > 0 {
					t.Errorf("unevaluated (( )) marker(s) survived into merged output: %s", strings.Join(bad, "; "))
				}
			}

			if tc.check != nil {
				tc.check(t, merged)
			}
		})
	}
}

// findUnresolvedMarkers walks a merged document looking for string
// values that still contain a literal "((" marker start -- a sign an
// operator call was left unevaluated (degraded to inert text) instead
// of failing loudly or resolving. allowed maps a dotted path (map keys
// joined by ".", list indices rendered as ".[N]") to the exact string
// that path is permitted to hold; every other marker survivor is
// reported.
func findUnresolvedMarkers(v interface{}, path string, allowed map[string]string) []string {
	var found []string
	switch val := v.(type) {
	case map[string]interface{}:
		for k, sub := range val {
			childPath := k
			if path != "" {
				childPath = path + "." + k
			}
			found = append(found, findUnresolvedMarkers(sub, childPath, allowed)...)
		}
	case []interface{}:
		for i, sub := range val {
			found = append(found, findUnresolvedMarkers(sub, fmt.Sprintf("%s.[%d]", path, i), allowed)...)
		}
	case string:
		if contains(val, "((") {
			if want, ok := allowed[path]; ok && want == val {
				return nil
			}
			found = append(found, fmt.Sprintf("%s: %q", path, val))
		}
	}
	return found
}

// asFloat64 normalizes the numeric-ish types mergeAllDocs may produce
// (int, int64, float64) for value spot-checks that don't want to care
// which one a given operator happened to return.
func asFloat64(v interface{}) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	}
	return 0, false
}

// TestExamplesCredentialDependentDirsParse spot-checks that every
// fixture in the credential-dependent directories at least parses and
// reaches an operator (Vault/AWS/NATS client, or a not-configured
// target) rather than failing on unrelated syntax or reference errors.
// It does not assert success, since no live Vault/AWS/NATS is reachable
// in this environment.
func TestExamplesCredentialDependentDirsParse(t *testing.T) {
	if os.Getenv("GRAFT_TEST_CREDENTIAL_EXAMPLES") == "" {
		t.Skip("skipping: exercises real Vault/AWS/NATS client retry/backoff " +
			"(can take minutes with no reachable service); set " +
			"GRAFT_TEST_CREDENTIAL_EXAMPLES=1 to run it")
	}
	examplesDir := examplesRootDir(t)
	allowed := []string{
		"vault client initialization",
		"novalidproviders",
		"no valid providers in chain",
		"no servers available for connection",
		"target",
		"please provide",
		"please specify",
		"credentials",
	}

	for _, dir := range credentialDependentDirs {
		t.Run(dir, func(t *testing.T) {
			full := filepath.Join(examplesDir, dir)
			entries, err := os.ReadDir(full)
			if err != nil {
				t.Fatalf("read dir %s: %s", full, err)
			}
			restore := chdir(t, full)
			defer restore()

			for _, e := range entries {
				name := e.Name()
				if e.IsDir() || (!hasSuffix(name, ".yml") && !hasSuffix(name, ".yaml")) {
					continue
				}
				files, err := openFiles([]string{name})
				if err != nil {
					t.Fatalf("open %s: %s", name, err)
				}
				_, _, mergeErr := mergeAllDocs(files, &mergeOpts{})
				closeAll(files)
				if mergeErr == nil {
					// Some fixtures resolve fully offline (e.g. a
					// vault-try chain that only exercises literal
					// fallbacks); that is fine too.
					continue
				}
				msg := toLower(mergeErr.Error())
				if !containsAny(msg, allowed) {
					t.Errorf("%s: error does not look credential/target-related, needs review: %s", name, mergeErr)
				}
			}
		})
	}
}

// TestFileOperatorExamples covers examples/file/basic.yml and
// examples/file/dynamic-paths.yml, both of which read files (via
// (( file ... ))) that examples/file/setup.sh generates rather than
// files committed to the repo (see the "Generated by
// examples/file/setup.sh" entries in .gitignore). This test runs
// setup.sh itself, merges both fixtures, then removes the generated
// scripts/, configs/, and certificates/ directories so the working
// tree is unchanged afterward. This is why neither fixture appears in
// runnableExamples(): both require this generation step first, which
// runnableExamples() has no mechanism for.
func TestFileOperatorExamples(t *testing.T) {
	examplesDir := examplesRootDir(t)
	fileDir := filepath.Join(examplesDir, "file")

	restore := chdir(t, fileDir)
	defer restore()

	if out, err := exec.CommandContext(t.Context(), "bash", "setup.sh").CombinedOutput(); err != nil {
		t.Fatalf("examples/file/setup.sh failed: %s\n%s", err, out)
	}
	defer func() {
		for _, generated := range []string{"scripts", "configs", "certificates"} {
			if err := os.RemoveAll(generated); err != nil {
				t.Errorf("cleanup: remove %s: %s", generated, err)
			}
		}
	}()

	for _, name := range []string{"basic.yml", "dynamic-paths.yml"} {
		t.Run(name, func(t *testing.T) {
			files, err := openFiles([]string{name})
			if err != nil {
				t.Fatalf("open %s: %s", name, err)
			}
			defer closeAll(files)

			_, _, err = mergeAllDocs(files, &mergeOpts{})
			if err != nil {
				t.Fatalf("expected %s to merge successfully after setup.sh, got error: %s", name, err)
			}
		})
	}
}

// TestNonYAMLExampleDirsExist is a light sanity check on
// nonYAMLExampleDirs: if one of these directories is renamed or
// removed, this catches the list going stale rather than silently
// documenting directories that no longer exist.
func TestNonYAMLExampleDirsExist(t *testing.T) {
	examplesDir := examplesRootDir(t)
	for _, dir := range nonYAMLExampleDirs {
		t.Run(dir, func(t *testing.T) {
			full := filepath.Join(examplesDir, dir)
			info, err := os.Stat(full)
			if err != nil {
				t.Fatalf("stat %s: %s", full, err)
			}
			if !info.IsDir() {
				t.Fatalf("%s is not a directory", full)
			}
		})
	}
}

// TestExamplesIntentionalErrors confirms the deliberate error-handling
// demo still demonstrates a real, current error (not a stale one from a
// syntax the parser no longer rejects the same way).
func TestExamplesIntentionalErrors(t *testing.T) {
	examplesDir := examplesRootDir(t)
	for _, rel := range intentionalErrorExamples {
		t.Run(rel, func(t *testing.T) {
			path := filepath.Join(examplesDir, rel)
			files, err := openFiles([]string{path})
			if err != nil {
				t.Fatalf("open %s: %s", path, err)
			}
			defer closeAll(files)

			_, _, err = mergeAllDocs(files, &mergeOpts{})
			if err == nil {
				t.Fatalf("expected %s to demonstrate an error, but it merged successfully", rel)
			}
		})
	}
}

// --- helpers ---

func examplesRootDir(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %s", err)
	}
	// Tests run with cmd/graft as the working directory; the examples
	// corpus lives at the repo root.
	if filepath.Base(wd) == "graft" && filepath.Base(filepath.Dir(wd)) == "cmd" {
		wd = filepath.Dir(filepath.Dir(wd))
	}
	return filepath.Join(wd, "examples")
}

// chdir switches the process working directory to dir for the duration
// of a test and returns a restore func. Tests in this file are not run
// in parallel with t.Parallel(), so a shared process-wide chdir is safe
// here (matching the rest of this package's test helpers, e.g.
// setStdinFromFile).
func chdir(t *testing.T, dir string) func() {
	t.Helper()
	original, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %s", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir %s: %s", dir, err)
	}
	return func() {
		if err := os.Chdir(original); err != nil {
			t.Fatalf("restore chdir %s: %s", original, err)
		}
	}
}

// setEnv sets the given environment variables for the duration of a
// test and returns a restore func that puts each var back to its prior
// state (set or unset). Like chdir, this mutates shared process state,
// which is safe here because this file's tests are not run with
// t.Parallel().
func setEnv(t *testing.T, vars map[string]string) func() {
	t.Helper()
	type prior struct {
		val string
		set bool
	}
	priors := make(map[string]prior, len(vars))
	for k, v := range vars {
		pv, ok := os.LookupEnv(k)
		priors[k] = prior{val: pv, set: ok}
		if err := os.Setenv(k, v); err != nil {
			t.Fatalf("setenv %s: %s", k, err)
		}
	}
	return func() {
		for k, p := range priors {
			if p.set {
				_ = os.Setenv(k, p.val)
			} else {
				_ = os.Unsetenv(k)
			}
		}
	}
}

func closeAll(files []YamlFile) {
	for _, f := range files {
		if f.Reader != nil {
			_ = f.Reader.Close()
		}
	}
}

func hasSuffix(s, suffix string) bool {
	return len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix
}

func toLower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + ('a' - 'A')
		}
	}
	return string(b)
}

func containsAny(s string, substrs []string) bool {
	for _, sub := range substrs {
		if contains(s, sub) {
			return true
		}
	}
	return false
}

func contains(s, sub string) bool {
	if len(sub) == 0 {
		return true
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
