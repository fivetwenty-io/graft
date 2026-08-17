package main

import (
	"os"
	"path/filepath"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

// Helper functions for safe type assertions in tests.
func asMap(v interface{}) map[string]interface{} {
	m, ok := v.(map[string]interface{})
	if !ok {
		return nil
	}
	return m
}

func asList(v interface{}) []interface{} {
	l, ok := v.([]interface{})
	if !ok {
		return nil
	}
	return l
}

func asString(v interface{}) string {
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return s
}

func asFile(v interface{}) *os.File {
	f, ok := v.(*os.File)
	if !ok {
		return nil
	}
	return f
}

func TestSplitExamples(t *testing.T) {
	Convey("Split operator examples should merge correctly", t, func() {
		// Get the project root directory
		wd, err := os.Getwd()
		So(err, ShouldBeNil)

		// Go up to project root if we're in cmd/graft
		if filepath.Base(wd) == "graft" && filepath.Base(filepath.Dir(wd)) == "cmd" {
			wd = filepath.Dir(filepath.Dir(wd))
		}

		Convey("Network parsing example", func() {
			filePath := filepath.Join(wd, "examples", "split", "network-parsing.yml")
			files, err := openFiles([]string{filePath})
			So(err, ShouldBeNil)
			defer func() {
				for _, f := range files {
					if file := asFile(f.Reader); file != nil {
						_ = file.Close()
					}
				}
			}()

			m, _, err := mergeAllDocs(files, &mergeOpts{})
			So(err, ShouldBeNil)

			// Check IP octets
			networkData := asMap(m["network_data"])
			ipOctets := asList(networkData["ip_octets"])
			So(len(ipOctets), ShouldEqual, 4)
			So(ipOctets[0], ShouldEqual, "192")
			So(ipOctets[1], ShouldEqual, "168")
			So(ipOctets[2], ShouldEqual, "1")
			So(ipOctets[3], ShouldEqual, "100")

			// Check MAC parts
			macParts := asList(networkData["mac_parts"])
			So(len(macParts), ShouldEqual, 6)
		})

		Convey("Data extraction example", func() {
			filePath := filepath.Join(wd, "examples", "split", "data-extraction.yml")
			files, err := openFiles([]string{filePath})
			So(err, ShouldBeNil)
			defer func() {
				for _, f := range files {
					if file := asFile(f.Reader); file != nil {
						_ = file.Close()
					}
				}
			}()

			m, _, err := mergeAllDocs(files, &mergeOpts{})
			So(err, ShouldBeNil)

			// Check URL params
			dataExtraction := asMap(m["data_extraction"])
			params := asList(dataExtraction["params"])
			So(len(params), ShouldEqual, 4)
			So(params[0], ShouldEqual, "host=localhost")
			So(params[1], ShouldEqual, "port=8080")

			// Check JSON fields
			jsonFields := asList(dataExtraction["json_fields"])
			So(len(jsonFields), ShouldEqual, 5)
			So(jsonFields[0], ShouldEqual, "")
			So(jsonFields[1], ShouldEqual, "name:john")
		})

		Convey("Version parsing example", func() {
			filePath := filepath.Join(wd, "examples", "split", "version-parsing.yml")
			files, err := openFiles([]string{filePath})
			So(err, ShouldBeNil)
			defer func() {
				for _, f := range files {
					if file := asFile(f.Reader); file != nil {
						_ = file.Close()
					}
				}
			}()

			m, _, err := mergeAllDocs(files, &mergeOpts{})
			So(err, ShouldBeNil)

			// Check base version
			versionParsing := asMap(m["version_parsing"])
			baseVersion := asList(versionParsing["base_version"])
			So(len(baseVersion), ShouldEqual, 3)
			So(baseVersion[0], ShouldEqual, "2.1.3")
			So(baseVersion[1], ShouldEqual, "beta.1")
			So(baseVersion[2], ShouldEqual, "build.456")
		})

		Convey("Comprehensive tests example", func() {
			filePath := filepath.Join(wd, "examples", "split", "comprehensive-tests.yml")
			files, err := openFiles([]string{filePath})
			So(err, ShouldBeNil)
			defer func() {
				for _, f := range files {
					if file := asFile(f.Reader); file != nil {
						_ = file.Close()
					}
				}
			}()

			m, _, err := mergeAllDocs(files, &mergeOpts{})
			So(err, ShouldBeNil)

			// Check basic tests
			basicTests := asMap(m["basic_tests"])
			commaSplit := asList(basicTests["comma_split"])
			So(len(commaSplit), ShouldEqual, 3)
			So(commaSplit[0], ShouldEqual, "apple")
			So(commaSplit[1], ShouldEqual, "banana")
			So(commaSplit[2], ShouldEqual, "cherry")

			// Check edge cases
			edgeCases := asMap(m["edge_cases"])
			unicodeSplit := asList(edgeCases["unicode_split"])
			So(len(unicodeSplit), ShouldEqual, 3)
			So(unicodeSplit[0], ShouldEqual, "Hello")
			So(unicodeSplit[1], ShouldEqual, "World")
			So(unicodeSplit[2], ShouldEqual, "Test")
		})

		Convey("Operator integration example", func() {
			filePath := filepath.Join(wd, "examples", "split", "operator-integration.yml")
			files, err := openFiles([]string{filePath})
			So(err, ShouldBeNil)
			defer func() {
				for _, f := range files {
					if file := asFile(f.Reader); file != nil {
						_ = file.Close()
					}
				}
			}()

			m, _, err := mergeAllDocs(files, &mergeOpts{})
			So(err, ShouldBeNil)

			// Check grab integration
			grabInt := asMap(m["grab_integration"])
			connParams := asList(grabInt["connection_params"])
			So(len(connParams), ShouldEqual, 4)
			So(connParams[0], ShouldEqual, "host=localhost")
			So(connParams[1], ShouldEqual, "port=5432")

			// Check concat integration
			concatInt := asMap(m["concat_integration"])
			splitFruits := asList(concatInt["split_fruits"])
			So(len(splitFruits), ShouldEqual, 3)
			So(splitFruits[0], ShouldEqual, "apple")
		})

		Convey("Regex patterns example", func() {
			filePath := filepath.Join(wd, "examples", "split", "regex-patterns.yml")
			files, err := openFiles([]string{filePath})
			So(err, ShouldBeNil)
			defer func() {
				for _, f := range files {
					if file := asFile(f.Reader); file != nil {
						_ = file.Close()
					}
				}
			}()

			m, _, err := mergeAllDocs(files, &mergeOpts{})
			So(err, ShouldBeNil)

			// Check basic regex
			basicRegex := asMap(m["basic_regex"])
			ipOctets := asList(basicRegex["ip_octets"])
			So(len(ipOctets), ShouldEqual, 4)
			So(ipOctets[0], ShouldEqual, "192")

			// Check PCRE features
			pcreFeatures := asMap(m["pcre_features"])
			lookbehindSplit := asList(pcreFeatures["lookbehind_split"])
			So(len(lookbehindSplit), ShouldEqual, 3)
			So(lookbehindSplit[0], ShouldEqual, "abc123")
			So(lookbehindSplit[1], ShouldEqual, "def456")
			So(lookbehindSplit[2], ShouldEqual, "ghi")
		})

		Convey("Reversibility tests example", func() {
			filePath := filepath.Join(wd, "examples", "split", "reversibility-tests.yml")
			files, err := openFiles([]string{filePath})
			So(err, ShouldBeNil)
			defer func() {
				for _, f := range files {
					if file := asFile(f.Reader); file != nil {
						_ = file.Close()
					}
				}
			}()

			m, _, err := mergeAllDocs(files, &mergeOpts{})
			So(err, ShouldBeNil)

			// Check perfect reversibility
			perfectRev := asMap(m["perfect_reversibility"])

			// Check split and rejoin
			originalCSV := asString(perfectRev["original_csv"])
			rejoinCSV := asString(perfectRev["rejoin_csv"])
			So(rejoinCSV, ShouldEqual, originalCSV)

			// Check empty string cases
			emptyStrings := asMap(m["empty_string_cases"])
			splitEmpties := asList(emptyStrings["split_empties"])
			So(len(splitEmpties), ShouldEqual, 6)
			So(splitEmpties[1], ShouldEqual, "")
			So(splitEmpties[3], ShouldEqual, "")
			So(splitEmpties[4], ShouldEqual, "")
		})
	})
}
