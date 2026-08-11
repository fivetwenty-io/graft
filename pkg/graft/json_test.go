package graft

import (
	"encoding/json"
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
