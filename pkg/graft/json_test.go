package graft

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestJSONifyFilesMultiDoc(t *testing.T) {
	Convey("JSONifyFiles on multi-doc YAML", t, func() {
		Convey("emits exactly one compact JSON string per document", func() {
			data := []byte("name: first\nvalue: 1\n---\nname: second\nvalue: 2\n---\nname: third\nlist:\n- a\n- b\n")
			lines, err := jsonifyDataMultiDoc(data, false)
			So(err, ShouldBeNil)
			So(len(lines), ShouldEqual, 3)

			for _, line := range lines {
				So(strings.Contains(line, "\n"), ShouldBeFalse)
				var v map[string]interface{}
				decErr := json.Unmarshal([]byte(line), &v)
				So(decErr, ShouldBeNil)
			}

			So(lines[0], ShouldEqual, `{"name":"first","value":1}`)
			So(lines[1], ShouldEqual, `{"name":"second","value":2}`)
			So(lines[2], ShouldEqual, `{"list":["a","b"],"name":"third"}`)
		})

		Convey("a single-document input still yields exactly one JSON line", func() {
			data := []byte("name: only\n")
			lines, err := jsonifyDataMultiDoc(data, false)
			So(err, ShouldBeNil)
			So(len(lines), ShouldEqual, 1)
			So(lines[0], ShouldEqual, `{"name":"only"}`)
		})

		Convey("a trailing dangling document separator produces an empty document, which errors like spruce (no silent {} line)", func() {
			data := []byte("name: first\n---\n")
			_, err := jsonifyDataMultiDoc(data, false)
			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "Root of YAML document is not a hash/map")
		})

		Convey("a whitespace-only document errors instead of silently producing {}", func() {
			_, err := jsonifyData([]byte("   \n\n"), false)
			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "Root of YAML document is not a hash/map")
		})

		Convey("a null document errors instead of silently producing {}", func() {
			_, err := jsonifyData([]byte("null\n"), false)
			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "Root of YAML document is not a hash/map")
		})

		Convey("a scalar-root document errors", func() {
			_, err := jsonifyData([]byte("just a string\n"), false)
			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "Root of YAML document is not a hash/map")
		})

		Convey("an array-root document errors", func() {
			_, err := jsonifyData([]byte("- 1\n- 2\n"), false)
			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "Root of YAML document is not a hash/map")
		})

		Convey("a valid empty map document succeeds", func() {
			out, err := jsonifyData([]byte("{}\n"), false)
			So(err, ShouldBeNil)
			So(out, ShouldEqual, "{}")
		})

		Convey("an unquoted YAML 1.1 boolean-lookalike coerces to a JSON boolean, matching spruce json", func() {
			out, err := jsonifyData([]byte("a: yes\nb: On\nc: NO\nd: off\n"), false)
			So(err, ShouldBeNil)
			So(out, ShouldEqual, `{"a":true,"b":true,"c":false,"d":false}`)
		})

		Convey("a quoted YAML 1.1 boolean-lookalike stays a JSON string, matching spruce json", func() {
			out, err := jsonifyData([]byte("a: \"yes\"\nb: 'On'\nc: \"NO\"\nd: 'off'\n"), false)
			So(err, ShouldBeNil)
			So(out, ShouldEqual, `{"a":"yes","b":"On","c":"NO","d":"off"}`)
		})

		Convey("quoted and unquoted forms are distinguished within the same document", func() {
			out, err := jsonifyData([]byte("quoted: \"yes\"\nbare: yes\n"), false)
			So(err, ShouldBeNil)
			So(out, ShouldEqual, `{"bare":true,"quoted":"yes"}`)
		})

		Convey("bool-lookalike coercion applies per-document across a multi-doc input", func() {
			data := []byte("flag: yes\n---\nflag: \"yes\"\n")
			lines, err := jsonifyDataMultiDoc(data, false)
			So(err, ShouldBeNil)
			So(len(lines), ShouldEqual, 2)
			So(lines[0], ShouldEqual, `{"flag":true}`)
			So(lines[1], ShouldEqual, `{"flag":"yes"}`)
		})
	})
}

func TestYAMLifyFiles(t *testing.T) {
	Convey("YAMLifyFiles converts JSON input to YAML", t, func() {
		dir := t.TempDir()

		Convey("a single compact JSON object becomes one YAML document", func() {
			path := writeTempFile(t, dir, "single.json", `{"database":{"host":"localhost","port":5432}}`)
			docs, err := YAMLifyFiles([]string{path})
			So(err, ShouldBeNil)
			So(len(docs), ShouldEqual, 1)
			So(docs[0], ShouldEqual, "database:\n  host: localhost\n  port: 5432")
		})

		Convey("pretty-printed multi-line JSON parses as a single document", func() {
			path := writeTempFile(t, dir, "pretty.json", "{\n  \"a\": 1,\n  \"b\": [1, 2, 3]\n}\n")
			docs, err := YAMLifyFiles([]string{path})
			So(err, ShouldBeNil)
			So(len(docs), ShouldEqual, 1)
			So(docs[0], ShouldEqual, "a: 1\nb:\n- 1\n- 2\n- 3")
		})

		Convey("one-JSON-object-per-line input (JSONifyFiles' own output shape) round-trips to one YAML doc per line", func() {
			path := writeTempFile(t, dir, "jsonl.json", "{\"doc\":1}\n{\"doc\":2}\n")
			docs, err := YAMLifyFiles([]string{path})
			So(err, ShouldBeNil)
			So(len(docs), ShouldEqual, 2)
			So(docs[0], ShouldEqual, "doc: 1")
			So(docs[1], ShouldEqual, "doc: 2")
		})

		Convey("multiple files are each converted, in argument order", func() {
			path1 := writeTempFile(t, dir, "one.json", `{"a":1}`)
			path2 := writeTempFile(t, dir, "two.json", `{"b":2}`)
			docs, err := YAMLifyFiles([]string{path1, path2})
			So(err, ShouldBeNil)
			So(docs, ShouldResemble, []string{"a: 1", "b: 2"})
		})

		Convey("a JSON array root converts to a YAML sequence document", func() {
			path := writeTempFile(t, dir, "array.json", `[1,2,3]`)
			docs, err := YAMLifyFiles([]string{path})
			So(err, ShouldBeNil)
			So(len(docs), ShouldEqual, 1)
			So(docs[0], ShouldEqual, "- 1\n- 2\n- 3")
		})

		Convey("invalid JSON errors with the source file named", func() {
			path := writeTempFile(t, dir, "bad.json", `{"a": }`)
			_, err := YAMLifyFiles([]string{path})
			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, path)
			So(err.Error(), ShouldContainSubstring, "Error parsing JSON")
		})

		Convey("empty input errors instead of silently producing nothing", func() {
			path := writeTempFile(t, dir, "empty.json", "")
			_, err := YAMLifyFiles([]string{path})
			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "no JSON documents found")
		})

		Convey("a missing file errors with the path named", func() {
			_, err := YAMLifyFiles([]string{dir + "/does-not-exist.json"})
			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "does-not-exist.json")
		})
	})
}

func TestCombineJSONLines(t *testing.T) {
	Convey("CombineJSONLines wraps individual JSON documents into a single JSON array", t, func() {
		Convey("multiple compact JSON objects become a pretty-printed array", func() {
			out, err := CombineJSONLines([]string{`{"a":1}`, `{"b":2}`})
			So(err, ShouldBeNil)
			So(out, ShouldEqual, "[\n  {\n    \"a\": 1\n  },\n  {\n    \"b\": 2\n  }\n]")
		})

		Convey("a single document still becomes a one-element array", func() {
			out, err := CombineJSONLines([]string{`{"a":1}`})
			So(err, ShouldBeNil)
			So(out, ShouldEqual, "[\n  {\n    \"a\": 1\n  }\n]")
		})

		Convey("zero documents becomes an empty array", func() {
			out, err := CombineJSONLines(nil)
			So(err, ShouldBeNil)
			So(out, ShouldEqual, "[]")
		})
	})
}

func writeTempFile(t *testing.T, dir, name, contents string) string {
	t.Helper()
	path := dir + "/" + name
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("writing temp file %s: %v", path, err)
	}
	return path
}

// jsonifyDataMultiDoc splits data the same way JSONifyFiles does and
// jsonifies each document, returning one compact JSON string per document
// (or the first error encountered). Extracted here so multi-doc splitting
// behavior can be tested without going through the file/stdin plumbing in
// JSONifyFiles.
func jsonifyDataMultiDoc(data []byte, strict bool) ([]string, error) {
	l := []string{}
	for _, doc := range splitYAMLDocs(data) {
		jsonData, err := jsonifyData(doc, strict)
		if err != nil {
			return nil, err
		}
		l = append(l, jsonData)
	}
	return l, nil
}
