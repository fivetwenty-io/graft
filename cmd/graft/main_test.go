package main

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/goccy/go-yaml"
	"github.com/mattn/go-isatty"
	. "github.com/smartystreets/goconvey/convey"

	"github.com/fivetwenty-io/graft/internal/config"
	"github.com/fivetwenty-io/graft/internal/features"
	"github.com/fivetwenty-io/graft/internal/utils/ansi"
	"github.com/fivetwenty-io/graft/log"
	"github.com/fivetwenty-io/graft/pkg/graft"
)

//nolint:gochecknoinits // Test setup requires init to configure test environment before tests run
func init() {
	// Disable ANSI colors for tests to avoid escape codes in assertions
	ansi.Color(false)
}

func openFiles(paths []string) ([]YamlFile, error) {
	files := []YamlFile{}
	for _, file := range paths {
		f, err := os.Open(file)
		if err != nil {
			return files, err
		}
		files = append(files, YamlFile{Path: file, Reader: f})
	}
	return files, nil
}

// setStdinFromFile points os.Stdin at path's contents for the duration of a
// test, simulating genesis's `cat file | graft ... -` and `graft ... < file`
// invocation patterns. It returns a restore func that must be called (via
// defer) to put the real os.Stdin back, so later tests aren't affected.
func setStdinFromFile(t *testing.T, path string) func() {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("setStdinFromFile: open %s: %s", path, err)
	}
	original := os.Stdin
	os.Stdin = f
	return func() {
		os.Stdin = original
		_ = f.Close()
	}
}

// genesisVersionRegex mirrors genesis's check_prereqs() version probe regex
// (lib/Genesis/Commands.pm): qr(.*version\s+(\S+).*)i.
var genesisVersionRegex = regexp.MustCompile(`(?i).*version\s+(\S+).*`)

// minGenesisVersion is genesis's minimum spruce-compatible version gate.
const minGenesisVersion = "1.28.0"

// semverAtLeast reports whether version's major.minor.patch numeric prefix
// is >= min's. It only compares the leading dotted numeric prefix (ignoring
// any leading 'v' and any pre-release/build metadata suffix), which is
// sufficient for genesis's minimum-version gate check.
func semverAtLeast(version, minVersion string) bool {
	parse := func(v string) ([3]int, bool) {
		v = strings.TrimPrefix(strings.TrimSpace(v), "v")
		// Strip anything after the first run of digits/dots (pre-release,
		// build metadata, or non-numeric suffixes like "(development)").
		end := 0
		for end < len(v) && (v[end] == '.' || (v[end] >= '0' && v[end] <= '9')) {
			end++
		}
		numeric := v[:end]
		parts := strings.Split(numeric, ".")
		var out [3]int
		for i := 0; i < 3 && i < len(parts); i++ {
			n, err := strconv.Atoi(parts[i])
			if err != nil {
				return out, false
			}
			out[i] = n
		}
		return out, len(parts) > 0 && numeric != ""
	}

	got, ok := parse(version)
	if !ok {
		return false
	}
	want, ok := parse(minVersion)
	if !ok {
		return false
	}
	for i := 0; i < 3; i++ {
		if got[i] != want[i] {
			return got[i] > want[i]
		}
	}
	return true
}

func TestParseYAML(t *testing.T) {
	Convey("parseYAML()", t, func() {
		Convey("returns error for invalid yaml data", func() {
			data := `
asdf: fdsa
- asdf: fdsa
`
			obj, err := parseYAML([]byte(data))
			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "Root of YAML document is not a hash/map:")
			So(obj, ShouldBeNil)
		})
		Convey("does not return error if yaml is empty", func() {
			data := `---
`
			obj, err := parseYAML([]byte(data))
			So(err, ShouldBeNil)
			So(obj, ShouldNotBeNil)
		})
		Convey("returns error if yaml is a bool", func() {
			data := `
true
`
			obj, err := parseYAML([]byte(data))
			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "Root of YAML document is not a hash/map:")
			So(obj, ShouldBeNil)
		})
		Convey("returns error if yaml is a string", func() {
			data := `
"1234"
`
			obj, err := parseYAML([]byte(data))
			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "Root of YAML document is not a hash/map:")
			So(obj, ShouldBeNil)
		})
		Convey("returns error if yaml is a number", func() {
			data := `
1234
`
			obj, err := parseYAML([]byte(data))
			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "Root of YAML document is not a hash/map:")
			So(obj, ShouldBeNil)
		})
		Convey("returns error if yaml an array", func() {
			data := `
- 1
- 2
`
			obj, err := parseYAML([]byte(data))
			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "Root of YAML document is not a hash/map:")
			So(obj, ShouldBeNil)
		})
		Convey("returns expected datastructure from valid yaml", func() {
			data := `
top:
  subarray:
  - one
  - two
`
			obj, err := parseYAML([]byte(data))
			expect := map[string]interface{}{
				"top": map[string]interface{}{
					"subarray": []interface{}{"one", "two"},
				},
			}
			So(obj, ShouldResemble, expect)
			So(err, ShouldBeNil)
		})
	})
}

func TestMergeAllDocs(t *testing.T) {
	Convey("mergeAllDocs()", t, func() {
		Convey("Fails with readFile error on bad first doc", func() {
			files, err := openFiles([]string{"../../assets/merge/second.yml"})
			_ = files[0].Reader.Close()
			So(err, ShouldBeNil)
			_, _, err = mergeAllDocs(files, &mergeOpts{})
			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "Error reading file ../../assets/merge/second.yml:")
		})
		Convey("Fails with parseYAML error on bad second doc", func() {
			files, err := openFiles([]string{"../../assets/merge/first.yml", "../../assets/merge/bad.yml"})
			So(err, ShouldBeNil)
			_, _, err = mergeAllDocs(files, &mergeOpts{})
			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "../../assets/merge/bad.yml: root of YAML document is not a hash/map")
		})
		Convey("Fails with mergeMap error", func() {
			files, err := openFiles([]string{"../../assets/merge/first.yml", "../../assets/merge/error.yml"})
			So(err, ShouldBeNil)
			_, _, err = mergeAllDocs(files, &mergeOpts{})
			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "$.array_inline.0: new object is a string, not a map - cannot merge by key")
		})
		//nolint:dupl // Test cases intentionally have similar structure with different file types
		Convey("Succeeds with valid files + yaml", func() {
			expect := map[string]interface{}{
				"key":           "overridden",
				"array_append":  []interface{}{"one", "two", "three"},
				"array_prepend": []interface{}{"three", "four", "five"},
				"array_replace": []interface{}{[]interface{}{1, 2, 3}},
				"array_inline": []interface{}{
					map[string]interface{}{"name": "first_elem", "val": "overwritten"},
					"second_elem was overwritten",
					"third elem is appended",
				},
				"array_default": []interface{}{
					"FIRST",
					"SECOND",
					"third",
				},
				"array_map_default": []interface{}{
					map[string]interface{}{
						"name": "AAA",
						"k1":   "key 1",
						"k2":   "updated",
					},
					map[string]interface{}{
						"name": "BBB",
						"k2":   "final",
						"k3":   "original",
					},
				},
				"map": map[string]interface{}{
					"key":  "value",
					"key2": "val2",
				},
			}
			files, err := openFiles([]string{"../../assets/merge/first.yml", "../../assets/merge/second.yml"})
			So(err, ShouldBeNil)
			result, _, err := mergeAllDocs(files, &mergeOpts{})
			So(err, ShouldBeNil)
			So(result, ShouldResemble, expect)
		})
		//nolint:dupl // Test cases intentionally have similar structure with different file types
		Convey("Succeeds with valid files + json", func() {
			expect := map[string]interface{}{
				"key":           "overridden",
				"array_append":  []interface{}{"one", "two", "three"},
				"array_prepend": []interface{}{"three", "four", "five"},
				"array_replace": []interface{}{[]interface{}{1, 2, 3}},
				"array_inline": []interface{}{
					map[string]interface{}{"name": "first_elem", "val": "overwritten"},
					"second_elem was overwritten",
					"third elem is appended",
				},
				"array_default": []interface{}{
					"FIRST",
					"SECOND",
					"third",
				},
				"array_map_default": []interface{}{
					map[string]interface{}{
						"name": "AAA",
						"k1":   "key 1",
						"k2":   "updated",
					},
					map[string]interface{}{
						"name": "BBB",
						"k2":   "final",
						"k3":   "original",
					},
				},
				"map": map[string]interface{}{
					"key":  "value",
					"key2": "val2",
				},
			}
			files, err := openFiles([]string{"../../assets/merge/first.json", "../../assets/merge/second.yml"})
			So(err, ShouldBeNil)
			result, _, err := mergeAllDocs(files, &mergeOpts{})
			So(err, ShouldBeNil)
			So(result, ShouldResemble, expect)
		})
		Convey("Blank/comment-only/null documents merge as {} no-ops (spruce parity)", func() {
			files, err := openFiles([]string{"../../assets/merge/first.yml", "../../assets/merge/empty.yml"})
			So(err, ShouldBeNil)
			withEmpty, _, err := mergeAllDocs(files, &mergeOpts{})
			So(err, ShouldBeNil)

			files, err = openFiles([]string{"../../assets/merge/first.yml"})
			So(err, ShouldBeNil)
			withoutEmpty, _, err := mergeAllDocs(files, &mergeOpts{})
			So(err, ShouldBeNil)

			So(withEmpty, ShouldResemble, withoutEmpty)
		})
		Convey("A lone blank document merges to an empty map, not a panic", func() {
			files, err := openFiles([]string{"../../assets/merge/empty.yml"})
			So(err, ShouldBeNil)
			result, _, err := mergeAllDocs(files, &mergeOpts{})
			So(err, ShouldBeNil)
			So(result, ShouldResemble, map[string]interface{}{})
		})
	})
}

func TestMain(t *testing.T) {
	Convey("main()", t, func() {
		var stdout string
		printStdOutf = func(format string, args ...interface{}) {
			// Append (not overwrite): commands like `json` on multi-doc
			// input call printStdOutf once per output line, matching
			// production's Fprintf-to-stdout behavior (see TestFan's
			// override, which uses the same append pattern).
			stdout += fmt.Sprintf(format, args...)
		}
		var stderr string
		// Edit log stderr function
		log.PrintStdErrf = func(format string, args ...interface{}) {
			stderr += fmt.Sprintf(format, args...)
		}

		rc := 256 // invalid return code to catch any issues
		exit = func(code int) {
			rc = code
		}

		usage = func() {
			stderr = "usage was called"
			exit(1)
		}

		Convey("Should output usage if bad args are passed", func() {
			os.Args = []string{"graft", "fdsafdada"}
			stdout = ""
			stderr = ""
			main()
			So(stderr, ShouldEqual, "usage was called")
			So(rc, ShouldEqual, 1)
		})
		Convey("Should output usage if no args at all", func() {
			os.Args = []string{"graft"}
			stdout = ""
			stderr = ""
			main()
			So(stderr, ShouldEqual, "usage was called")
			So(rc, ShouldEqual, 1)
		})
		Convey("Should error if no args to merge and no files listed", func() {
			os.Args = []string{"graft", "merge"}
			stdout = ""
			stderr = ""
			main()
			So(stderr, ShouldEqual, "Error reading STDIN: no data found. Did you forget to pipe data to STDIN, or specify yaml files to merge?\n")
			So(rc, ShouldEqual, 2)
		})
		Convey("Should output version", func() {
			Convey("When '-v' is specified", func() {
				os.Args = []string{"graft", "-v"}
				stdout = ""
				stderr = ""
				main()
				So(stdout, ShouldStartWith, fmt.Sprintf("graft - Version %s", Version))
				So(stderr, ShouldEqual, "")
				So(rc, ShouldEqual, 0)

				matches := genesisVersionRegex.FindStringSubmatch(stdout)
				So(matches, ShouldNotBeNil)
				So(semverAtLeast(matches[1], minGenesisVersion), ShouldBeTrue)
			})
			Convey("When '--version' is specified", func() {
				os.Args = []string{"graft", "--version"}
				stdout = ""
				stderr = ""
				main()
				So(stdout, ShouldStartWith, fmt.Sprintf("graft - Version %s", Version))
				So(stderr, ShouldEqual, "")
				So(rc, ShouldEqual, 0)

				matches := genesisVersionRegex.FindStringSubmatch(stdout)
				So(matches, ShouldNotBeNil)
				So(semverAtLeast(matches[1], minGenesisVersion), ShouldBeTrue)
			})
			Convey("When '-v' precedes a subcommand", func() {
				// spruce checks its Version flag before dispatching any
				// verb: `spruce -v merge whatever` prints the version and
				// exits 0 without touching the files. The nonexistent file
				// below proves dispatch never happens.
				os.Args = []string{"graft", "-v", "merge", "does-not-exist.yml"}
				stdout = ""
				stderr = ""
				main()
				So(stdout, ShouldStartWith, fmt.Sprintf("graft - Version %s", Version))
				So(stderr, ShouldEqual, "")
				So(rc, ShouldEqual, 0)
			})
			Convey("When '-v' follows a subcommand", func() {
				// Post-verb -v is ignored and the verb runs (spruce
				// instead treats it as a filename and exits 2; the
				// divergence is documented in the compat contract).
				// Pinned so the version flag never short-circuits a
				// scripted merge whose stdout becomes a manifest.
				os.Args = []string{"graft", "merge", "-v", "../../assets/merge/second.yml"}
				stdout = ""
				stderr = ""
				main()
				So(stdout, ShouldNotContainSubstring, "Version")
				So(stdout, ShouldNotEqual, "")
				So(stderr, ShouldEqual, "")
				So(rc, ShouldEqual, 0)
			})
			Convey("When invoked under a different argv[0] name", func() {
				// The version line echoes the name the binary was invoked
				// as (os.Args[0], same as spruce), so a spruce-named
				// symlink or copy reports itself as spruce to genesis.
				os.Args = []string{"spruce", "-v"}
				stdout = ""
				stderr = ""
				main()
				So(stdout, ShouldStartWith, fmt.Sprintf("spruce - Version %s", Version))
				So(stderr, ShouldEqual, "")
				So(rc, ShouldEqual, 0)
			})
		})
		Convey("Should panic on errors merging docs", func() {
			os.Args = []string{"graft", "merge", "../../assets/merge/bad.yml"}
			stdout = ""
			stderr = ""
			main()
			So(stderr, ShouldContainSubstring, "../../assets/merge/bad.yml: root of YAML document is not a hash/map")
			So(rc, ShouldEqual, 2)
		})
		/* Fixme - how to trigger this?
		Convey("Should panic on errors marshaling yaml", func () {
		})
		*/
		Convey("Should output merged yaml on success", func() {
			os.Args = []string{"graft", "merge", "../../assets/merge/first.yml", "../../assets/merge/second.yml"}
			stdout = ""
			stderr = ""
			main()
			So(stdout, ShouldEqual, `array_append:
- one
- two
- three
array_default:
- FIRST
- SECOND
- third
array_inline:
- name: first_elem
  val: overwritten
- second_elem was overwritten
- third elem is appended
array_map_default:
- k1: key 1
  k2: updated
  name: AAA
- k2: final
  k3: original
  name: BBB
array_prepend:
- three
- four
- five
array_replace:
- - 1
  - 2
  - 3
key: overridden
map:
  key: value
  key2: val2

`)
			So(stderr, ShouldEqual, "")
		})
		Convey("Should output merged yaml with multi-doc enabled", func() {
			os.Args = []string{"graft", "merge", "-m", "../../assets/merge/multi-doc.yml"}
			stdout = ""
			stderr = ""
			main()
			So(stdout, ShouldEqual, `doc:
  data:
    test01: stuff
    test02: morestuff

`)
			So(stderr, ShouldEqual, "")
		})
		Convey("Should merge from stdin via '-' sentinel (genesis 'cat file | merge --skip-eval -' pattern)", func() {
			restoreStdin := setStdinFromFile(t, "../../assets/merge/first.yml")
			defer restoreStdin()

			os.Args = []string{"graft", "merge", "--skip-eval", "-"}
			stdout = ""
			stderr = ""
			main()
			So(stderr, ShouldEqual, "")
			So(stdout, ShouldNotEqual, "")
		})
		Convey("Should merge from stdin via '-' sentinel combined with a --prune flag (genesis 'echo ... | merge --prune -' pattern)", func() {
			restoreStdin := setStdinFromFile(t, "../../assets/merge/first.yml")
			defer restoreStdin()

			os.Args = []string{"graft", "merge", "--prune", "array_append", "-"}
			stdout = ""
			stderr = ""
			main()
			So(stderr, ShouldEqual, "")
			So(stdout, ShouldNotContainSubstring, "array_append")
		})
		Convey("Should merge from stdin implicitly when no file args and no '-' are given", func() {
			restoreStdin := setStdinFromFile(t, "../../assets/merge/first.yml")
			defer restoreStdin()

			os.Args = []string{"graft", "merge"}
			stdout = ""
			stderr = ""
			main()
			So(stderr, ShouldEqual, "")
			So(stdout, ShouldNotEqual, "")
		})
		Convey("Should not evaluate graft logic when --no-eval", func() {
			os.Args = []string{"graft", "merge", "--skip-eval", "../../assets/no-eval/first.yml", "../../assets/no-eval/second.yml"}
			stdout = ""
			stderr = ""
			main()
			So(stdout, ShouldEqual, `injected_jobs:
  .: (( inject jobs ))
jobs:
- name: consul
- name: route
- name: cell
- name: cc_bridge
param: (( param "Fill this in later" ))
properties:
  loggregator: true
  no_eval: (( grab property ))
  no_prune: (( prune ))
  not_empty: not_empty

`)
			So(stderr, ShouldEqual, "")
		})
		Convey("Should execute --prunes  when --no-eval", func() {
			os.Args = []string{"graft", "merge", "--skip-eval", "--prune", "jobs", "../../assets/no-eval/first.yml", "../../assets/no-eval/second.yml"}
			stdout = ""
			stderr = ""
			main()
			So(stdout, ShouldEqual, `injected_jobs:
  .: (( inject jobs ))
param: (( param "Fill this in later" ))
properties:
  loggregator: true
  no_eval: (( grab property ))
  no_prune: (( prune ))
  not_empty: not_empty

`)
			So(stderr, ShouldEqual, "")
		})
		Convey("Should execute --cherry-picks  when --no-eval", func() {
			os.Args = []string{"graft", "merge", "--skip-eval", "--cherry-pick", "properties", "../../assets/no-eval/first.yml", "../../assets/no-eval/second.yml"}
			stdout = ""
			stderr = ""
			main()
			So(stdout, ShouldEqual, `properties:
  loggregator: true
  no_eval: (( grab property ))
  no_prune: (( prune ))
  not_empty: not_empty

`)
			So(stderr, ShouldEqual, "")
		})
		Convey("Should handle de-referencing", func() {
			os.Args = []string{"graft", "merge", "../../assets/dereference/first.yml", "../../assets/dereference/second.yml"}
			stdout = ""
			stderr = ""
			main()
			So(stdout, ShouldEqual, `jobs:
- name: my-server
  static_ips:
  - 192.168.1.0
properties:
  client:
    servers:
    - 192.168.1.0

`)
			So(stderr, ShouldEqual, "")
		})
		Convey("De-referencing cyclical datastructures should throw an error", func() {
			os.Args = []string{"graft", "merge", "../../assets/dereference/cyclic-data.yml"}
			stdout = ""
			stderr = ""
			main()
			So(stdout, ShouldEqual, "")
			So(stderr, ShouldContainSubstring, "max recursion depth. You seem to have a self-referencing dataset\n")
			So(rc, ShouldEqual, 2)
		})
		Convey("Dereferencing multiple values should behave as desired", func() {
			os.Args = []string{"graft", "merge", "../../assets/dereference/multi-value.yml"}
			stdout = ""
			stderr = ""
			main()
			So(stderr, ShouldEqual, "")
			So(stdout, ShouldEqual, `jobs:
- instances: 1
  name: api_z1
  networks:
  - name: net1
    static_ips:
    - 192.168.1.2
- instances: 1
  name: api_z2
  networks:
  - name: net2
    static_ips:
    - 192.168.2.2
networks:
- name: net1
  subnets:
  - cloud_properties: random
    static:
    - "192.168.1.2 - 192.168.1.30"
- name: net2
  subnets:
  - cloud_properties: random
    static:
    - "192.168.2.2 - 192.168.2.30"
properties:
  api_server_primary: 192.168.1.2
  api_servers:
  - 192.168.1.2
  - 192.168.2.2

`)
		})
		Convey("Should output error on bad de-reference", func() {
			os.Args = []string{"graft", "merge", "../../assets/dereference/bad.yml"}
			stdout = ""
			stderr = ""
			main()
			So(stderr, ShouldContainSubstring, "`$.my` could not be found in the datastructure")
			So(rc, ShouldEqual, 2)
		})
		Convey("Pruning should happen after de-referencing", func() {
			os.Args = []string{"graft", "merge", "--prune", "jobs", "--prune", "properties.client.servers", "../../assets/dereference/first.yml", "../../assets/dereference/second.yml"}
			stdout = ""
			stderr = ""
			main()
			So(stderr, ShouldEqual, "")
			So(stdout, ShouldEqual, `properties:
  client: {}

`)
		})
		Convey("can dereference ~ / null values", func() {
			os.Args = []string{"graft", "merge", "--prune", "meta", "../../assets/dereference/null.yml"}
			stdout = ""
			stderr = ""
			main()
			So(stderr, ShouldEqual, "")
			So(stdout, ShouldEqual, `value: null

`)
		})
		Convey("can dereference nestedly", func() {
			os.Args = []string{"graft", "merge", "../../assets/dereference/multi.yml"}
			stdout = ""
			stderr = ""
			main()
			So(stderr, ShouldEqual, "")
			So(stdout, ShouldEqual, `name1: name
name2: name
name3: name
name4: name

`)
		})
		Convey("static_ips() failures return errors to the user", func() {
			os.Args = []string{"graft", "merge", "../../assets/static_ips/jobs.yml"}
			stdout = ""
			stderr = ""
			main()
			So(stderr, ShouldContainSubstring, ".static_ips: `$.networks` could not be found in the datastructure\n")
			So(stdout, ShouldEqual, "")
		})
		Convey("static_ips() get resolved, and are resolved prior to dereferencing", func() {
			os.Args = []string{"graft", "merge", "../../assets/static_ips/properties.yml", "../../assets/static_ips/jobs.yml", "../../assets/static_ips/network.yml"}
			stdout = ""
			stderr = ""
			main()
			So(stderr, ShouldEqual, "")
			So(stdout, ShouldEqual, `jobs:
- instances: 3
  name: api_z1
  networks:
  - name: net1
    static_ips:
    - 10.0.0.2
    - 10.0.0.3
    - 10.0.0.4
networks:
- name: net1
  subnets:
  - static:
    - "10.0.0.2 - 10.0.0.20"
properties:
  api_servers:
  - 10.0.0.2
  - 10.0.0.3
  - 10.0.0.4

`)
		})
		Convey("Included yaml file is escaped", func() {
			_ = os.Setenv("GRAFT_FILE_BASE_PATH", "../../assets/file_operator")
			defer func() { _ = os.Unsetenv("GRAFT_FILE_BASE_PATH") }()
			os.Args = []string{"graft", "merge", "../../assets/file_operator/test.yml"}
			stdout = ""
			stderr = ""
			main()
			So(stdout, ShouldEqual, `content:
  meta_test:
    stuff: "---\nmeta:\n  filename: test.yml\n\ncontent:\n  meta_test:\n    stuff: (( file meta.filename ))\n"
meta:
  filename: test.yml

`)
			So(stderr, ShouldEqual, "")
		})

		Convey("Parameters override their requirement", func() {
			os.Args = []string{"graft", "merge", "../../assets/params/global.yml", "../../assets/params/good.yml"}
			stdout = ""
			stderr = ""
			main()
			So(stdout, ShouldEqual, `cpu: 3
nested:
  key:
    override: true
networks:
- true
storage: 4096

`)
			So(stderr, ShouldEqual, "")
		})
		Convey("Parameters must be specified", func() {
			os.Args = []string{"graft", "merge", "../../assets/params/global.yml", "../../assets/params/fail.yml"}
			stdout = ""
			stderr = ""
			main()
			So(stdout, ShouldEqual, "")
			So(stderr, ShouldContainSubstring, "$.nested.key.override: provide nested override\n")
		})
		Convey("Pruning takes place after parameters", func() {
			os.Args = []string{"graft", "merge", "--prune", "nested", "../../assets/params/global.yml", "../../assets/params/fail.yml"}
			stdout = ""
			stderr = ""
			main()
			So(stderr, ShouldEqual, `1 error(s) detected:
 - $.nested.key.override: provide nested override


`)
			So(stdout, ShouldEqual, "")
		})
		Convey("string concatenation works", func() {
			os.Args = []string{"graft", "merge", "--prune", "local", "--prune", "env", "--prune", "cluster", "../../assets/concat/concat.yml"}
			stdout = ""
			stderr = ""
			main()
			So(stderr, ShouldEqual, "")
			So(stdout, ShouldEqual, `ident: c=mjolnir/prod;1234567890-abcdef

`)
		})
		Convey("string concatenation handles non-strings correctly", func() {
			os.Args = []string{"graft", "merge", "--prune", "local", "../../assets/concat/coerce.yml"}
			stdout = ""
			stderr = ""
			main()
			So(stderr, ShouldEqual, "")
			So(stdout, ShouldEqual, `url: http://domain.example.com/?v=1.3&rev=42

`)
		})
		Convey("string concatenation failure detected", func() {
			os.Args = []string{"graft", "merge", "../../assets/concat/fail.yml"}
			stdout = ""
			stderr = ""
			main()
			So(stdout, ShouldEqual, "")
			So(stderr, ShouldContainSubstring, "$.ident: unable to resolve `local.sites.42.uuid`:")
		})
		Convey("string concatenation handles multiple levels of reference", func() {
			os.Args = []string{"graft", "merge", "../../assets/concat/multi.yml"}
			stdout = ""
			stderr = ""
			main()
			So(stderr, ShouldEqual, "")
			So(stdout, ShouldEqual, `bar: quux.bar
baz: quux.bar.baz
foo: quux.bar.baz.foo
quux: quux

`)
			Convey("string concatenation handles infinite loop self-reference", func() {
				os.Args = []string{"graft", "merge", "../../assets/concat/loop.yml"}
				stdout = ""
				stderr = ""
				main()
				So(stdout, ShouldEqual, "")
				So(stderr, ShouldContainSubstring, "cycle detected")
			})
		})

		Convey("only param errors are displayed, if present", func() {
			os.Args = []string{"graft", "merge", "../../assets/errors/multi.yml"}
			stdout = ""
			stderr = ""
			main()
			So(stdout, ShouldEqual, "")
			So(stderr, ShouldEqual, ""+
				"1 error(s) detected:\n"+
				" - $.an-error: missing param!\n"+
				"\n\n"+
				"")
		})

		Convey("multiple errors of the same type on the same level are displayed", func() {
			os.Args = []string{"graft", "merge", "../../assets/errors/multi2.yml"}
			stdout = ""
			stderr = ""
			main()
			So(stdout, ShouldEqual, "")
			So(stderr, ShouldEqual, ""+
				"3 error(s) detected:\n"+
				" - $.a: first\n"+
				" - $.b: second\n"+
				" - $.c: third\n"+
				"\n\n"+
				"")
		})

		Convey("json command converts YAML to JSON", func() {
			os.Args = []string{"graft", "json", "../../assets/json/in.yml"}
			stdout = ""
			stderr = ""
			main()
			So(stderr, ShouldEqual, "")
			So(stdout, ShouldEqual, `{"map":{"list":["string",42,{"map":"of things"}]}}
`)
		})

		Convey("json command reads from stdin via redirect (no file arg)", func() {
			restoreStdin := setStdinFromFile(t, "../../assets/json/in.yml")
			defer restoreStdin()

			os.Args = []string{"graft", "json"}
			stdout = ""
			stderr = ""
			main()
			So(stderr, ShouldEqual, "")
			So(stdout, ShouldEqual, `{"map":{"list":["string",42,{"map":"of things"}]}}
`)
		})

		Convey("json command reads from stdin via explicit '-' sentinel", func() {
			restoreStdin := setStdinFromFile(t, "../../assets/json/in.yml")
			defer restoreStdin()

			os.Args = []string{"graft", "json", "-"}
			stdout = ""
			stderr = ""
			main()
			So(stderr, ShouldEqual, "")
			So(stdout, ShouldEqual, `{"map":{"list":["string",42,{"map":"of things"}]}}
`)
		})

		Convey("json command emits one JSON object per line for multi-doc input (genesis lines() framing)", func() {
			os.Args = []string{"graft", "json", "../../assets/merge/multi-doc.yml"}
			stdout = ""
			stderr = ""
			main()
			So(stderr, ShouldEqual, "")
			So(stdout, ShouldEqual, `{"doc":{"data":{"test01":"stuff"}}}
{"doc":{"data":{"test02":"morestuff"}}}
`)
		})

		Convey("json command handles malformed YAML", func() {
			os.Args = []string{"graft", "json", "../../assets/json/malformed.yml"}
			stdout = ""
			stderr = ""
			main()
			So(stdout, ShouldEqual, "")
			So(stderr, ShouldContainSubstring, "Root of YAML document is not a hash/map:")
		})

		Convey("json --reverse converts a compact JSON file to YAML", func() {
			os.Args = []string{"graft", "json", "--reverse", "../../assets/json/reverse-basic.json"}
			stdout = ""
			stderr = ""
			main()
			So(stderr, ShouldEqual, "")
			So(stdout, ShouldEqual, "database:\n  host: localhost\n  port: 5432\n")
		})

		Convey("json -r is the short form of --reverse", func() {
			os.Args = []string{"graft", "json", "-r", "../../assets/json/reverse-basic.json"}
			stdout = ""
			stderr = ""
			main()
			So(stderr, ShouldEqual, "")
			So(stdout, ShouldEqual, "database:\n  host: localhost\n  port: 5432\n")
		})

		Convey("json --reverse accepts pretty-printed multi-line JSON", func() {
			os.Args = []string{"graft", "json", "--reverse", "../../assets/json/reverse-pretty.json"}
			stdout = ""
			stderr = ""
			main()
			So(stderr, ShouldEqual, "")
			So(stdout, ShouldEqual, "a: 1\nb:\n- 1\n- 2\n- 3\n")
		})

		Convey("json --reverse round-trips graft json's own one-object-per-line output as multiple --- separated YAML documents", func() {
			os.Args = []string{"graft", "json", "--reverse", "../../assets/json/reverse-jsonl.json"}
			stdout = ""
			stderr = ""
			main()
			So(stderr, ShouldEqual, "")
			So(stdout, ShouldEqual, "---\ndoc: 1\n---\ndoc: 2\n")
		})

		Convey("json --reverse reads from stdin", func() {
			restoreStdin := setStdinFromFile(t, "../../assets/json/reverse-basic.json")
			defer restoreStdin()

			os.Args = []string{"graft", "json", "--reverse"}
			stdout = ""
			stderr = ""
			main()
			So(stderr, ShouldEqual, "")
			So(stdout, ShouldEqual, "database:\n  host: localhost\n  port: 5432\n")
		})

		Convey("json --reverse errors on invalid JSON, naming the source file", func() {
			os.Args = []string{"graft", "json", "--reverse", "../../assets/json/reverse-invalid.json"}
			stdout = ""
			stderr = ""
			main()
			So(stdout, ShouldEqual, "")
			So(stderr, ShouldContainSubstring, "reverse-invalid.json")
			So(stderr, ShouldContainSubstring, "Error parsing JSON")
		})

		Convey("json --multi-doc wraps multiple documents into a single JSON array instead of one object per line", func() {
			os.Args = []string{"graft", "json", "--multi-doc", "../../assets/merge/multi-doc.yml"}
			stdout = ""
			stderr = ""
			main()
			So(stderr, ShouldEqual, "")
			So(stdout, ShouldEqual, "[\n  {\n    \"doc\": {\n      \"data\": {\n        \"test01\": \"stuff\"\n      }\n    }\n  },\n  {\n    \"doc\": {\n      \"data\": {\n        \"test02\": \"morestuff\"\n      }\n    }\n  }\n]\n")
		})

		Convey("json without --multi-doc keeps the default one-JSON-object-per-line shape unchanged (genesis compat)", func() {
			os.Args = []string{"graft", "json", "../../assets/merge/multi-doc.yml"}
			stdout = ""
			stderr = ""
			main()
			So(stderr, ShouldEqual, "")
			So(stdout, ShouldEqual, `{"doc":{"data":{"test01":"stuff"}}}
{"doc":{"data":{"test02":"morestuff"}}}
`)
		})

		Convey("json with an explicit file argument does not also read a non-empty stdin (regression: cmdJSONEval used to unconditionally append '-')", func() {
			restoreStdin := setStdinFromFile(t, "../../assets/vaultinfo/novault.yml")
			defer restoreStdin()

			os.Args = []string{"graft", "json", "../../assets/json/in.yml"}
			stdout = ""
			stderr = ""
			main()
			So(stderr, ShouldEqual, "")
			So(stdout, ShouldEqual, `{"map":{"list":["string",42,{"map":"of things"}]}}
`)
		})

		Convey("vaultinfo lists vault calls in given file", func() {
			os.Args = []string{"graft", "vaultinfo", "../../assets/vaultinfo/single.yml"}
			stdout = ""
			stderr = ""
			main()
			So(stdout, ShouldEqual, `secrets:
- key: secret/bar:beep
  references:
  - meta.foo

`)
			So(stderr, ShouldEqual, "")
		})

		Convey("vaultinfo can handle multiple references to the same key", func() {
			os.Args = []string{"graft", "vaultinfo", "../../assets/vaultinfo/duplicate.yml"}
			stdout = ""
			stderr = ""
			main()
			So(stdout, ShouldEqual, `secrets:
- key: secret/bar:beep
  references:
  - meta.foo
  - meta.otherfoo

`)
			So(stderr, ShouldEqual, "")
		})

		Convey("vaultinfo can handle there being no vault references", func() {
			os.Args = []string{"graft", "vaultinfo", "../../assets/vaultinfo/novault.yml"}
			stdout = ""
			stderr = ""
			main()
			So(stdout, ShouldEqual, `secrets: []

`)
			So(stderr, ShouldEqual, "")
		})

		Convey("vaultinfo can handle concatenated vault secrets", func() {
			os.Args = []string{"graft", "vaultinfo", "../../assets/vaultinfo/concat.yml"}
			stdout = ""
			stderr = ""
			main()
			So(stdout, ShouldEqual, `secrets:
- key: imaprefix/beep:boop
  references:
  - foo.bar
- key: imaprefix/cup:cake
  references:
  - foo.bat
- key: imaprefix/hello:world
  references:
  - foo.wom

`)
			So(stderr, ShouldEqual, "")
		})

		Convey("vaultinfo can merge multiple files", func() {
			os.Args = []string{"graft", "vaultinfo", "../../assets/vaultinfo/merge1.yml", "../../assets/vaultinfo/merge2.yml"}
			stdout = ""
			stderr = ""
			main()
			So(stdout, ShouldEqual, `secrets:
- key: secret/foo:bar
  references:
  - foo
- key: secret/meep:meep
  references:
  - bar

`)
			So(stderr, ShouldEqual, "")
		})

		Convey("vaultinfo can handle improper yaml", func() {
			os.Args = []string{"graft", "vaultinfo", "../../assets/vaultinfo/improper.yml"}
			stdout = ""
			stderr = ""
			main()
			So(stdout, ShouldEqual, "")
			So(stderr, ShouldContainSubstring, "parse_error: failed to parse YAML")
		})

		Convey("Adding (dynamic) prune support for list entries (edge case scenario)", func() {
			os.Args = []string{"graft", "merge", "../../assets/prune/prune-in-lists/fileA.yml", "../../assets/prune/prune-in-lists/fileB.yml"}
			stdout = ""
			stderr = ""

			main()
			So(stderr, ShouldEqual, "")
			So(stdout, ShouldEqual, `meta:
  list:
  - one
  - three

`)
		})
		Convey("vaultinfo --json emits the same secret/reference data as JSON", func() {
			os.Args = []string{"graft", "vaultinfo", "--json", "../../assets/vaultinfo/single.yml"}
			stdout = ""
			stderr = ""
			main()
			So(stderr, ShouldEqual, "")
			So(stdout, ShouldEqual, "{\n  \"secrets\": [\n    {\n      \"key\": \"secret/bar:beep\",\n      \"references\": [\n        \"meta.foo\"\n      ]\n    }\n  ]\n}\n")
		})

		Convey("vaultinfo --json with no vault references emits an empty array, not null", func() {
			os.Args = []string{"graft", "vaultinfo", "--json", "../../assets/vaultinfo/novault.yml"}
			stdout = ""
			stderr = ""
			main()
			So(stderr, ShouldEqual, "")
			So(stdout, ShouldEqual, "{\n  \"secrets\": []\n}\n")
		})

		Convey("vaultinfo --paths-only lists only the vault secret keys, one per line, sorted", func() {
			os.Args = []string{"graft", "vaultinfo", "--paths-only", "../../assets/vaultinfo/concat.yml"}
			stdout = ""
			stderr = ""
			main()
			So(stderr, ShouldEqual, "")
			So(stdout, ShouldEqual, "imaprefix/beep:boop\nimaprefix/cup:cake\nimaprefix/hello:world\n")
		})

		Convey("vaultinfo --paths-only --json emits a JSON array of the secret keys", func() {
			os.Args = []string{"graft", "vaultinfo", "--paths-only", "--json", "../../assets/vaultinfo/concat.yml"}
			stdout = ""
			stderr = ""
			main()
			So(stderr, ShouldEqual, "")
			So(stdout, ShouldEqual, "[\n  \"imaprefix/beep:boop\",\n  \"imaprefix/cup:cake\",\n  \"imaprefix/hello:world\"\n]\n")
		})

		Convey("vaultinfo with neither --json nor --paths-only keeps the default YAML shape byte-identical (genesis compat)", func() {
			os.Args = []string{"graft", "vaultinfo", "../../assets/vaultinfo/single.yml"}
			stdout = ""
			stderr = ""
			main()
			So(stdout, ShouldEqual, `secrets:
- key: secret/bar:beep
  references:
  - meta.foo

`)
			So(stderr, ShouldEqual, "")
		})

		Convey("vaultinfo handles gopatch files", func() {
			os.Args = []string{"graft", "vaultinfo", "--go-patch", "../../assets/vaultinfo/merge1.yml", "../../assets/vaultinfo/go-patch.yml"}
			stdout = ""
			stderr = ""
			main()
			So(stdout, ShouldEqual, `secrets:
- key: secret/beep:boop
  references:
  - bar
- key: secret/blork:blork
  references:
  - new_key
- key: secret/foo:bar
  references:
  - foo

`)
			So(stderr, ShouldEqual, "")
		})

		Convey("vaultinfo exits non-zero on internal failure so a pipefail pipeline surfaces it", func() {
			os.Args = []string{"graft", "vaultinfo", "../../assets/vaultinfo/improper.yml"}
			stdout = ""
			stderr = ""
			main()
			So(stdout, ShouldEqual, "")
			So(rc, ShouldEqual, 2)
		})

		Convey("vaultinfo reports unresolvable nodes in genesis's vault_paths() stderr format", func() {
			os.Args = []string{"graft", "vaultinfo", "../../assets/vaultinfo/unresolvable.yml"}
			stdout = ""
			stderr = ""
			main()
			So(stdout, ShouldEqual, "")
			So(rc, ShouldEqual, 2)

			// genesis's ManifestProvider.pm vault_paths() extracts unresolvable
			// node paths from vaultinfo stderr with this exact pattern.
			unresolvablePathPattern := regexp.MustCompile(`(?m)^\s*-\s*\$\.(\S+?):`)
			matches := unresolvablePathPattern.FindAllStringSubmatch(stderr, -1)
			So(matches, ShouldHaveLength, 1)
			So(matches[0][1], ShouldEqual, "meta.broken")
		})

		Convey("vaultinfo output piped through 'graft json' yields genesis's expected {secrets:[{key,references}]} shape", func() {
			os.Args = []string{"graft", "vaultinfo", "../../assets/vaultinfo/single.yml"}
			stdout = ""
			stderr = ""
			main()
			So(stderr, ShouldEqual, "")
			vaultinfoYAML := stdout

			pipedFile, err := os.CreateTemp(t.TempDir(), "vaultinfo-*.yml")
			So(err, ShouldBeNil)
			_, err = pipedFile.WriteString(vaultinfoYAML)
			So(err, ShouldBeNil)
			So(pipedFile.Close(), ShouldBeNil)

			os.Args = []string{"graft", "json", pipedFile.Name()}
			stdout = ""
			stderr = ""
			main()
			So(stderr, ShouldEqual, "")
			So(stdout, ShouldEqual, `{"secrets":[{"key":"secret/bar:beep","references":["meta.foo"]}]}
`)
		})

		Convey("Adding (static) prune support for list entries (edge case scenario)", func() {
			os.Args = []string{"graft", "merge", "--prune", "meta.list.1", "../../assets/prune/prune-in-lists/fileA.yml"}
			stdout = ""
			stderr = ""

			main()
			So(stderr, ShouldEqual, "")
			So(stdout, ShouldEqual, `meta:
  list:
  - one
  - three

`)
		})

		Convey("Prune of an array element reached through an enclosing numeric array index", func() {
			os.Args = []string{"graft", "merge", "--prune", "jobs.0.networks.1", "../../assets/prune/prune-through-array/fileA.yml"}
			stdout = ""
			stderr = ""

			main()
			So(stderr, ShouldEqual, "")
			So(stdout, ShouldEqual, `jobs:
- name: web
  networks:
  - net-a
  - net-c
- name: worker
  networks:
  - net-x
  - net-y

`)
		})

		Convey("Prune of an array element reached through an enclosing named array entry", func() {
			os.Args = []string{"graft", "merge", "--prune", "jobs.web.networks.1", "../../assets/prune/prune-through-array/fileA.yml"}
			stdout = ""
			stderr = ""

			main()
			So(stderr, ShouldEqual, "")
			So(stdout, ShouldEqual, `jobs:
- name: web
  networks:
  - net-a
  - net-c
- name: worker
  networks:
  - net-x
  - net-y

`)
		})

		Convey("Issue - prune and inject cause side-effect", func() {
			os.Args = []string{"graft", "merge", "--prune", "meta", "../../assets/prune/prune-issue-with-inject/fileA.yml", "../../assets/prune/prune-issue-with-inject/fileB.yml"}
			stdout = ""
			stderr = ""

			main()
			So(stderr, ShouldEqual, "")
			So(stdout, ShouldEqual, `jobs:
- instances: 2
  name: main-job
  templates:
  - name: one
  - name: two
  update:
    canaries: 1
    max_in_flight: 3
- instances: 1
  name: another-job
  templates:
  - name: one
  - name: two
  update:
    canaries: 2

`)
		})

		Convey("Issue - prune and new-list-entry cause side-effect", func() {
			os.Args = []string{"graft", "merge", "--prune", "meta", "../../assets/prune/prune-issue-in-lists-with-new-entry/fileA.yml", "../../assets/prune/prune-issue-in-lists-with-new-entry/fileB.yml"}
			stdout = ""
			stderr = ""

			main()
			So(stderr, ShouldEqual, "")
			So(stdout, ShouldEqual, `list:
- name: A
  release: A
  version: A
- name: B
  release: B
  version: B
- name: C
  release: C
  version: C
- name: D
  release: D

`)
		})

		Convey("Issue #158 prune doesn't work when goes at the end (regression?) - variant A (https://github.com/fivetwenty-io/graft/issues/158)", func() {
			os.Args = []string{"graft", "merge", "../../assets/prune/issue-158/test.yml", "../../assets/prune/issue-158/prune.yml"}
			stdout = ""
			stderr = ""

			main()
			So(stderr, ShouldEqual, "")
			So(stdout, ShouldEqual, `test1: t2

`)
		})

		Convey("Issue #158 prune doesn't work when goes at the end (regression?) - variant B (https://github.com/fivetwenty-io/graft/issues/158)", func() {
			os.Args = []string{"graft", "merge", "../../assets/prune/issue-158/prune.yml", "../../assets/prune/issue-158/test.yml"}
			stdout = ""
			stderr = ""

			main()
			So(stderr, ShouldEqual, "")
			So(stdout, ShouldEqual, `test1: t2

`)
		})

		Convey("Text needed", func() {
			os.Args = []string{"graft", "merge", "../../assets/prune/issue-250/fileA.yml", "../../assets/prune/issue-250/fileB.yml"}
			stdout = ""
			stderr = ""

			main()
			So(stderr, ShouldEqual, "")
			So(stdout, ShouldEqual, `list:
- name: zero
  params:
    fail-fast: false
    preload: true
- name: one
  params:
    fail-fast: false
    preload: true
- name: two
  params:
    preload: false

`)
		})

		Convey("The delete operator deletes an entry in a simple list", func() {
			os.Args = []string{"graft", "merge", "../../assets/delete/simple-string-fileA.yml", "../../assets/delete/simple-string-fileB.yml"}
			stdout = ""
			stderr = ""

			main()
			So(stderr, ShouldEqual, "")
			So(stdout, ShouldEqual, `meta:
  list:
  - one
  - two
  - five

`)
		})

		Convey("The delete operator deletes an entry with whitespaces or special characters in a simple list", func() {
			os.Args = []string{"graft", "merge", "../../assets/delete/text-fileA.yml", "../../assets/delete/text-fileB.yml"}
			stdout = ""
			stderr = ""

			main()
			So(stderr, ShouldEqual, "")
			So(stdout, ShouldEqual, `meta:
  list:
  - Leonel Messi
  - Oliver Kahn
stuff:
  default_groups:
  - openid
  - cloud_controller.read
  - uaa.user
  - approvals.me
  - profile
  - roles
  - user_attributes
  - uaa.offline_token
  environment_scripts:
  - scripts/configure-HA-hosts.sh
  - scripts/forward_logfiles.sh

`)
		})

		Convey("Issue #156 Can use concat with static ips", func() {
			os.Args = []string{"graft", "merge", "../../assets/static_ips/issue-156/concat.yml"}
			stdout = ""
			stderr = ""

			main()
			So(stderr, ShouldEqual, "")
			So(stdout, ShouldEqual, `jobs:
- instances: 1
  name: pepe
  networks:
  - name: cf1
    static_ips:
    - 10.4.5.4
meta:
  network_prefix: "10.4"
networks:
- name: cf1
  subnets:
  - range: 10.4.36.0/24
    static:
    - "10.4.5.4 - 10.4.5.100"

`)
		})

		Convey("Issue #194 Globs with missing sub-items track data flow deps properly", func() {
			os.Args = []string{"graft", "merge", "../../assets/static_ips/vips-plus-grab.yml"}
			stdout = ""
			stderr = ""

			main()
			So(stderr, ShouldEqual, "")
			So(stdout, ShouldEqual, `jobs:
- instances: 1
  name: bosh
  networks:
  - name: stuff
    static_ips:
    - 1.2.3.4
meta:
  ips:
  - 1.2.3.4
networks:
- name: stuff
  subnets:
  - static:
    - 1.2.3.4
- name: stuff2
  type: vip

`)
		})
		Convey("Issue #201 - using `azs` instead of `az` in subnets", func() {
			Convey("jobs in only one zone can see the IPs of all subnets that mentioned that zone", func() {
				os.Args = []string{"graft", "merge", "../../assets/static_ips/multi-azs-one-zone-job.yml"}
				stdout = ""
				stderr = ""

				main()
				So(stderr, ShouldEqual, "")
				So(stdout, ShouldEqual, `jobs:
- azs:
  - z1
  instances: 2
  name: static_z1
  networks:
  - name: net1
    static_ips:
    - 10.0.0.1
    - 10.1.1.1
networks:
- name: net1
  subnets:
  - azs:
    - z1
    - z2
    - z3
    static:
    - "10.0.0.1 - 10.0.0.15"
  - azs:
    - z1
    static:
    - 10.1.1.1
  - azs:
    - z2
    static:
    - 10.2.2.2

`)
			})
			Convey("jobs in multiple zones can see the IPs of all subnets mentioning those zones", func() {
				os.Args = []string{"graft", "merge", "../../assets/static_ips/multi-azs-multi-zone-job.yml"}
				stdout = ""
				stderr = ""

				main()
				So(stderr, ShouldEqual, "")
				So(stdout, ShouldEqual, `jobs:
- azs:
  - z1
  - z2
  - z3
  instances: 2
  name: static_z1
  networks:
  - name: net1
    static_ips:
    - 10.1.1.1
    - 10.2.2.2
networks:
- name: net1
  subnets:
  - azs:
    - z1
    - z2
    - z3
    static:
    - "10.0.0.1 - 10.0.0.15"
  - azs:
    - z1
    static:
    - 10.1.1.1
  - azs:
    - z2
    static:
    - 10.2.2.2

`)
			})
			Convey("a z2-only job cannot see z1-only IPs", func() {
				os.Args = []string{"graft", "merge", "../../assets/static_ips/multi-azs-z2-underprovision.yml"}
				stdout = ""
				stderr = ""

				main()
				So(stderr, ShouldEqual, `1 error(s) detected:
 - $.jobs.static_z1.networks.net1.static_ips: request for static_ip(15) in a pool of only 15 (zero-indexed) static addresses


`)
				So(stdout, ShouldEqual, "")
			})
			Convey("jobs with multiple zones see one copy of available IPs, rather than one copy per zone", func() {
				os.Args = []string{"graft", "merge", "../../assets/static_ips/multi-azs-multi-underprovision.yml"}
				stdout = ""
				stderr = ""

				main()
				So(stderr, ShouldEqual, `1 error(s) detected:
 - $.jobs.static_z1.networks.net1.static_ips: request for static_ip(16) in a pool of only 16 (zero-indexed) static addresses


`)
				So(stdout, ShouldEqual, "")
			})
			Convey("edge case - same index used for different IPs with multi-az subnets", func() {
				os.Args = []string{"graft", "merge", "../../assets/static_ips/multi-azs-same-index-different-ip.yml"}
				stdout = ""
				stderr = ""

				main()
				So(stderr, ShouldEqual, "")
				So(stdout, ShouldEqual, `jobs:
- azs:
  - z1
  instances: 1
  name: static_z1
  networks:
  - name: net1
    static_ips:
    - 10.1.1.1
- azs:
  - z2
  instances: 1
  name: static_z2
  networks:
  - name: net1
    static_ips:
    - 10.2.2.2
networks:
- name: net1
  subnets:
  - azs:
    - z1
    - z2
    - z3
    static:
    - "10.0.0.1 - 10.0.0.15"
  - azs:
    - z1
    static:
    - 10.1.1.1
  - azs:
    - z2
    static:
    - 10.2.2.2

`)
			})
			Convey("edge case - dont give out same IP when specified in jobs with different zones", func() {
				os.Args = []string{"graft", "merge", "../../assets/static_ips/multi-azs-same-ip-different-zones.yml"}
				stdout = ""
				stderr = ""

				main()
				So(stderr, ShouldEqual, `1 error(s) detected:
 - $.jobs.static_z2.networks.net1.static_ips: tried to use IP '10.0.0.15', but that address is already allocated to static_z1/0


`)
				So(stdout, ShouldEqual, "")
			})
			Convey("edge case - don't give out same IP when using different offsets", func() {
				os.Args = []string{"graft", "merge", "../../assets/static_ips/multi-azs-same-ip-different-index.yml"}
				stdout = ""
				stderr = ""

				main()
				So(stderr, ShouldEqual, `1 error(s) detected:
 - $.jobs.static_z2.networks.net1.static_ips: tried to use IP '10.2.2.2', but that address is already allocated to static_z1/0


`)
				So(stdout, ShouldEqual, "")
			})
		})

		Convey("Empty operator works", func() {
			var baseFile, mergeFile string
			baseFile = "../../assets/empty/base.yml"

			testEmpty := func(files ...string) {
				os.Args = append([]string{"graft", "merge"}, files...)
				stdout = ""
				stderr = ""
				main()
				So(stderr, ShouldEqual, "")
				So(stdout, ShouldEqual, `meta:
  first: {}
  second: []
  third: ""

`)
			}

			Convey("when merging over maps", func() {
				Convey("with references as the type", func() {
					mergeFile = "../../assets/empty/references.yml"
					testEmpty(baseFile, mergeFile)
				})
				Convey("with literals as the type", func() {
					mergeFile = "../../assets/empty/literals.yml"
					testEmpty(baseFile, mergeFile)
				})
			})

			Convey("when merging over nothing", func() {
				Convey("with references as the type", func() {
					mergeFile = "../../assets/empty/references.yml"
					testEmpty(mergeFile)
				})
				Convey("with literals as the type", func() {
					mergeFile = "../../assets/empty/literals.yml"
					testEmpty(mergeFile)
				})
			})
		})

		Convey("Join operator works", func() {
			Convey("when dependencies could cause improper evaluation order", func() {
				os.Args = []string{"graft", "merge", "../../assets/join/issue-155/deps.yml"}
				stdout = ""
				stderr = ""
				main()
				So(stderr, ShouldEqual, "")
				So(stdout, ShouldEqual, `b:
- hello
- world
greeting: hello
output:
- hello world
- hello bye
z:
- hello
- bye

`)
			})
		})

		Convey("Calc operator works", func() {
			Convey("Calc comes with built-in functions", func() {
				os.Args = []string{"graft", "merge", "--prune", "meta", "../../assets/calc/functions.yml"}
				stdout = ""
				stderr = ""
				main()
				So(stderr, ShouldEqual, "")
				So(stdout, ShouldEqual, `properties:
  homework:
    ceil: 9
    floor: 3
    max: 8.333
    min: 3.666
    mod: 1.0010000000000003
    pow: 2374.968565439325
    sqrt: 2.886693610343848

`)
			})

			Convey("Calc works with dependencies", func() {
				os.Args = []string{"graft", "merge", "--prune", "meta", "../../assets/calc/dependencies.yml"}
				stdout = ""
				stderr = ""
				main()
				So(stderr, ShouldEqual, "")
				So(stdout, ShouldEqual, `jobs:
- instances: 4
  name: big_ones
- instances: 1
  name: small_ones
- instances: 2
  name: extra_ones

`)
			})

			Convey("Calc expects only one argument which is a quoted mathematical expression (as a Literal in Graft)", func() {
				os.Args = []string{"graft", "merge", "--prune", "meta", "../../assets/calc/wrong-syntax.yml"}
				stdout = ""
				stderr = ""
				main()
				So(stderr, ShouldEqual, `1 error(s) detected:
 - $.jobs.one.instances: calc operator only expects one argument containing the expression


`)
				So(stdout, ShouldEqual, "")
			})

			Convey("Calc operator does not support named variables", func() {
				os.Args = []string{"graft", "merge", "--prune", "meta", "../../assets/calc/no-named-variables.yml"}
				stdout = ""
				stderr = ""
				main()
				So(stderr, ShouldEqual, `1 error(s) detected:
 - $.jobs.one.instances: calc operator does not support named variables in expression: pi, r


`)
				So(stdout, ShouldEqual, "")
			})

			Convey("Calc operator checks input for built-in functions", func() {
				os.Args = []string{"graft", "merge", "--prune", "meta", "../../assets/calc/bad-functions.yml"}
				stdout = ""
				stderr = ""
				main()
				So(stderr, ShouldEqual, `7 error(s) detected:
 - $.properties.homework.ceil: ceil function expects one argument of type float64
 - $.properties.homework.floor: floor function expects one argument of type float64
 - $.properties.homework.max: max function expects two arguments of type float64
 - $.properties.homework.min: min function expects two arguments of type float64
 - $.properties.homework.mod: mod function expects two arguments of type float64
 - $.properties.homework.pow: pow function expects two arguments of type float64
 - $.properties.homework.sqrt: sqrt function expects one argument of type float64


`)
				So(stdout, ShouldEqual, "")
			})

			Convey("Calc operator checks referenced types", func() {
				os.Args = []string{"graft", "merge", "--prune", "meta", "../../assets/calc/wrong-type.yml"}
				stdout = ""
				stderr = ""
				main()
				So(stderr, ShouldEqual, `4 error(s) detected:
 - $.properties.homework.list: path meta.list is of type slice, which cannot be used in calculations
 - $.properties.homework.map: path meta.map is of type map, which cannot be used in calculations
 - $.properties.homework.nil: path meta.nil references a nil value, which cannot be used in calculations
 - $.properties.homework.string: path meta.string is of type string, which cannot be used in calculations


`)
				So(stdout, ShouldEqual, "")
			})

			Convey("Calc returns int64s if possible", func() {
				os.Args = []string{"graft", "merge", "--prune", "meta", "../../assets/calc/large-ints.yml"}
				stdout = ""
				stderr = ""
				main()
				So(stderr, ShouldEqual, "")
				So(stdout, ShouldEqual, `float: 7.7760001e+06
int: 7776000

`)
			})
		})

		Convey("YAML output is ordered the same way each time (#184)", func() {
			for i := 0; i < 30; i++ {
				os.Args = []string{"graft", "merge", "../../assets/output-order/sample.yml"}
				stdout = ""
				stderr = ""
				main()
				So(stderr, ShouldEqual, "")
				So(stdout, ShouldEqual, `properties:
  cc:
    quota_definitions:
      q2GB:
        non_basic_services_allowed: true
      q4GB:
        non_basic_services_allowed: true
      q256MB:
        non_basic_services_allowed: true

`)
			}
		})

		Convey("Sort test cases", func() {
			Convey("sort operator functionality", func() {
				os.Args = []string{"graft", "merge", "../../assets/sort/base.yml", "../../assets/sort/op.yml"}
				stdout = ""
				stderr = ""
				main()
				So(stderr, ShouldEqual, "")
				So(stdout, ShouldEqual, `float_list:
- 1.42
- 2.42
- 3.42
- 4.42
- 5.42
- 6.42
- 7.42
- 8.42
- 9.42
foobar_list:
- foobar: item-6
- foobar: item-7
- foobar: item-8
- foobar: item-9
- foobar: item-g
- foobar: item-h
- foobar: item-i
- foobar: item-j
- foobar: item-k
- foobar: item-l
- foobar: item-m
int_list:
- 1
- 2
- 3
- 4
- 5
- 6
- 7
- 8
- 9
key_list:
- key: item-1
- key: item-2
- key: item-3
- key: item-4
- key: item-a
- key: item-b
- key: item-c
- key: item-d
- key: item-e
- key: item-f
- key: item-g
- key: item-h
- key: item-i
name_list:
- name: item-1
- name: item-2
- name: item-3
- name: item-4
- name: item-5
- name: item-6
- name: item-7
- name: item-8
- name: item-9
- name: item-a
- name: item-b
- name: item-c
- name: item-d
- name: item-e
- name: item-f
- name: item-g
- name: item-h
- name: item-i
- name: item-j
- name: item-k
- name: item-l
- name: item-m
- name: item-n
- name: item-o
- name: item-p
- name: item-q
- name: item-r
- name: item-s
- name: item-t
- name: item-u
- name: item-v
- name: item-w
- name: item-x
- name: item-y
- name: item-z

`)
			})
		})

		Convey("Given a Graft merge using the (( load <location> )) operator", func() {
			Convey("When the location is a local location", func() {
				Convey("The local data (via literal) should be loaded and inserted", func() {
					_ = os.Setenv("GRAFT_FILE_BASE_PATH", "../../")
					defer func() { _ = os.Unsetenv("GRAFT_FILE_BASE_PATH") }()
					os.Args = []string{"graft", "merge", "../../assets/load/base-local.yml"}
					stdout = ""
					stderr = ""
					main()
					So(stderr, ShouldEqual, "")
					So(stdout, ShouldEqual, `yet:
  another:
    yaml:
      structure:
        load:
          complex-list:
          - name: one
          - name: two
          map:
            key: value
          simple-list:
          - one
          - two

`)
				})

				Convey("Absolute paths are not interpreted as remote locations", func() {
					file, fileErr := os.CreateTemp("../../assets/load", "base-local-abs.yml")
					if fileErr != nil {
						fmt.Println(fileErr)
					}
					defer func() { _ = os.Remove(file.Name()) }()

					path, pathErr := filepath.Abs("../../assets/load/users.yml")
					if pathErr != nil {
						fmt.Println(pathErr)
					}

					content := "params:\n  users: (( load \"" + path + "\" ))"

					if _, err := file.WriteString(content); err != nil {
						fmt.Println(pathErr)
					}

					_ = os.Setenv("GRAFT_FILE_BASE_PATH", "../../")
					defer func() { _ = os.Unsetenv("GRAFT_FILE_BASE_PATH") }()
					os.Args = []string{"graft", "merge", "--prune", "meta", file.Name()}
					stdout = ""
					stderr = ""
					main()
					So(stderr, ShouldEqual, "")
					So(stdout, ShouldEqual, `params:
  users:
  - color: green
    name: bob
  - color: red
    name: fred

`)
				})

				Convey("The local data (via reference) should be loaded and inserted", func() {
					os.Args = []string{"graft", "merge", "--prune", "meta", "../../assets/load/base-local-ref.yml"}
					stdout = ""
					stderr = ""
					main()
					So(stderr, ShouldEqual, "")
					So(stdout, ShouldEqual, `params:
  users:
  - color: green
    name: bob
  - color: red
    name: fred

`)
				})

				Convey("That an error is returned if no file can be found", func() {
					os.Args = []string{"graft", "merge", "../../assets/load/base-local.yml"}
					stdout = ""
					stderr = ""
					main()
					So(stderr, ShouldEqual, `1 error(s) detected:
 - $.yet.another.yaml.structure.load: unable to get any content using location assets/load/other.yml: it is not a file or usable URI


`)
					So(stdout, ShouldEqual, "")
				})
			})

			Convey("When the location is a remote location", func() {
				srv := &http.Server{Addr: ":31337", ReadHeaderTimeout: 10 * time.Second}
				defer func() {
					if srv != nil {
						_ = srv.Shutdown(context.Background())
					}
				}()

				go func() {
					http.Handle("/assets/",
						http.StripPrefix("/assets/",
							http.FileServer(http.Dir("../../assets/"))))

					_ = srv.ListenAndServe()
				}()
				time.Sleep(1 * time.Second)

				Convey("The remote data should be loaded and inserted", func() {
					os.Args = []string{"graft", "merge", "../../assets/load/base-remote.yml"}
					stdout = ""
					stderr = ""
					main()
					So(stderr, ShouldEqual, "")
					So(stdout, ShouldEqual, `yet:
  another:
    yaml:
      structure:
        load:
        - one
        - two

`)
				})
			})
		})

		Convey("Cherry picking test cases", func() {
			Convey("Cherry pick just one root level path", func() {
				os.Args = []string{"graft", "merge", "--cherry-pick", "properties", "../../assets/cherry-pick/fileA.yml", "../../assets/cherry-pick/fileB.yml"}
				stdout = ""
				stderr = ""
				main()
				So(stderr, ShouldEqual, "")
				So(stdout, ShouldEqual, `properties:
  hahn:
    flags: open
    id: b503e54a-c872-4643-a09c-5480c5940d0c
  vb:
    flags: auth,block,read-only
    id: 74a03820-3f81-45ca-afd5-d7d57b947ff1

`)
			})

			Convey("Cherry pick a path that is a list entry", func() {
				os.Args = []string{"graft", "merge", "--cherry-pick", "releases.vb", "../../assets/cherry-pick/fileA.yml", "../../assets/cherry-pick/fileB.yml"}
				stdout = ""
				stderr = ""
				main()
				So(stderr, ShouldEqual, "")
				So(stdout, ShouldEqual, `releases:
- name: vb

`)
			})

			Convey("Cherry pick a path that is deep down the structure", func() {
				os.Args = []string{"graft", "merge", "--cherry-pick", "meta.some.deep.structure.maplist", "../../assets/cherry-pick/fileA.yml", "../../assets/cherry-pick/fileB.yml"}
				stdout = ""
				stderr = ""
				main()
				So(stderr, ShouldEqual, "")
				So(stdout, ShouldEqual, `meta:
  some:
    deep:
      structure:
        maplist:
          keyA: valueA
          keyB: valueB

`)
			})

			Convey("Cherry pick a series of different paths at the same time", func() {
				os.Args = []string{"graft", "merge", "--cherry-pick", "properties", "--cherry-pick", "releases.vb", "--cherry-pick", "meta.some.deep.structure.maplist", "../../assets/cherry-pick/fileA.yml", "../../assets/cherry-pick/fileB.yml"}
				stdout = ""
				stderr = ""
				main()
				So(stderr, ShouldEqual, "")
				So(stdout, ShouldEqual, `meta:
  some:
    deep:
      structure:
        maplist:
          keyA: valueA
          keyB: valueB
properties:
  hahn:
    flags: open
    id: b503e54a-c872-4643-a09c-5480c5940d0c
  vb:
    flags: auth,block,read-only
    id: 74a03820-3f81-45ca-afd5-d7d57b947ff1
releases:
- name: vb

`)
			})

			Convey("Cherry pick a path and prune something at the same time in a map", func() {
				os.Args = []string{"graft", "merge", "--cherry-pick", "properties", "--prune", "properties.vb.flags", "../../assets/cherry-pick/fileA.yml", "../../assets/cherry-pick/fileB.yml"}
				stdout = ""
				stderr = ""
				main()
				So(stderr, ShouldEqual, "")
				So(stdout, ShouldEqual, `properties:
  hahn:
    flags: open
    id: b503e54a-c872-4643-a09c-5480c5940d0c
  vb:
    id: 74a03820-3f81-45ca-afd5-d7d57b947ff1

`)
			})

			Convey("Cherry picking should fail if you cherry-pick a prune path", func() {
				os.Args = []string{"graft", "merge", "--cherry-pick", "properties", "--prune", "properties", "../../assets/cherry-pick/fileA.yml", "../../assets/cherry-pick/fileB.yml"}
				stdout = ""
				stderr = ""
				main()
				So(stderr, ShouldEqual, "Merge failed: validation_error: key not found: properties (missing segment 'properties')\n")
				So(stdout, ShouldEqual, "")
			})

			Convey("Cherry picking should fail if picking a sub-level path while prune wipes the parent", func() {
				os.Args = []string{"graft", "merge", "--cherry-pick", "releases.vb", "--prune", "releases", "../../assets/cherry-pick/fileA.yml", "../../assets/cherry-pick/fileB.yml"}
				stdout = ""
				stderr = ""
				main()
				So(stderr, ShouldEqual, "Merge failed: validation_error: key not found: releases.vb (missing segment 'releases')\n")
				So(stdout, ShouldEqual, "")
			})

			Convey("Cherry pick a list entry path of a list that uses 'key' as its identifier", func() {
				os.Args = []string{"graft", "merge", "--cherry-pick", "list.two", "../../assets/cherry-pick/key-based-list.yml"}
				stdout = ""
				stderr = ""
				main()
				So(stderr, ShouldEqual, "")
				So(stdout, ShouldEqual, `list:
- desc: The second one
  key: two
  version: v2

`)
			})

			Convey("Cherry pick a list entry path of a list that uses 'id' as its identifier", func() {
				os.Args = []string{"graft", "merge", "--cherry-pick", "list.two", "../../assets/cherry-pick/id-based-list.yml"}
				stdout = ""
				stderr = ""
				main()
				So(stderr, ShouldEqual, "")
				So(stdout, ShouldEqual, `list:
- desc: The second one
  id: two
  version: v2

`)
			})

			Convey("Cherry pick one list entry path that references the index", func() {
				os.Args = []string{"graft", "merge", "--cherry-pick", "list.1", "../../assets/cherry-pick/name-based-list.yml"}
				stdout = ""
				stderr = ""
				main()
				So(stderr, ShouldEqual, "")
				So(stdout, ShouldEqual, `list:
- desc: The second one
  name: two
  version: v2

`)
			})

			Convey("Cherry pick two list entry paths that reference indexes", func() {
				os.Args = []string{"graft", "merge", "--cherry-pick", "list.1", "--cherry-pick", "list.4", "../../assets/cherry-pick/name-based-list.yml"}
				stdout = ""
				stderr = ""
				main()
				So(stderr, ShouldEqual, "")
				So(stdout, ShouldEqual, `list:
- desc: The fifth one
  name: five
  version: v5
- desc: The second one
  name: two
  version: v2

`)
			})

			Convey("Cherry pick one list entry path that references an invalid index", func() {
				// Note: Current implementation treats "list.10" as a map key, not an array index
				// This creates a new key "10" under "list" with the entire array as value
				os.Args = []string{"graft", "merge", "--cherry-pick", "list.10", "../../assets/cherry-pick/name-based-list.yml"}
				stdout = ""
				stderr = ""
				main()
				So(stderr, ShouldEqual, "")
				So(stdout, ShouldEqual, `list:
  "10":
  - desc: The first one
    name: one
    version: v1
  - desc: The second one
    name: two
    version: v2
  - desc: The third one
    name: three
    version: v3
  - desc: The fourth one
    name: four
    version: v4
  - desc: The fifth one
    name: five
    version: v5
  - desc: The sixth one
    name: six
    version: v6

`)
			})

			Convey("Cherry pick should only pick the exact name based on the path", func() {
				os.Args = []string{"graft", "merge", "--cherry-pick", "map", "--prune", "subkey", "../../assets/cherry-pick/test-exact-names.yml"}
				stdout = ""
				stderr = ""
				main()
				So(stderr, ShouldEqual, "")
				So(stdout, ShouldEqual, `map:
  other: value
  subkey: this is the real subkey

`)
			})

			Convey("Cherry pick should only evaluate the dynamic operators that are relevant", func() {
				os.Args = []string{"graft", "merge", "--cherry-pick", "params", "../../assets/cherry-pick/partial-eval.yml"}
				stdout = ""
				stderr = ""
				main()
				So(stderr, ShouldEqual, "")
				So(stdout, ShouldEqual, `params:
  mode: default
  name: sandbox-thing
  type: thing

`)
			})
		})

		Convey("FallbackAppend should cause the default behavior after a key merge to go to append", func() {
			os.Args = []string{"graft", "merge", "--fallback-append", "../../assets/fallback-append/test1.yml", "../../assets/fallback-append/test2.yml"}
			stdout = ""
			stderr = ""
			main()
			So(stderr, ShouldEqual, "")
			So(stdout, ShouldEqual, `array:
- thing: 1
  value: foo
- thing: 2
  value: bar
- thing: 1
  value: baz

`)
		})

		Convey("Without FallbackAppend, the default merge behavior after a key merge should still be inline", func() {
			os.Args = []string{"graft", "merge", "../../assets/fallback-append/test1.yml", "../../assets/fallback-append/test2.yml"}
			stdout = ""
			stderr = ""
			main()
			So(stderr, ShouldEqual, "")
			So(stdout, ShouldEqual, `array:
- thing: 1
  value: baz
- thing: 2
  value: bar

`)
		})

		Convey("Defer", func() {
			Convey("should err if there are no arguments", func() {
				os.Args = []string{"graft", "merge", "../../assets/defer/nothing.yml"}
				stdout = ""
				stderr = ""
				main()
				So(stderr, ShouldEqual, `1 error(s) detected:
 - $.foo: defer has no arguments - what are you deferring?


`)
				So(stdout, ShouldEqual, "")
			})

			Convey("on a non-quoted string", func() {
				os.Args = []string{"graft", "merge", "../../assets/defer/simple-ref.yml"}
				stdout = ""
				stderr = ""
				main()
				So(stderr, ShouldEqual, "")
				So(stdout, ShouldEqual, `foo: (( thing ))

`)
			})

			Convey("on a quoted string", func() {
				os.Args = []string{"graft", "merge", "../../assets/defer/simple-string.yml"}
				stdout = ""
				stderr = ""
				main()
				So(stderr, ShouldEqual, "")
				So(stdout, ShouldEqual, `foo: (( "thing" ))

`)
			})

			Convey("on a non-quoted string called nil", func() {
				os.Args = []string{"graft", "merge", "../../assets/defer/simple-nil.yml"}
				stdout = ""
				stderr = ""
				main()
				So(stderr, ShouldEqual, "")
				So(stdout, ShouldEqual, `foo: (( nil ))

`)
			})

			Convey("on an integer", func() {
				os.Args = []string{"graft", "merge", "../../assets/defer/simple-int.yml"}
				stdout = ""
				stderr = ""
				main()
				So(stderr, ShouldEqual, "")
				So(stdout, ShouldEqual, `foo: (( 123 ))

`)
			})

			Convey("on a float", func() {
				os.Args = []string{"graft", "merge", "../../assets/defer/simple-float.yml"}
				stdout = ""
				stderr = ""
				main()
				So(stderr, ShouldEqual, "")
				So(stdout, ShouldEqual, `foo: (( 1.23 ))

`)
			})

			Convey("on an environment variable ", func() {
				os.Args = []string{"graft", "merge", "../../assets/defer/simple-envvar.yml"}
				stdout = ""
				stderr = ""
				main()
				So(stderr, ShouldEqual, "")
				So(stdout, ShouldEqual, `foo: (( $TESTVAR ))

`)
			})

			Convey("on an unquoted string that could reference another key", func() {
				os.Args = []string{"graft", "merge", "../../assets/defer/reference.yml"}
				stdout = ""
				stderr = ""
				main()
				So(stderr, ShouldEqual, "")
				So(stdout, ShouldEqual, `foo: (( thing ))
thing: (( thing ))

`)
			})

			Convey("on a value with a logical-or", func() {
				os.Args = []string{"graft", "merge", "../../assets/defer/or.yml"}
				stdout = ""
				stderr = ""
				main()
				So(stderr, ShouldEqual, "")
				So(stdout, ShouldEqual, `foo: (( grab this || "that" ))

`)
			})

			Convey("with another operator in the defer", func() {
				os.Args = []string{"graft", "merge", "../../assets/defer/grab.yml"}
				stdout = ""
				stderr = ""
				main()
				So(stderr, ShouldEqual, "")
				So(stdout, ShouldEqual, `foo: (( grab thing ))
grab: beep
thing: boop

`)
			})
		})

		Convey("non-specific node tags specific test cases", func() {
			Convey("non-specific node tags test case - style 1", func() {
				os.Args = []string{"graft", "merge", "../../assets/non-specific-node-tags-issue/fileA-1.yml", "../../assets/non-specific-node-tags-issue/fileB.yml"}
				stdout = ""
				stderr = ""
				main()
				So(stderr, ShouldEqual, "")
				So(stdout, ShouldEqual, `some:
  yaml:
    structure:
      certificate: |
        -----BEGIN CERTIFICATE-----
        QSBzcHJ1Y2UgaXMgYSB0cmVlIG9mIHRoZSBnZW51cyBQaWNlYSAvcGHJqsuIc2nL
        kMmZLyxbMV0gYSBnZW51cyBvZiBhYm91dCAzNSBzcGVjaWVzIG9mIGNvbmlmZXJv
        dXMgZXZlcmdyZWVuIHRyZWVzIGluIHRoZSBGYW1pbHkgUGluYWNlYWUsIGZvdW5k
        IGluIHRoZSBub3J0aGVybiB0ZW1wZXJhdGUgYW5kIGJvcmVhbCAodGFpZ2EpIHJl
        Z2lvbnMgb2YgdGhlIGVhcnRoLiBTcHJ1Y2VzIGFyZSBsYXJnZSB0cmVlcywgZnJv
        bSBhYm91dCAyMOKAkzYwIG1ldHJlcyAoYWJvdXQgNjDigJMyMDAgZmVldCkgdGFs
        bCB3aGVuIG1hdHVyZSwgYW5kIGNhbiBiZSBkaXN0aW5ndWlzaGVkIGJ5IHRoZWly
        IHdob3JsZWQgYnJhbmNoZXMgYW5kIGNvbmljYWwgZm9ybS4gVGhlIG5lZWRsZXMs
        IG9yIGxlYXZlcywgb2Ygc3BydWNlIHRyZWVzIGFyZSBhdHRhY2hlZCBzaW5nbHkg
        dG8gdGhlIGJyYW5jaGVzIGluIGEgc3BpcmFsIGZhc2hpb24sIGVhY2ggbmVlZGxl
        IG9uIGEgc21hbGwgcGVnLWxpa2Ugc3RydWN0dXJlLiBUaGUgbmVlZGxlcyBhcmUg
        c2hlZCB3aGVuIDTigJMxMCB5ZWFycyBvbGQsIGxlYXZpbmcgdGhlIGJyYW5jaGVz
        IHJvdWdoIHdpdGggdGhlIHJldGFpbmVkIHBlZ3MgKGFuIGVhc3kgbWVhbnMgb2Yg
        ZGlzdGluZ3Vpc2hpbmcgdGhlbSBmcm9tIG90aGVyIHNpbWlsYXIgZ2VuZXJhLCB3
        aGVyZSB0aGUgYnJhbmNoZXMgYXJlIGZhaXJseSBzbW9vdGgpLgoKU3BydWNlcyBh
        cmUgdXNlZCBhcyBmb29kIHBsYW50cyBieSB0aGUgbGFydmFlIG9mIHNvbWUgTGVw
        aWRvcHRlcmEgKG1vdGggYW5kIGJ1dHRlcmZseSkgc3BlY2llczsgc2VlIGxpc3Qg
        b2YgTGVwaWRvcHRlcmEgdGhhdCBmZWVkIG9uIHNwcnVjZXMuIFRoZXkgYXJlIGFs
        c28gdXNlZCBieSB0aGUgbGFydmFlIG9mIGdhbGwgYWRlbGdpZHMgKEFkZWxnZXMg
        c3BlY2llcykuCgpJbiB0aGUgbW91bnRhaW5zIG9mIHdlc3Rlcm4gU3dlZGVuIHNj
        aWVudGlzdHMgaGF2ZSBmb3VuZCBhIE5vcndheSBzcHJ1Y2UgdHJlZSwgbmlja25h
        bWVkIE9sZCBUamlra28sIHdoaWNoIGJ5IHJlcHJvZHVjaW5nIHRocm91Z2ggbGF5
        ZXJpbmcgaGFzIHJlYWNoZWQgYW4gYWdlIG9mIDksNTUwIHllYXJzIGFuZCBpcyBj
        bGFpbWVkIHRvIGJlIHRoZSB3b3JsZCdzIG9sZGVzdCBrbm93biBsaXZpbmcgdHJl
        ZS4K
        -----END CERTIFICATE-----
      someotherkey: value

`)
			})

			Convey("non-specific node tags test case - style 2", func() {
				os.Args = []string{"graft", "merge", "../../assets/non-specific-node-tags-issue/fileA-2.yml", "../../assets/non-specific-node-tags-issue/fileB.yml"}
				stdout = ""
				stderr = ""
				main()
				So(stderr, ShouldEqual, "")
				So(stdout, ShouldEqual, `some:
  yaml:
    structure:
      certificate: "-----BEGIN CERTIFICATE----- QSBzcHJ1Y2UgaXMgYSB0cmVlIG9mIHRoZSBnZW51cyBQaWNlYSAvcGHJqsuIc2nL kMmZLyxbMV0gYSBnZW51cyBvZiBhYm91dCAzNSBzcGVjaWVzIG9mIGNvbmlmZXJv dXMgZXZlcmdyZWVuIHRyZWVzIGluIHRoZSBGYW1pbHkgUGluYWNlYWUsIGZvdW5k IGluIHRoZSBub3J0aGVybiB0ZW1wZXJhdGUgYW5kIGJvcmVhbCAodGFpZ2EpIHJl Z2lvbnMgb2YgdGhlIGVhcnRoLiBTcHJ1Y2VzIGFyZSBsYXJnZSB0cmVlcywgZnJv bSBhYm91dCAyMOKAkzYwIG1ldHJlcyAoYWJvdXQgNjDigJMyMDAgZmVldCkgdGFs bCB3aGVuIG1hdHVyZSwgYW5kIGNhbiBiZSBkaXN0aW5ndWlzaGVkIGJ5IHRoZWly IHdob3JsZWQgYnJhbmNoZXMgYW5kIGNvbmljYWwgZm9ybS4gVGhlIG5lZWRsZXMs IG9yIGxlYXZlcywgb2Ygc3BydWNlIHRyZWVzIGFyZSBhdHRhY2hlZCBzaW5nbHkg dG8gdGhlIGJyYW5jaGVzIGluIGEgc3BpcmFsIGZhc2hpb24sIGVhY2ggbmVlZGxl IG9uIGEgc21hbGwgcGVnLWxpa2Ugc3RydWN0dXJlLiBUaGUgbmVlZGxlcyBhcmUg c2hlZCB3aGVuIDTigJMxMCB5ZWFycyBvbGQsIGxlYXZpbmcgdGhlIGJyYW5jaGVz IHJvdWdoIHdpdGggdGhlIHJldGFpbmVkIHBlZ3MgKGFuIGVhc3kgbWVhbnMgb2Yg ZGlzdGluZ3Vpc2hpbmcgdGhlbSBmcm9tIG90aGVyIHNpbWlsYXIgZ2VuZXJhLCB3 aGVyZSB0aGUgYnJhbmNoZXMgYXJlIGZhaXJseSBzbW9vdGgpLgoKU3BydWNlcyBh cmUgdXNlZCBhcyBmb29kIHBsYW50cyBieSB0aGUgbGFydmFlIG9mIHNvbWUgTGVw aWRvcHRlcmEgKG1vdGggYW5kIGJ1dHRlcmZseSkgc3BlY2llczsgc2VlIGxpc3Qg b2YgTGVwaWRvcHRlcmEgdGhhdCBmZWVkIG9uIHNwcnVjZXMuIFRoZXkgYXJlIGFs c28gdXNlZCBieSB0aGUgbGFydmFlIG9mIGdhbGwgYWRlbGdpZHMgKEFkZWxnZXMg c3BlY2llcykuCgpJbiB0aGUgbW91bnRhaW5zIG9mIHdlc3Rlcm4gU3dlZGVuIHNj aWVudGlzdHMgaGF2ZSBmb3VuZCBhIE5vcndheSBzcHJ1Y2UgdHJlZSwgbmlja25h bWVkIE9sZCBUamlra28sIHdoaWNoIGJ5IHJlcHJvZHVjaW5nIHRocm91Z2ggbGF5 ZXJpbmcgaGFzIHJlYWNoZWQgYW4gYWdlIG9mIDksNTUwIHllYXJzIGFuZCBpcyBj bGFpbWVkIHRvIGJlIHRoZSB3b3JsZCdzIG9sZGVzdCBrbm93biBsaXZpbmcgdHJl ZS4K -----END CERTIFICATE-----"
      someotherkey: value

`)
			})

			Convey("Issue #198 - avoid nil panics when merging arrays with nil elements", func() {
				os.Args = []string{"graft", "merge", "../../assets/issue-198/nil-array-elements.yml"}
				stdout = ""
				stderr = ""
				main()
				So(stderr, ShouldEqual, "")
				So(stdout, ShouldEqual, `empty_nil:
- null
- more stuff
explicit_nil:
- null
- stuff
latter_elements_nil:
- stuff
- null
nested_nil:
- stuff:
  - null
  - nested nil above
  thing: has stuff

`)
			})

			Convey("Issue #172 - don't panic if target key has map value", func() {
				os.Args = []string{"graft", "merge", "../../assets/issue-172/implicitmergemap.yml"}
				stdout = ""
				stderr = ""
				main()
				So(stderr, ShouldEqual, `warning: $.array-of-maps.0: new object's key 'name' cannot have a value which is a hash or sequence - cannot merge by key
warning: Falling back to inline merge strategy
`)
				So(stdout, ShouldEqual, `array-of-maps:
- name:
    subkey1: true
    subkey2: false

`)
			})
			Convey("Issue #172 - don't panic if target key has sequence value", func() {
				os.Args = []string{"graft", "merge", "../../assets/issue-172/implicitmergeseq.yml"}
				stdout = ""
				stderr = ""
				main()
				So(stderr, ShouldEqual, `warning: $.array-of-maps.0: new object's key 'name' cannot have a value which is a hash or sequence - cannot merge by key
warning: Falling back to inline merge strategy
`)
				So(stdout, ShouldEqual, `array-of-maps:
- name:
  - subkey1
  - subkey2

`)
			})

			Convey("Issue #172 - error instead of panic if merge was specifically requested but target key has map value", func() {
				os.Args = []string{"graft", "merge", "../../assets/issue-172/explicitmerge1.yml"}
				stdout = ""
				stderr = ""
				main()
				So(stderr, ShouldEqual, `1 error(s) detected:
 - $.array-of-maps.0: new object's key 'name' cannot have a value which is a hash or sequence - cannot merge by key


`)
				So(stdout, ShouldEqual, "")
			})

			Convey("Issue #172 - error instead of panic if merge on key was specifically requested but target key has map value", func() {
				os.Args = []string{"graft", "merge", "../../assets/issue-172/explicitmergeonkey1.yml"}
				stdout = ""
				stderr = ""
				main()
				So(stderr, ShouldEqual, `1 error(s) detected:
 - $.array-of-maps.0: new object's key 'mergekey' cannot have a value which is a hash or sequence - cannot merge by key


`)
				So(stdout, ShouldEqual, "")
			})
		})

		Convey("Issue #215 - Handle really big ints as operator arguments", func() {
			Convey("We didn't break normal small ints", func() {
				os.Args = []string{"graft", "merge", "../../assets/issue-215/smallint.yml"}
				stdout = ""
				stderr = ""
				main()
				So(stderr, ShouldEqual, "")
				So(stdout, ShouldEqual, "foo: -> 3 <-\n\n")
			})

			Convey("We can handle ints bigger than 2^63 - 1", func() {
				os.Args = []string{"graft", "merge", "../../assets/issue-215/hugeint.yml"}
				stdout = ""
				stderr = ""
				main()
				So(stderr, ShouldEqual, "")
				// Note: Large integers beyond float64 precision are converted to scientific notation
				So(stdout, ShouldEqual, "foo: -> 6.239871649276491e+24 <-\n\n")
			})
		})

		Convey("Issue #153 - Cartesian Product should produce a []interface{}", func() {
			os.Args = []string{"graft", "merge", "../../assets/cartesian-product/can-be-joined.yml"}
			stdout = ""
			stderr = ""
			main()
			So(stderr, ShouldEqual, "")
			So(stdout, ShouldEqual, `ips:
- 1.2.3.4
- 2.2.3.4
ips_with_port:
- 1.2.3.4:80
- 2.2.3.4:80
join_ips_with_port: 1.2.3.4:80,2.2.3.4:80

`)
		})

		Convey("Issue #169 - Cartesian Product should produce a []interface{}", func() {
			os.Args = []string{"graft", "merge", "../../assets/cartesian-product/can-be-grabbed.yml"}
			stdout = ""
			stderr = ""
			main()
			So(stderr, ShouldEqual, "")
			So(stdout, ShouldEqual, `groups:
- jobs:
  - master-isolation-tests
  - master-integration-tests
  - master-dependencies-test
  - master-docker-build
  name: master
meta:
  fast-tests:
  - isolation
  master-fast-tests:
  - master-isolation-tests
  master-slow-tests:
  - master-integration-tests
  slow-tests:
  - integration

`)
		})

		Convey("Issue #267 - specifying an explicit merge operator must behave in the same way as relying on the default implicit merge operation", func() {
			Convey("Option 1 - standard use-case: no explicit merge, named-entry list identifier key is the default called 'name'", func() {
				os.Args = []string{"graft", "merge", "../../assets/issue-267/option1-fileA.yml", "../../assets/issue-267/option1-fileB.yml"}
				stdout = ""
				stderr = ""

				main()
				So(stderr, ShouldEqual, "")
				So(stdout, ShouldEqual, `serverFiles:
  prometheus.yml:
    scrape_configs:
    - name: one
    - name: two

`)
			})

			Convey("Option 2 - academic version of the option 1: same set-up, but with explicit usage of the merge operator", func() {
				os.Args = []string{"graft", "merge", "../../assets/issue-267/option2-fileA.yml", "../../assets/issue-267/option2-fileB.yml"}
				stdout = ""
				stderr = ""

				main()
				So(stderr, ShouldEqual, "")
				So(stdout, ShouldEqual, `serverFiles:
  prometheus.yml:
    scrape_configs:
    - name: one
    - name: two

`)
			})

			Convey("Option 3 - even more academic version of the option 1: same set-up, but with explicit usage of the merge operator and specification of the default identifier key called 'name'", func() {
				os.Args = []string{"graft", "merge", "../../assets/issue-267/option3-fileA.yml", "../../assets/issue-267/option3-fileB.yml"}
				stdout = ""
				stderr = ""

				main()
				So(stderr, ShouldEqual, "")
				So(stdout, ShouldEqual, `serverFiles:
  prometheus.yml:
    scrape_configs:
    - name: one
    - name: two

`)
			})

			Convey("Option 4 - actual real world use case, where the identifier key is call 'job_name' and therefore explicit merge on key is required", func() {
				os.Args = []string{"graft", "merge", "../../assets/issue-267/option4-fileA.yml", "../../assets/issue-267/option4-fileB.yml"}
				stdout = ""
				stderr = ""

				main()
				So(stderr, ShouldEqual, "")
				So(stdout, ShouldEqual, `serverFiles:
  prometheus.yml:
    scrape_configs:
    - job_name: one
    - job_name: two

`)
			})
		})

		Convey("Support go-patch files", func() {
			Convey("go-patch can modify yaml files in the merge phase, and insert graft operators as required", func() {
				os.Args = []string{"graft", "merge", "--go-patch", "../../assets/go-patch/base.yml", "../../assets/go-patch/patch.yml", "../../assets/go-patch/toMerge.yml"}
				stdout = ""
				stderr = ""
				main()
				So(stderr, ShouldEqual, "")
				So(stdout, ShouldEqual, `array:
- 10
- 5
- 6
graft_array_grab:
- add graft stuff in the beginning of the array
- name: item7
- name: item8
- name: item9
items:
- add graft stuff in the beginning of the array
- name: item7
- name: item8
- name: item9
key: 10
key2:
  nested:
    another_nested:
      super_nested: 10
    super_nested: 10
  other: 3
more_stuff: is here
new_key: 10

`)
			})
			Convey("go-patch throws errors to the front-end when there are go-patch issues", func() {
				os.Args = []string{"graft", "merge", "--go-patch", "../../assets/go-patch/base.yml", "../../assets/go-patch/err.yml", "../../assets/go-patch/toMerge.yml"}
				stdout = ""
				stderr = ""
				main()
				So(stderr, ShouldEqual, "Merge failed: Expected to find a map key 'key_not_there' for path '/key_not_there' (found map keys: 'array', 'items', 'key', 'key2')\n")
				So(stdout, ShouldEqual, "")
			})
			Convey("yaml-parser throws errors when trying to parse gopatch from array-based files", func() {
				os.Args = []string{"graft", "merge", "--go-patch", "../../assets/go-patch/base.yml", "../../assets/go-patch/bad.yml"}
				stdout = ""
				stderr = ""
				main()
				// Byte-exact pin against HEAD (064cdea) behavior, captured via a
				// HEAD-built binary: `graft merge --go-patch base.yml bad.yml`
				// piped to a file (stderr is not a TTY, so ansi color renders as
				// plain text). Full-string equality, not substring, catches
				// silent drift in wording, capitalization, or trailing newlines.
				So(stderr, ShouldEqual, "../../assets/go-patch/bad.yml: Root of YAML document is not a hash/map. Tried parsing it as go-patch, but got: [1:3] string was used where mapping is expected\n>  1 | - this\n         ^\n   2 | - isn't\n   3 | - gopatch\n\n\n")
				So(stdout, ShouldEqual, "")
			})
			Convey("go-patch definition parsing errors are byte-exact with HEAD (F1 pin)", func() {
				os.Args = []string{"graft", "merge", "--go-patch", "../../assets/go-patch/base.yml", "../../assets/go-patch/badtype.yml"}
				stdout = ""
				stderr = ""
				main()
				// Byte-exact pin against HEAD (064cdea) behavior. The old
				// cmd/graft parseGoPatch used
				// ansi.Errorf("@R{Unable to parse go-patch definitions: %s\n", err)
				// - an unbalanced brace (no closing "}") that ansi's markup
				// parser can never resolve, so it always printed the "@R{"
				// prefix and capitalized "Unable" literally, regardless of
				// color/TTY state. That literal text IS the observable
				// contract; pkg/graft.ParseGoPatch reproduces it verbatim.
				So(stderr, ShouldEqual, "../../assets/go-patch/badtype.yml: @R{Unable to parse go-patch definitions: Unknown operation [0] with type 'bogus' within\n{\n  \"Type\": \"bogus\",\n  \"Path\": \"/a\"\n}\n\n\n")
				So(stdout, ShouldEqual, "")
			})
			Convey("go-patch parse-error stderr is byte-exact with HEAD under --color=on (F13 pin)", func() {
				defer ansi.Color(false) // restore the test-suite default (see init()) for every later test
				os.Args = []string{"graft", "--color", "on", "merge", "--go-patch", "../../assets/go-patch/base.yml", "../../assets/go-patch/bad.yml"}
				stdout = ""
				stderr = ""
				main()
				// Case B (yaml.Unmarshal failure): HEAD's inner ansi.Errorf had a
				// *balanced* @R{...got} brace, so under color it rendered its own
				// \x1b[31m...\x1b[0m pair; the outer ansi.Errorf("@m{%s}: @R{%s}\n",
				// ...) wrap then nests ANOTHER \x1b[31m in front of that already-
				// colored text (hence the doubled \x1b[31m\x1b[31m) and appends its
				// own \x1b[0m at the very end. Restoring ParseGoPatch's case B to
				// ansi.Errorf (see gopatch_parse.go) reproduces this exactly.
				So(stderr, ShouldEqual, "\x1b[35m../../assets/go-patch/bad.yml\x1b[0m: \x1b[31m\x1b[31mRoot of YAML document is not a hash/map. Tried parsing it as go-patch, but got\x1b[0m: [1:3] string was used where mapping is expected\n>  1 | - this\n         ^\n   2 | - isn't\n   3 | - gopatch\n\x1b[0m\n\n")
				So(stdout, ShouldEqual, "")
			})
			Convey("go-patch definition parsing errors are byte-exact with HEAD under --color=on (F13 pin)", func() {
				defer ansi.Color(false) // restore the test-suite default (see init()) for every later test
				os.Args = []string{"graft", "--color", "on", "merge", "--go-patch", "../../assets/go-patch/base.yml", "../../assets/go-patch/badtype.yml"}
				stdout = ""
				stderr = ""
				main()
				// Case A (NewOpsFromDefinitions failure): the unbalanced-brace
				// "@R{Unable..." text is a literal constant in every color mode
				// (see the F1 pin above), so only the *outer* wrap's @R{%s} markup
				// renders here - a single \x1b[31m right after the literal "@R{"
				// and a single \x1b[0m at the end, unaffected by this fix.
				So(stderr, ShouldEqual, "\x1b[35m../../assets/go-patch/badtype.yml\x1b[0m: @R{\x1b[31mUnable to parse go-patch definitions: Unknown operation [0] with type 'bogus' within\n{\n  \"Type\": \"bogus\",\n  \"Path\": \"/a\"\n}\n\x1b[0m\n\n")
				So(stdout, ShouldEqual, "")
			})
			Convey("go-patch handles named arrays with :before syntax (#283)", func() {
				os.Args = []string{"graft", "merge", "--go-patch", "../../assets/go-patch/base.yml", "../../assets/go-patch/before.yml"}
				stdout = ""
				stderr = ""
				main()
				So(stderr, ShouldEqual, "")
				So(stdout, ShouldEqual, `array:
- 4
- 5
- 6
items:
- name: item7
- name: 7.5
- name: item8
- name: item9
key: 1
key2:
  nested:
    super_nested: 2
  other: 3

`)
			})
			Convey("go-patch applies at its position in the file sequence, not after every regular document (base, patch, overlay: patch applies to base, overlay merges on top)", func() {
				os.Args = []string{"graft", "merge", "--go-patch", "../../assets/go-patch/positional/base.yml", "../../assets/go-patch/positional/patch.yml", "../../assets/go-patch/positional/overlay.yml"}
				stdout = ""
				stderr = ""
				main()
				So(stderr, ShouldEqual, "")
				So(stdout, ShouldEqual, `key: overlay

`)
			})
			Convey("go-patch applied last still overrides an earlier overlay (base, overlay, patch)", func() {
				os.Args = []string{"graft", "merge", "--go-patch", "../../assets/go-patch/positional/base.yml", "../../assets/go-patch/positional/overlay.yml", "../../assets/go-patch/positional/patch.yml"}
				stdout = ""
				stderr = ""
				main()
				So(stderr, ShouldEqual, "")
				So(stdout, ShouldEqual, `key: patched

`)
			})
		})
		Convey("setting DEFAULT_ARRAY_MERGE_KEY", func() {
			_ = os.Setenv("DEFAULT_ARRAY_MERGE_KEY", "id")
			Convey("changes how arrays of maps are merged by default", func() {
				os.Args = []string{"graft", "merge", "../../assets/default-array-merge-var/first.yml", "../../assets/default-array-merge-var/second.yml"}
				stdout = ""
				stderr = ""
				main()
				So(stderr, ShouldEqual, "")
				So(stdout, ShouldEqual, `array:
- id: first
  value: 123
- id: second
  value: 987
- id: third
  value: true

`)
			})
			_ = os.Setenv("DEFAULT_ARRAY_MERGE_KEY", "")
		})

		Convey("Color Options", func() {
			Convey("--color flag validation", func() {
				// Test invalid color option
				os.Args = []string{"graft", "--color", "invalid", "merge", "../../assets/merge/first.yml"}
				stdout = ""
				stderr = ""
				rc = 256

				main()
				So(rc, ShouldEqual, 1)
				So(stderr, ShouldContainSubstring, "Invalid --color option: invalid")
			})

			Convey("--color on forces color output", func() {
				// Create a test file that will produce an error with color
				testFile := "../../assets/test_color.yml"
				err := os.WriteFile(testFile, []byte("test:\n  value: (( grab missing.key ))"), 0o600)
				So(err, ShouldBeNil)
				defer func() { _ = os.Remove(testFile) }()

				os.Args = []string{"graft", "--color", "on", "merge", testFile}
				stdout = ""
				stderr = ""
				rc = 256

				main()
				So(rc, ShouldEqual, 2)
				// Check for ANSI escape sequences in error output
				So(stderr, ShouldContainSubstring, "\x1b[")
			})

			Convey("--color off disables color output", func() {
				// Create a test file that will produce an error without color
				testFile := "../../assets/test_color_off.yml"
				err := os.WriteFile(testFile, []byte("test:\n  value: (( grab missing.key ))"), 0o600)
				So(err, ShouldBeNil)
				defer func() { _ = os.Remove(testFile) }()

				os.Args = []string{"graft", "--color", "off", "merge", testFile}
				stdout = ""
				stderr = ""
				rc = 256

				main()
				So(rc, ShouldEqual, 2)
				// Check that no ANSI escape sequences are in error output
				So(stderr, ShouldNotContainSubstring, "\x1b[")
			})
		})

		Convey("Config Options", func() {
			Convey("--config flag absent leaves default behavior unchanged", func() {
				os.Args = []string{"graft", "merge", "../../assets/merge/first.yml", "../../assets/merge/second.yml"}
				stdout = ""
				stderr = ""
				rc = 256
				main()
				So(rc, ShouldEqual, 0)
				So(stderr, ShouldEqual, "")
				baseline := stdout

				// An empty config file must not change the merge result versus
				// no --config flag at all (internal/config.DefaultConfig() sets
				// Cache.Enabled=true, but that must not silently flip behavior).
				emptyConfig := filepath.Join(t.TempDir(), "empty-config.yaml")
				err := os.WriteFile(emptyConfig, []byte(""), 0o600)
				So(err, ShouldBeNil)

				os.Args = []string{"graft", "--config", emptyConfig, "merge", "../../assets/merge/first.yml", "../../assets/merge/second.yml"}
				stdout = ""
				stderr = ""
				rc = 256
				main()
				So(rc, ShouldEqual, 0)
				So(stderr, ShouldEqual, "")
				So(stdout, ShouldEqual, baseline)
			})

			Convey("--config flag with a valid non-empty file merges successfully", func() {
				cfgFile := filepath.Join(t.TempDir(), "config.yaml")
				err := os.WriteFile(cfgFile, []byte("logging:\n  level: debug\n  format: json\n"), 0o600)
				So(err, ShouldBeNil)

				os.Args = []string{"graft", "--config", cfgFile, "merge", "../../assets/merge/first.yml", "../../assets/merge/second.yml"}
				stdout = ""
				stderr = ""
				rc = 256
				main()
				So(rc, ShouldEqual, 0)
				So(stderr, ShouldEqual, "")
				So(stdout, ShouldNotEqual, "")
			})

			Convey("--config flag with a missing file fails with a clear error", func() {
				os.Args = []string{"graft", "--config", "../../assets/does-not-exist-config.yaml", "merge", "../../assets/merge/first.yml"}
				stdout = ""
				stderr = ""
				rc = 256
				main()
				So(rc, ShouldNotEqual, 0)
				So(stderr, ShouldContainSubstring, "does-not-exist-config.yaml")
			})

			Convey("--config flag with an unparseable file fails with a clear error", func() {
				badConfig := filepath.Join(t.TempDir(), "bad-config.yaml")
				err := os.WriteFile(badConfig, []byte("engine:\n  strict_mode: [unterminated\n"), 0o600)
				So(err, ShouldBeNil)

				os.Args = []string{"graft", "--config", badConfig, "merge", "../../assets/merge/first.yml"}
				stdout = ""
				stderr = ""
				rc = 256
				main()
				So(rc, ShouldNotEqual, 0)
				So(stderr, ShouldNotEqual, "")
			})

			Convey("--config flag with an invalid config value fails validation", func() {
				invalidConfig := filepath.Join(t.TempDir(), "invalid-config.yaml")
				err := os.WriteFile(invalidConfig, []byte("logging:\n  level: not-a-real-level\n"), 0o600)
				So(err, ShouldBeNil)

				os.Args = []string{"graft", "--config", invalidConfig, "merge", "../../assets/merge/first.yml"}
				stdout = ""
				stderr = ""
				rc = 256
				main()
				So(rc, ShouldNotEqual, 0)
				So(stderr, ShouldContainSubstring, "logging.level")
			})
		})

		Convey("Feature Flag Options", func() {
			Convey("no GRAFT_FEATURE_* vars leaves default behavior unchanged", func() {
				os.Args = []string{"graft", "merge", "../../assets/merge/first.yml", "../../assets/merge/second.yml"}
				stdout = ""
				stderr = ""
				rc = 256
				main()
				So(rc, ShouldEqual, 0)
				So(stderr, ShouldEqual, "")
				baseline := stdout

				// GRAFT_FEATURE_CACHE only toggles whether the engine builds an
				// internal result cache (a performance optimization); it must
				// not change merge output content.
				_ = os.Setenv("GRAFT_FEATURE_CACHE", "false")
				defer func() { _ = os.Unsetenv("GRAFT_FEATURE_CACHE") }()

				os.Args = []string{"graft", "merge", "../../assets/merge/first.yml", "../../assets/merge/second.yml"}
				stdout = ""
				stderr = ""
				rc = 256
				main()
				So(rc, ShouldEqual, 0)
				So(stderr, ShouldEqual, "")
				So(stdout, ShouldEqual, baseline)
			})

			Convey("an unrecognized GRAFT_FEATURE_CACHE value is ignored, not a fatal error", func() {
				_ = os.Setenv("GRAFT_FEATURE_CACHE", "not-a-real-bool")
				defer func() { _ = os.Unsetenv("GRAFT_FEATURE_CACHE") }()

				os.Args = []string{"graft", "merge", "../../assets/merge/first.yml", "../../assets/merge/second.yml"}
				stdout = ""
				stderr = ""
				rc = 256
				main()
				So(rc, ShouldEqual, 0)
				So(stderr, ShouldEqual, "")
			})
		})

		Convey("Env Var Defaults Explicitly Set", func() {
			Convey("every GRAFT_*/GRAFT_FEATURE_* var pinned to its own default leaves output unchanged", func() {
				os.Args = []string{"graft", "merge", "../../assets/merge/first.yml", "../../assets/merge/second.yml"}
				stdout = ""
				stderr = ""
				rc = 256
				main()
				So(rc, ShouldEqual, 0)
				So(stderr, ShouldEqual, "")
				baseline := stdout

				// internal/config's own defaults (config.DefaultConfig()) and
				// internal/features' own defaults (features.DefaultFlags()),
				// pinned explicitly via env instead of left unset. Proves the
				// env tier is a true no-op when every var's value matches the
				// default it would resolve to anyway - not just that one
				// setting doesn't observably affect output (the "Feature Flag
				// Options" cases above), but that ALL of them set at once still
				// reproduce the exact same baseline as leaving everything
				// unset. GRAFT_CACHE_L2_PATH is excluded: its default is "" and
				// internal/config/env.go treats an empty env value as unset
				// (applyCacheEnv only assigns when val != ""), so there is no
				// way to "explicitly set" it to its own default that's
				// distinguishable from leaving it unset.
				defaultEnv := map[string]string{
					config.EnvEngineStrictMode:     "false",
					config.EnvEngineMaxRecursion:   "100",
					config.EnvEngineTimeout:        "30s",
					config.EnvCacheEnabled:         "true",
					config.EnvCacheMaxSize:         "10000",
					config.EnvCacheTTL:             "5m",
					config.EnvCacheL2Enabled:       "false",
					config.EnvParallelEnabled:      "true",
					config.EnvParallelMinWorkers:   "1",
					config.EnvParallelMaxWorkers:   "0",
					config.EnvMetricsEnabled:       "false",
					config.EnvMetricsFormat:        "prometheus",
					config.EnvMetricsEndpoint:      "/metrics",
					config.EnvLoggingLevel:         "info",
					config.EnvLoggingFormat:        "text",
					features.EnvFeatureParallel:    "false",
					features.EnvFeatureCache:       "true",
					features.EnvFeatureMetrics:     "false",
					features.EnvFeatureDebug:       "false",
					features.EnvFeatureStrictTypes: "false",
					features.EnvFeaturePools:       "true",
				}
				for name, val := range defaultEnv {
					_ = os.Setenv(name, val)
				}
				defer func() {
					for name := range defaultEnv {
						_ = os.Unsetenv(name)
					}
				}()

				os.Args = []string{"graft", "merge", "../../assets/merge/first.yml", "../../assets/merge/second.yml"}
				stdout = ""
				stderr = ""
				rc = 256
				main()
				So(rc, ShouldEqual, 0)
				So(stderr, ShouldEqual, "")
				So(stdout, ShouldEqual, baseline)
			})
		})

		Convey("merge history/tracing flags", func() {
			Convey("--history prints per-path history instead of the merged document", func() {
				os.Args = []string{"graft", "merge", "--history", "../../assets/diff/base.yml", "../../assets/diff/modified.yml"}
				stdout = ""
				stderr = ""
				rc = 256
				main()
				So(stderr, ShouldEqual, "")
				So(rc, ShouldEqual, 0)
				So(stdout, ShouldStartWith, "Merge History:\n")
				So(stdout, ShouldContainSubstring, "database.host:\n")
				So(stdout, ShouldContainSubstring, "[0] ../../assets/diff/base.yml → localhost")
				So(stdout, ShouldContainSubstring, "[1] ../../assets/diff/modified.yml → db.prod.example.com")
				So(stdout, ShouldContainSubstring, "Final              → db.prod.example.com")
				So(stdout, ShouldContainSubstring, "database.port:\n")
				So(stdout, ShouldContainSubstring, "(unchanged)")
			})

			Convey("--trace-path prints one path's annotated history", func() {
				os.Args = []string{"graft", "merge", "--trace-path", "database.host", "../../assets/diff/base.yml", "../../assets/diff/modified.yml"}
				stdout = ""
				stderr = ""
				rc = 256
				main()
				So(stderr, ShouldEqual, "")
				So(rc, ShouldEqual, 0)
				So(stdout, ShouldStartWith, "database.host:\n")
				So(stdout, ShouldContainSubstring, "Type: value")
				So(stdout, ShouldContainSubstring, "Final              → db.prod.example.com")
			})

			Convey("--trace-path on an unknown path is an error", func() {
				os.Args = []string{"graft", "merge", "--trace-path", "no.such.path", "../../assets/diff/base.yml", "../../assets/diff/modified.yml"}
				stdout = ""
				stderr = ""
				rc = 256
				main()
				So(stdout, ShouldEqual, "")
				So(stderr, ShouldContainSubstring, "No history found for path")
				So(stderr, ShouldContainSubstring, "no.such.path")
				So(rc, ShouldEqual, 2)
			})

			Convey("--show-changes prints a summary and only changed/added/removed paths", func() {
				os.Args = []string{"graft", "merge", "--show-changes", "../../assets/diff/base.yml", "../../assets/diff/modified.yml"}
				stdout = ""
				stderr = ""
				rc = 256
				main()
				So(stderr, ShouldEqual, "")
				So(rc, ShouldEqual, 0)
				So(stdout, ShouldStartWith, "Merge Summary: 2 files → 5 keys (2 changed, 1 added, 0 removed)\n")
				So(stdout, ShouldContainSubstring, "database.host:\n")
				So(stdout, ShouldContainSubstring, "✗")
				So(stdout, ShouldContainSubstring, "✓")
				So(stdout, ShouldContainSubstring, "database.ssl:\n")
				So(stdout, ShouldContainSubstring, "+ ../../assets/diff/modified.yml true")
				// database.port and meta.version are unchanged and must not appear.
				So(stdout, ShouldNotContainSubstring, "database.port")
			})

			Convey("--changes-only lists only changed paths with old -> new values", func() {
				os.Args = []string{"graft", "merge", "--changes-only", "../../assets/diff/base.yml", "../../assets/diff/modified.yml"}
				stdout = ""
				stderr = ""
				rc = 256
				main()
				So(stderr, ShouldEqual, "")
				So(rc, ShouldEqual, 0)
				So(stdout, ShouldStartWith, "Changed paths (3 paths of 5):\n")
				So(stdout, ShouldContainSubstring, "database.host")
				So(stdout, ShouldContainSubstring, "localhost → db.prod.example.com")
				So(stdout, ShouldContainSubstring, "database.ssl")
				So(stdout, ShouldContainSubstring, "<none> → true")
			})

			Convey("--history reflects a --prune removal as a POST-phase entry", func() {
				os.Args = []string{"graft", "merge", "--history", "--prune", "meta", "../../assets/diff/base.yml", "../../assets/diff/modified.yml"}
				stdout = ""
				stderr = ""
				rc = 256
				main()
				So(stderr, ShouldEqual, "")
				So(rc, ShouldEqual, 0)
				So(stdout, ShouldContainSubstring, "meta.version:\n")
				So(stdout, ShouldContainSubstring, "<pruned>")
			})

			Convey("combining --history and --show-changes is a usage error", func() {
				os.Args = []string{"graft", "merge", "--history", "--show-changes", "../../assets/diff/base.yml", "../../assets/diff/modified.yml"}
				stdout = ""
				stderr = ""
				rc = 256
				main()
				So(stdout, ShouldEqual, "")
				So(stderr, ShouldContainSubstring, "mutually exclusive")
				So(rc, ShouldEqual, 1)
			})
		})

		Convey("diff command", func() {
			Convey("exits 0 and reports no differences for identical files", func() {
				os.Args = []string{"graft", "diff", "../../assets/merge/first.yml", "../../assets/merge/first.yml"}
				stdout = ""
				stderr = ""
				rc = 256
				main()
				So(rc, ShouldEqual, 0)
				So(stderr, ShouldEqual, "")
				So(stdout, ShouldEqual, "\n\n")
			})

			Convey("exits 1 and reports differences for differing files, uncolored when stdout is not a tty", func() {
				os.Args = []string{"graft", "diff", "../../assets/merge/first.yml", "../../assets/merge/second.yml"}
				stdout = ""
				stderr = ""
				rc = 256
				main()
				So(rc, ShouldEqual, 1)
				So(stderr, ShouldEqual, "")
				So(stdout, ShouldNotEqual, "")
				// go test's stdout is never a tty (it's captured by the
				// test binary), so the default "auto" color detection
				// must emit no ANSI escapes here: diff coloring is gated
				// on isatty(stdout), matching spruce, not forced on.
				So(stdout, ShouldNotContainSubstring, "\x1b[")
			})

			Convey("exits 2 on a load error", func() {
				os.Args = []string{"graft", "diff", "../../assets/merge/first.yml", "../../assets/merge/does-not-exist.yml"}
				stdout = ""
				stderr = ""
				rc = 256
				main()
				So(rc, ShouldEqual, 2)
				So(stdout, ShouldEqual, "")
				So(stderr, ShouldNotEqual, "")
			})

			Convey("exits 1 with usage when given the wrong number of files", func() {
				os.Args = []string{"graft", "diff", "../../assets/merge/first.yml"}
				stdout = ""
				stderr = ""
				rc = 256
				main()
				So(rc, ShouldEqual, 1)
			})

			Convey("--color=off and --color=on are both honored without error and stay uncolored off a real tty", func() {
				os.Args = []string{"graft", "diff", "--color", "off", "../../assets/merge/first.yml", "../../assets/merge/second.yml"}
				stdout = ""
				stderr = ""
				rc = 256
				main()
				So(rc, ShouldEqual, 1)
				So(stdout, ShouldNotContainSubstring, "\x1b[")

				os.Args = []string{"graft", "diff", "--color", "on", "../../assets/merge/first.yml", "../../assets/merge/second.yml"}
				stdout = ""
				stderr = ""
				rc = 256
				main()
				So(rc, ShouldEqual, 1)
			})

			Convey("--color=bogus is rejected as a usage error", func() {
				os.Args = []string{"graft", "diff", "--color", "bogus", "../../assets/merge/first.yml", "../../assets/merge/second.yml"}
				stdout = ""
				stderr = ""
				rc = 256
				main()
				So(rc, ShouldEqual, 1)
			})

			Convey("--changes lists changes grouped by kind", func() {
				os.Args = []string{"graft", "diff", "--changes", "../../assets/diff/base.yml", "../../assets/diff/modified.yml"}
				stdout = ""
				stderr = ""
				rc = 256
				main()
				So(stderr, ShouldEqual, "")
				So(rc, ShouldEqual, 1)
				So(stdout, ShouldStartWith, "Changes (2 modified, 1 added, 1 removed):\n")
				So(stdout, ShouldContainSubstring, "MODIFIED  database.host")
				So(stdout, ShouldContainSubstring, "- localhost")
				So(stdout, ShouldContainSubstring, "+ db.prod.example.com")
				So(stdout, ShouldContainSubstring, "ADDED     database.ssl")
				So(stdout, ShouldContainSubstring, "REMOVED   meta")
			})

			Convey("--changes on identical files reports zero changes and exits 0", func() {
				os.Args = []string{"graft", "diff", "--changes", "../../assets/diff/base.yml", "../../assets/diff/base.yml"}
				stdout = ""
				stderr = ""
				rc = 256
				main()
				So(stderr, ShouldEqual, "")
				So(rc, ShouldEqual, 0)
				So(stdout, ShouldEqual, "Changes (0 modified, 0 added, 0 removed):\n")
			})

			Convey("--unified renders a git-style diff grouped by top-level key", func() {
				os.Args = []string{"graft", "diff", "--unified", "../../assets/diff/base.yml", "../../assets/diff/modified.yml"}
				stdout = ""
				stderr = ""
				rc = 256
				main()
				So(stderr, ShouldEqual, "")
				So(rc, ShouldEqual, 1)
				So(stdout, ShouldStartWith, "--- ../../assets/diff/base.yml\n+++ ../../assets/diff/modified.yml\n")
				So(stdout, ShouldContainSubstring, "@@ database @@")
				So(stdout, ShouldContainSubstring, "@@ meta @@")
				So(stdout, ShouldContainSubstring, "-  host: localhost")
				So(stdout, ShouldContainSubstring, "+  host: db.prod.example.com")
			})

			Convey("--unified --context=0 is accepted", func() {
				os.Args = []string{"graft", "diff", "--unified", "--context", "0", "../../assets/diff/base.yml", "../../assets/diff/modified.yml"}
				stdout = ""
				stderr = ""
				rc = 256
				main()
				So(stderr, ShouldEqual, "")
				So(rc, ShouldEqual, 1)
				So(stdout, ShouldContainSubstring, "@@ database @@")
			})

			Convey("--side-by-side renders two columns separated by a divider", func() {
				os.Args = []string{"graft", "diff", "--side-by-side", "../../assets/diff/base.yml", "../../assets/diff/modified.yml"}
				stdout = ""
				stderr = ""
				rc = 256
				main()
				So(stderr, ShouldEqual, "")
				So(rc, ShouldEqual, 1)
				So(stdout, ShouldContainSubstring, "../../assets/diff/base.yml")
				So(stdout, ShouldContainSubstring, "../../assets/diff/modified.yml")
				So(stdout, ShouldContainSubstring, "┼")
			})

			Convey("--side-by-side --width controls the column width", func() {
				os.Args = []string{"graft", "diff", "--side-by-side", "--width", "40", "../../assets/diff/base.yml", "../../assets/diff/modified.yml"}
				stdout = ""
				stderr = ""
				rc = 256
				main()
				So(stderr, ShouldEqual, "")
				So(rc, ShouldEqual, 1)
				lines := strings.Split(stdout, "\n")
				So(utf8.RuneCountInString(lines[0]), ShouldBeLessThanOrEqualTo, 44)
			})

			Convey("--quiet suppresses output but keeps the differences exit code", func() {
				os.Args = []string{"graft", "diff", "--changes", "--quiet", "../../assets/diff/base.yml", "../../assets/diff/modified.yml"}
				stdout = ""
				stderr = ""
				rc = 256
				main()
				So(stderr, ShouldEqual, "")
				So(stdout, ShouldEqual, "")
				So(rc, ShouldEqual, 1)
			})

			Convey("combining --changes and --unified is a usage error", func() {
				os.Args = []string{"graft", "diff", "--changes", "--unified", "../../assets/diff/base.yml", "../../assets/diff/modified.yml"}
				stdout = ""
				stderr = ""
				rc = 256
				main()
				So(rc, ShouldEqual, 1)
				So(stderr, ShouldContainSubstring, "mutually exclusive")
			})

			Convey("--no-color disables color even when --color=on is given", func() {
				os.Args = []string{"graft", "diff", "--color", "on", "--no-color", "--changes", "../../assets/diff/base.yml", "../../assets/diff/modified.yml"}
				stdout = ""
				stderr = ""
				rc = 256
				main()
				So(rc, ShouldEqual, 1)
				So(stdout, ShouldNotContainSubstring, "\x1b[")
			})
		})
	})
}

// TestHandleColorFlag locks the --color decision logic used by handleDiff
// and the root command's PersistentPreRunE: "on"/"off" are explicit
// overrides, "auto"/"" defer to isatty(stderr), and anything else is
// rejected.
func TestHandleColorFlag(t *testing.T) {
	Convey("handleColorFlag()", t, func() {
		Convey("'on' forces color on and is valid", func() {
			enabled, valid := handleColorFlag("on")
			So(valid, ShouldBeTrue)
			So(enabled, ShouldBeTrue)
		})
		Convey("'off' forces color off and is valid", func() {
			enabled, valid := handleColorFlag("off")
			So(valid, ShouldBeTrue)
			So(enabled, ShouldBeFalse)
		})
		Convey("'auto' and '' defer to isatty(stderr) and are valid", func() {
			want := isatty.IsTerminal(os.Stderr.Fd())
			enabled, valid := handleColorFlag("auto")
			So(valid, ShouldBeTrue)
			So(enabled, ShouldEqual, want)

			enabled, valid = handleColorFlag("")
			So(valid, ShouldBeTrue)
			So(enabled, ShouldEqual, want)
		})
		Convey("an unrecognized value is invalid", func() {
			enabled, valid := handleColorFlag("bogus")
			So(valid, ShouldBeFalse)
			So(enabled, ShouldBeFalse)
		})
	})
}

// TestDiffFiles locks diffFiles()'s exit-code-relevant return values
// (hasDifferences, err) independent of the CLI plumbing in handleDiff, and
// confirms its report body carries no ANSI escapes when stdout is not a
// tty (spruce parity: dyff/bunt's own isatty(stdout) auto-detection gates
// diff coloring, not graft's --color flag).
func TestDiffFiles(t *testing.T) {
	Convey("diffFiles()", t, func() {
		Convey("identical files report no differences", func() {
			output, hasDifferences, err := diffFiles([]string{"../../assets/merge/first.yml", "../../assets/merge/first.yml"})
			So(err, ShouldBeNil)
			So(hasDifferences, ShouldBeFalse)
			So(output, ShouldEqual, "\n")
		})
		Convey("differing files report differences with no ANSI escapes off a tty", func() {
			output, hasDifferences, err := diffFiles([]string{"../../assets/merge/first.yml", "../../assets/merge/second.yml"})
			So(err, ShouldBeNil)
			So(hasDifferences, ShouldBeTrue)
			So(output, ShouldNotBeEmpty)
			So(output, ShouldNotContainSubstring, "\x1b[")
		})
		Convey("a missing file is a load error", func() {
			_, _, err := diffFiles([]string{"../../assets/merge/first.yml", "../../assets/merge/does-not-exist.yml"})
			So(err, ShouldNotBeNil)
		})
		Convey("any count other than two files is a usage error", func() {
			_, _, err := diffFiles([]string{"../../assets/merge/first.yml"})
			So(err, ShouldNotBeNil)

			_, _, err = diffFiles([]string{"../../assets/merge/first.yml", "../../assets/merge/second.yml", "../../assets/merge/first.yml"})
			So(err, ShouldNotBeNil)
		})
	})
}

func TestResolveStartupConfigEnvOverridesFile(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte("logging:\n  level: debug\n"), 0o600); err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}

	_ = os.Setenv("GRAFT_LOGGING_LEVEL", "error")
	defer func() { _ = os.Unsetenv("GRAFT_LOGGING_LEVEL") }()

	cfg, err := resolveStartupConfig(configPath)
	if err != nil {
		t.Fatalf("resolveStartupConfig() error = %v", err)
	}

	if cfg.Logging.Level != "error" {
		t.Errorf("Expected GRAFT_LOGGING_LEVEL to override file's logging.level 'debug', got %q", cfg.Logging.Level)
	}
}

func TestResolveStartupConfigEnvOverridesDefault(t *testing.T) {
	_ = os.Setenv("GRAFT_CACHE_ENABLED", "false")
	defer func() { _ = os.Unsetenv("GRAFT_CACHE_ENABLED") }()

	cfg, err := resolveStartupConfig("")
	if err != nil {
		t.Fatalf("resolveStartupConfig() error = %v", err)
	}

	if cfg.Cache.Enabled {
		t.Error("Expected GRAFT_CACHE_ENABLED=false to override default Cache.Enabled=true")
	}
}

func TestResolveStartupConfigInvalidEnvOverrideFails(t *testing.T) {
	_ = os.Setenv("GRAFT_LOGGING_LEVEL", "not-a-real-level")
	defer func() { _ = os.Unsetenv("GRAFT_LOGGING_LEVEL") }()

	_, err := resolveStartupConfig("")
	if err == nil {
		t.Fatal("Expected error for invalid GRAFT_LOGGING_LEVEL, got nil")
	}
	if !strings.Contains(err.Error(), "logging.level") {
		t.Errorf("Expected error to mention 'logging.level', got: %v", err)
	}
}

func TestResolveStartupFeatureFlagsNoEnvMatchesDefaults(t *testing.T) {
	ff := resolveStartupFeatureFlags()

	want := features.DefaultFlags().GetAll()
	got := ff.GetAll()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("resolveStartupFeatureFlags() with no GRAFT_FEATURE_* vars set = %v, want default flags %v", got, want)
	}
}

func TestResolveStartupFeatureFlagsEnvOverridesDefault(t *testing.T) {
	_ = os.Setenv("GRAFT_FEATURE_CACHE", "false")
	defer func() { _ = os.Unsetenv("GRAFT_FEATURE_CACHE") }()

	ff := resolveStartupFeatureFlags()

	if ff.IsEnabled(features.FeatureCaching) {
		t.Error("Expected GRAFT_FEATURE_CACHE=false to override default FeatureCaching=true")
	}
	// Unrelated flags stay at their defaults - only the named env var's flag changes.
	if !ff.IsEnabled(features.FeatureMemoryPools) {
		t.Error("Expected FeatureMemoryPools to remain at its default (true) when only GRAFT_FEATURE_CACHE is set")
	}
	if ff.IsEnabled(features.FeatureParallelEvaluation) {
		t.Error("Expected FeatureParallelEvaluation to remain at its default (false) when only GRAFT_FEATURE_CACHE is set")
	}
}

func TestResolveStartupFeatureFlagsInvalidEnvValueIgnored(t *testing.T) {
	// internal/features.LoadFromEnv documents that an unparseable value is
	// ignored (flag keeps its prior value), unlike resolveStartupConfig's
	// GRAFT_* vars which fail validation. No error path exists to test here;
	// this asserts the flag is left at its default instead.
	_ = os.Setenv("GRAFT_FEATURE_CACHE", "not-a-real-bool")
	defer func() { _ = os.Unsetenv("GRAFT_FEATURE_CACHE") }()

	ff := resolveStartupFeatureFlags()

	if !ff.IsEnabled(features.FeatureCaching) {
		t.Error("Expected unparseable GRAFT_FEATURE_CACHE value to be ignored, leaving FeatureCaching at its default (true)")
	}
}

// TestConfigEngineOptsWiresFeatureFlags proves configEngineOpts's
// WithFeatureFlags wiring observably changes engine construction: disabling
// features.FeatureCaching (as GRAFT_FEATURE_CACHE=false would resolve to via
// resolveStartupFeatureFlags) suppresses the engine's internal cache
// instance even though mergeAllDocs always requests graft.WithCache(true, ..).
func TestConfigEngineOptsWiresFeatureFlags(t *testing.T) {
	newEngineWithCache := func(ff *features.FeatureFlags) *graft.DefaultEngine {
		opts := append(configEngineOpts(nil, ff), graft.WithCache(true, 1000))
		engine, err := graft.NewEngine(opts...)
		if err != nil {
			t.Fatalf("graft.NewEngine() error = %v", err)
		}
		de, ok := engine.(*graft.DefaultEngine)
		if !ok {
			t.Fatalf("graft.NewEngine() returned %T, want *graft.DefaultEngine", engine)
		}
		return de
	}

	baseline := newEngineWithCache(features.DefaultFlags())
	if baseline.GetCache() == nil {
		t.Error("Expected a cache instance with default feature flags (FeatureCaching enabled) and WithCache(true, ..)")
	}

	disabledCache := features.DefaultFlags()
	disabledCache.Set(features.FeatureCaching, false)
	withCacheDisabled := newEngineWithCache(disabledCache)
	if withCacheDisabled.GetCache() != nil {
		t.Error("Expected no cache instance when FeatureCaching is disabled via feature flags, even though WithCache(true, ..) was requested")
	}
}

func TestConfigEngineOptsNilInputsProduceNoOptions(t *testing.T) {
	opts := configEngineOpts(nil, nil)
	if len(opts) != 0 {
		t.Errorf("configEngineOpts(nil, nil) returned %d options, want 0", len(opts))
	}
}

func TestDebug(t *testing.T) {
	usage = func() {}
	Convey("debug flags:", t, func() {
		Convey("-D enables debugging", func() {
			os.Args = []string{"graft", "-D"}
			log.DebugOn = false
			main()
			So(log.DebugOn, ShouldBeTrue)
		})
		Convey("--debug enables debugging", func() {
			os.Args = []string{"graft", "--debug"}
			log.DebugOn = false
			main()
			So(log.DebugOn, ShouldBeTrue)
		})
		Convey("DEBUG=\"tRuE\" enables debugging", func() {
			_ = os.Setenv("DEBUG", "tRuE")
			os.Args = []string{"graft"}
			log.DebugOn = false
			main()
			So(log.DebugOn, ShouldBeTrue)
		})
		Convey("DEBUG=1 enables debugging", func() {
			_ = os.Setenv("DEBUG", "1")
			os.Args = []string{"graft"}
			log.DebugOn = false
			main()
			So(log.DebugOn, ShouldBeTrue)
		})
		Convey("DEBUG=randomval enables debugging", func() {
			_ = os.Setenv("DEBUG", "randomval")
			os.Args = []string{"graft"}
			log.DebugOn = false
			main()
			So(log.DebugOn, ShouldBeTrue)
		})
		Convey("DEBUG=\"fAlSe\" disables debugging", func() {
			_ = os.Setenv("DEBUG", "fAlSe")
			os.Args = []string{"graft"}
			log.DebugOn = false
			main()
			So(log.DebugOn, ShouldBeFalse)
		})
		Convey("DEBUG=0 disables debugging", func() {
			_ = os.Setenv("DEBUG", "0")
			os.Args = []string{"graft"}
			log.DebugOn = false
			main()
			So(log.DebugOn, ShouldBeFalse)
		})
		Convey("DEBUG=\"\" disables debugging", func() {
			_ = os.Setenv("DEBUG", "")
			os.Args = []string{"graft"}
			log.DebugOn = false
			main()
			So(log.DebugOn, ShouldBeFalse)
		})
	})
}

func TestFan(t *testing.T) {
	var stdout string
	printStdOutf = func(format string, args ...interface{}) {
		stdout += fmt.Sprintf(format, args...)
	}
	var stderr string
	// Edit log stderr function
	log.PrintStdErrf = func(format string, args ...interface{}) {
		stderr += fmt.Sprintf(format, args...)
	}

	rc := 256 // invalid return code to catch any issues
	exit = func(code int) {
		rc = code
	}

	usage = func() {
		stderr = "usage was called"
		exit(1)
	}

	Convey("graft fan errors when failing to read a file it was given", t, func() {
		os.Args = []string{"graft", "fan", "../../assets/fan/nonexistent.yml", "../../assets/fan/multi-doc-1.yml"}
		stdout = ""
		stderr = ""
		main()
		So(stderr, ShouldContainSubstring, "Error reading file ../../assets/fan/nonexistent.yml: open ../../assets/fan/nonexistent.yml: no such file or directory")
		So(stdout, ShouldEqual, "")
		So(rc, ShouldEqual, 2)
	})
	Convey("graft fan errors with the correct document index when there's an initial doc-separator", t, func() {
		os.Args = []string{"graft", "fan", "../../assets/fan/source.yml", "../../assets/fan/invalid-yaml-with-doc-separator.yml"}
		stdout = ""
		stderr = ""
		main()
		So(stderr, ShouldContainSubstring, "../../assets/fan/invalid-yaml-with-doc-separator.yml[0]:")
		So(stdout, ShouldEqual, "")
		So(rc, ShouldEqual, 2)
	})
	Convey("graft fan errors with the correct doc index when there is no initial doc separator", t, func() {
		os.Args = []string{"graft", "fan", "../../assets/fan/source.yml", "../../assets/fan/invalid-yaml.yml"}
		stdout = ""
		stderr = ""
		main()
		So(stderr, ShouldContainSubstring, "../../assets/fan/invalid-yaml.yml[0]:")
		So(stdout, ShouldEqual, "")
		So(rc, ShouldEqual, 2)
	})
	Convey("graft fan errors if no source file is provided", t, func() {
		os.Args = []string{"graft", "fan"}
		stdout = ""
		stderr = ""
		main()
		So(stderr, ShouldContainSubstring, "You must specify at least a source document to graft fan. If no files are specified, STDIN is used. Using STDIN for source and target docs only works with -m")
		So(stdout, ShouldEqual, "")
		So(rc, ShouldEqual, 2)
	})
	Convey("graft fan merges one doc into all the docs of the other files", t, func() {
		os.Args = []string{"graft", "fan", "--prune", "meta", "../../assets/fan/source.yml", "../../assets/fan/multi-doc-1.yml", "../../assets/fan/multi-doc-2.yml", "../../assets/fan/multi-doc-3.yml"}
		stdout = ""
		stderr = ""
		main()
		So(stderr, ShouldEqual, "")
		So(stdout, ShouldEqual, `---
doc1: i've-been-grabbed

---
doc2: i've-been-grabbed
other: stuff

---
doc3: i've-been-grabbed

---
no-grab: here

---
doc4: i've-been-grabbed

---
doc5: i've-been-grabbed
other: stuff

---
doc6:
  no-grab: here

---
doc7:
  no-grab: here

`)
		So(rc, ShouldEqual, 0)
	})
	Convey("graft fan merges a multi doc source into all the docs of the other files", t, func() {
		os.Args = []string{"graft", "fan", "-m", "--prune", "meta", "../../assets/fan/multi-doc-source.yml", "../../assets/fan/multi-doc-1.yml", "../../assets/fan/multi-doc-2.yml", "../../assets/fan/multi-doc-3.yml"}
		stdout = ""
		stderr = ""
		main()
		So(stderr, ShouldEqual, "")
		So(stdout, ShouldEqual, `---
sdoc: i've-been-grabbed

---
doc1: i've-been-grabbed

---
doc2: i've-been-grabbed
other: stuff

---
doc3: i've-been-grabbed

---
no-grab: here

---
doc4: i've-been-grabbed

---
doc5: i've-been-grabbed
other: stuff

---
doc6:
  no-grab: here

---
doc7:
  no-grab: here

`)
		So(rc, ShouldEqual, 0)
	})

	Convey("graft fan expands a directory target argument into its .yml/.yaml/.json files, sorted, skipping dotfiles and non-config files", t, func() {
		os.Args = []string{"graft", "fan", "../../assets/fan/dirsource.yml", "../../assets/fan/targets/"}
		stdout = ""
		stderr = ""
		main()
		So(stderr, ShouldEqual, "")
		So(stdout, ShouldEqual, `---
application: my-app
meta:
  environment: development
  name: development

---
application: my-app
meta:
  environment: production
  name: production

`)
		So(rc, ShouldEqual, 0)
	})

	Convey("graft fan --output-dir writes one file per target instead of writing to stdout", t, func() {
		outDir := t.TempDir()
		os.Args = []string{"graft", "fan", "--output-dir", outDir, "../../assets/fan/dirsource.yml", "../../assets/fan/targets/dev.yml", "../../assets/fan/targets/prod.yml"}
		stdout = ""
		stderr = ""
		main()
		So(stderr, ShouldEqual, "")
		So(stdout, ShouldEqual, "")
		So(rc, ShouldEqual, 0)

		devOut, err := os.ReadFile(filepath.Join(outDir, "dev.yml"))
		So(err, ShouldBeNil)
		So(string(devOut), ShouldEqual, "application: my-app\nmeta:\n  environment: development\n  name: development\n")

		prodOut, err := os.ReadFile(filepath.Join(outDir, "prod.yml"))
		So(err, ShouldBeNil)
		So(string(prodOut), ShouldEqual, "application: my-app\nmeta:\n  environment: production\n  name: production\n")
	})

	Convey("graft fan --output-dir with a directory target creates the output directory and names files after the target files", t, func() {
		outDir := filepath.Join(t.TempDir(), "nested", "output")
		os.Args = []string{"graft", "fan", "-o", outDir, "../../assets/fan/dirsource.yml", "../../assets/fan/targets/"}
		stdout = ""
		stderr = ""
		main()
		So(stderr, ShouldEqual, "")
		So(stdout, ShouldEqual, "")
		So(rc, ShouldEqual, 0)

		entries, err := os.ReadDir(outDir)
		So(err, ShouldBeNil)
		names := []string{}
		for _, e := range entries {
			names = append(names, e.Name())
		}
		So(names, ShouldResemble, []string{"dev.yml", "prod.yml"})
	})

	Convey("graft fan errors on an empty target directory", t, func() {
		emptyDir := t.TempDir()
		os.Args = []string{"graft", "fan", "../../assets/fan/dirsource.yml", emptyDir}
		stdout = ""
		stderr = ""
		main()
		So(stdout, ShouldEqual, "")
		So(stderr, ShouldContainSubstring, "contains no .yml/.yaml/.json files")
		So(rc, ShouldEqual, 2)
	})

	Convey("graft fan with explicit file arguments does not also read a non-empty stdin (regression: cmdFanEval used to unconditionally append '-')", t, func() {
		restoreStdin := setStdinFromFile(t, "../../assets/vaultinfo/novault.yml")
		defer restoreStdin()

		os.Args = []string{"graft", "fan", "../../assets/fan/dirsource.yml", "../../assets/fan/targets/dev.yml"}
		stdout = ""
		stderr = ""
		main()
		So(stderr, ShouldEqual, "")
		So(rc, ShouldEqual, 0)
		// Only one document merged (targets/dev.yml): a single "---" block,
		// no second one for a spurious stdin-sourced target.
		So(stdout, ShouldEqual, `---
application: my-app
meta:
  environment: development
  name: development

`)
	})

	Convey("graft fan --output-dir with explicit file arguments does not create a spurious stdin.yml output file", t, func() {
		restoreStdin := setStdinFromFile(t, "../../assets/vaultinfo/novault.yml")
		defer restoreStdin()

		outDir := t.TempDir()
		os.Args = []string{"graft", "fan", "--output-dir", outDir, "../../assets/fan/dirsource.yml", "../../assets/fan/targets/dev.yml"}
		stdout = ""
		stderr = ""
		main()
		So(stderr, ShouldEqual, "")
		So(rc, ShouldEqual, 0)

		entries, err := os.ReadDir(outDir)
		So(err, ShouldBeNil)
		names := []string{}
		for _, e := range entries {
			names = append(names, e.Name())
		}
		So(names, ShouldResemble, []string{"dev.yml"})
	})

	Convey("graft fan still reads stdin as the target when no target files are given and stdin is piped", t, func() {
		restoreStdin := setStdinFromFile(t, "../../assets/fan/multi-doc-1.yml")
		defer restoreStdin()

		os.Args = []string{"graft", "fan", "-m", "../../assets/fan/multi-doc-source.yml"}
		stdout = ""
		stderr = ""
		main()
		So(stderr, ShouldEqual, "")
		So(rc, ShouldEqual, 0)
		So(stdout, ShouldNotEqual, "")
	})

	Convey("graft fan with a source but no target arguments still reads stdin as the target (F20 regression: F11's guard over-narrowed to len==0, but fan's first positional is the source, not a target)", t, func() {
		restoreStdin := setStdinFromFile(t, "../../assets/fan/targets/dev.yml")
		defer restoreStdin()

		os.Args = []string{"graft", "fan", "../../assets/fan/dirsource.yml"}
		stdout = ""
		stderr = ""
		main()
		So(stderr, ShouldEqual, "")
		So(rc, ShouldEqual, 0)
		So(stdout, ShouldEqual, `---
application: my-app
meta:
  environment: development
  name: development

`)
	})

	Convey("graft fan still honors an explicit '-' target alongside other file arguments", t, func() {
		restoreStdin := setStdinFromFile(t, "../../assets/fan/multi-doc-1.yml")
		defer restoreStdin()

		os.Args = []string{"graft", "fan", "../../assets/fan/source.yml", "-"}
		stdout = ""
		stderr = ""
		main()
		So(stderr, ShouldEqual, "")
		So(rc, ShouldEqual, 0)
		So(stdout, ShouldNotEqual, "")
	})
}

func TestExamples(t *testing.T) {
	var stdout string
	printStdOutf = func(format string, args ...interface{}) {
		stdout = fmt.Sprintf(format, args...)
	}
	var stderr string
	log.PrintStdErrf = func(format string, args ...interface{}) {
		stderr += fmt.Sprintf(format, args...)
	}

	rc := 256 // invalid return code to catch any issues
	exit = func(code int) {
		rc = code
	}

	YAML := func(path string) string {
		s, err := os.ReadFile(path)
		So(err, ShouldBeNil)

		data := make(map[string]interface{})
		err = yaml.Unmarshal(graft.QuoteInjectKeys(s), &data)
		So(err, ShouldBeNil)
		data = graft.NormalizeMap(data)

		var buf bytes.Buffer
		enc := yaml.NewEncoder(&buf, yaml.Indent(2))
		err = enc.Encode(data)
		So(err, ShouldBeNil)
		So(enc.Close(), ShouldBeNil)

		return buf.String() + "\n"
	}

	Convey("Examples from README.md", t, func() {
		example := func(args ...string) {
			expect := args[len(args)-1]
			args = args[:len(args)-1]

			os.Args = []string{"graft", "merge"}
			os.Args = append(os.Args, args...)
			stdout, stderr = "", ""
			main()

			So(stderr, ShouldEqual, "")
			So(stdout, ShouldEqual, YAML(expect))
			So(rc, ShouldEqual, 0)
		}

		Convey("Basic Example", func() {
			example(
				"../../examples/basic/main.yml",
				"../../examples/basic/merge.yml",

				"../../examples/basic/output.yml",
			)
		})

		Convey("Map Replacements", func() {
			example(
				"../../examples/map-replacement/original.yml",
				"../../examples/map-replacement/delete.yml",
				"../../examples/map-replacement/insert.yml",

				"../../examples/map-replacement/output.yml",
			)
		})

		Convey("Key Removal", func() {
			example(
				"--prune", "deleteme",
				"../../examples/key-removal/original.yml",
				"../../examples/key-removal/things.yml",

				"../../examples/key-removal/output.yml",
			)

			example(
				"../../examples/pruning/base.yml",
				"../../examples/pruning/jobs.yml",
				"../../examples/pruning/networks.yml",

				"../../examples/pruning/output.yml",
			)
		})

		Convey("Lists of Maps", func() {
			example(
				"../../examples/list-of-maps/original.yml",
				"../../examples/list-of-maps/new.yml",

				"../../examples/list-of-maps/output.yml",
			)
		})

		Convey("Static IPs", func() {
			example(
				"../../examples/static-ips/jobs.yml",
				"../../examples/static-ips/properties.yml",
				"../../examples/static-ips/networks.yml",

				"../../examples/static-ips/output.yml",
			)
		})

		Convey("Static IPs with availability zones", func() {
			example(
				"../../examples/availability-zones/jobs.yml",
				"../../examples/availability-zones/properties.yml",
				"../../examples/availability-zones/networks.yml",

				"../../examples/availability-zones/output.yml",
			)
		})

		Convey("Injecting Subtrees", func() {
			example(
				"--prune", "meta",
				"../../examples/inject/all-in-one.yml",

				"../../examples/inject/output.yml",
			)

			example(
				"--prune", "meta",
				"../../examples/inject/templates.yml",
				"../../examples/inject/green.yml",

				"../../examples/inject/output.yml",
			)
		})

		Convey("Pruning", func() {
			example(
				"../../examples/pruning/base.yml",
				"../../examples/pruning/jobs.yml",
				"../../examples/pruning/networks.yml",

				"../../examples/pruning/output.yml",
			)
		})

		Convey("Inserting", func() {
			example(
				"../../examples/inserting/main.yml",
				"../../examples/inserting/addon.yml",

				"../../examples/inserting/result.yml",
			)
		})

		Convey("Calc", func() {
			example(
				"--prune", "meta",
				"../../examples/calc/meta.yml",
				"../../examples/calc/jobs.yml",

				"../../examples/calc/result.yml",
			)
		})
	})
}
