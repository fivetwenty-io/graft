package graft

import (
	"testing"

	"github.com/cppforlife/go-patch/patch"
	. "github.com/smartystreets/goconvey/convey"
)

func TestNewGoPatchDocument(t *testing.T) {
	Convey("NewGoPatchDocument", t, func() {
		Convey("should create a go-patch document with given ops", func() {
			ops := patch.Ops{
				patch.ReplaceOp{
					Path:  patch.MustNewPointerFromString("/test"),
					Value: "value",
				},
			}

			doc := NewGoPatchDocument(ops)

			So(doc, ShouldNotBeNil)
			So(IsGoPatchDocument(doc), ShouldBeTrue)

			extractedOps, ok := GetGoPatchOps(doc)
			So(ok, ShouldBeTrue)
			So(extractedOps, ShouldResemble, ops)
		})

		Convey("should create a go-patch document with empty ops", func() {
			ops := patch.Ops{}
			doc := NewGoPatchDocument(ops)

			So(doc, ShouldNotBeNil)
			So(IsGoPatchDocument(doc), ShouldBeTrue)
		})
	})
}

func TestIsGoPatchDocument(t *testing.T) {
	Convey("IsGoPatchDocument", t, func() {
		Convey("should return true for go-patch documents", func() {
			doc := NewGoPatchDocument(patch.Ops{})
			So(IsGoPatchDocument(doc), ShouldBeTrue)
		})

		Convey("should return false for regular documents", func() {
			doc := NewDocument(map[string]interface{}{
				"key": "value",
			})
			So(IsGoPatchDocument(doc), ShouldBeFalse)
		})
	})
}

func TestGetGoPatchOps(t *testing.T) {
	Convey("GetGoPatchOps", t, func() {
		Convey("should return ops for go-patch documents", func() {
			ops := patch.Ops{
				patch.ReplaceOp{
					Path:  patch.MustNewPointerFromString("/test"),
					Value: "value",
				},
			}
			doc := NewGoPatchDocument(ops)

			extractedOps, ok := GetGoPatchOps(doc)
			So(ok, ShouldBeTrue)
			So(extractedOps, ShouldResemble, ops)
		})

		Convey("should return false for regular documents", func() {
			doc := NewDocument(map[string]interface{}{
				"key": "value",
			})

			extractedOps, ok := GetGoPatchOps(doc)
			So(ok, ShouldBeFalse)
			So(extractedOps, ShouldBeNil)
		})
	})
}

func TestGoPatchDocument_UnsupportedOperations(t *testing.T) {
	Convey("Go-patch document unsupported operations", t, func() {
		doc := NewGoPatchDocument(patch.Ops{})

		Convey("Get should return error", func() {
			result, err := doc.Get("path")
			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "do not support Get operations")
			So(result, ShouldBeNil)
		})

		Convey("GetString should return error", func() {
			result, err := doc.GetString("path")
			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "do not support GetString operations")
			So(result, ShouldEqual, "")
		})

		Convey("GetInt should return error", func() {
			result, err := doc.GetInt("path")
			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "do not support GetInt operations")
			So(result, ShouldEqual, 0)
		})

		Convey("GetBool should return error", func() {
			result, err := doc.GetBool("path")
			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "do not support GetBool operations")
			So(result, ShouldBeFalse)
		})

		Convey("GetSlice should return error", func() {
			result, err := doc.GetSlice("path")
			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "do not support GetSlice operations")
			So(result, ShouldBeNil)
		})

		Convey("GetMap should return error", func() {
			result, err := doc.GetMap("path")
			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "do not support GetMap operations")
			So(result, ShouldBeNil)
		})

		Convey("Set should return error", func() {
			err := doc.Set("path", "value")
			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "do not support Set operations")
		})

		Convey("Delete should return error", func() {
			err := doc.Delete("path")
			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "do not support Delete operations")
		})

		Convey("ToYAML should return error", func() {
			result, err := doc.ToYAML()
			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "do not support ToYAML operations")
			So(result, ShouldBeNil)
		})

		Convey("ToJSON should return error", func() {
			result, err := doc.ToJSON()
			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "do not support ToJSON operations")
			So(result, ShouldBeNil)
		})

		Convey("GetInt64 should return error", func() {
			result, err := doc.GetInt64("path")
			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "do not support GetInt64 operations")
			So(result, ShouldEqual, int64(0))
		})

		Convey("GetFloat64 should return error", func() {
			result, err := doc.GetFloat64("path")
			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "do not support GetFloat64 operations")
			So(result, ShouldEqual, 0.0)
		})

		Convey("GetStringSlice should return error", func() {
			result, err := doc.GetStringSlice("path")
			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "do not support GetStringSlice operations")
			So(result, ShouldBeNil)
		})

		Convey("GetMapStringString should return error", func() {
			result, err := doc.GetMapStringString("path")
			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "do not support GetMapStringString operations")
			So(result, ShouldBeNil)
		})
	})
}

func TestGoPatchDocument_SupportedOperations(t *testing.T) {
	Convey("Go-patch document supported operations", t, func() {
		ops := patch.Ops{
			patch.ReplaceOp{
				Path:  patch.MustNewPointerFromString("/test"),
				Value: "value",
			},
		}
		doc := NewGoPatchDocument(ops)

		Convey("Keys should return empty slice", func() {
			keys := doc.Keys()
			So(keys, ShouldNotBeNil)
			So(len(keys), ShouldEqual, 0)
		})

		Convey("RawData should return the ops", func() {
			rawData := doc.RawData()
			So(rawData, ShouldEqual, ops)
		})

		Convey("GetData should return the ops", func() {
			data := doc.GetData()
			So(data, ShouldEqual, ops)
		})

		Convey("Prune should return the same document", func() {
			result := doc.Prune("key")
			So(result, ShouldEqual, doc)
		})

		Convey("CherryPick should return the same document", func() {
			result := doc.CherryPick("key1", "key2")
			So(result, ShouldEqual, doc)
		})

		Convey("Clone should create a copy with same ops", func() {
			cloned := doc.Clone()
			So(cloned, ShouldNotBeNil)
			So(IsGoPatchDocument(cloned), ShouldBeTrue)

			clonedOps, ok := GetGoPatchOps(cloned)
			So(ok, ShouldBeTrue)
			So(clonedOps, ShouldResemble, ops)

			// Verify it's a different document instance, even if ops are shared
			So(cloned != doc, ShouldBeTrue)
		})
	})
}

// TestGoPatchDocument_C4PromotedMethods exercises the C4 convenience
// methods (String/Int/Int64/Float64/Bool/Has/Paths/SortKeys/ToJSONIndent),
// which goPatchDocument gets by embedding document rather than
// reimplementing. A go-patch document's embedded document is always at its
// zero value (data is nil, since nothing ever sets it), so every one of
// these returns the same well-defined "empty document" result regardless
// of the go-patch ops it holds; that is the intended behavior, not an
// oversight (see the doc comment on goPatchDocument).
func TestGoPatchDocument_C4PromotedMethods(t *testing.T) {
	Convey("Go-patch document C4 promoted methods", t, func() {
		doc := NewGoPatchDocument(patch.Ops{
			patch.ReplaceOp{
				Path:  patch.MustNewPointerFromString("/test"),
				Value: "value",
			},
		})

		Convey("checked getters return their zero value", func() {
			So(doc.String("anything"), ShouldEqual, "")
			So(doc.Int("anything"), ShouldEqual, 0)
			So(doc.Int64("anything"), ShouldEqual, int64(0))
			So(doc.Float64("anything"), ShouldEqual, 0.0)
			So(doc.Bool("anything"), ShouldBeFalse)
		})

		Convey("Has returns false", func() {
			So(doc.Has("anything"), ShouldBeFalse)
		})

		Convey("Paths returns empty", func() {
			So(len(doc.Paths()), ShouldEqual, 0)
		})

		Convey("SortKeys returns a usable empty Document", func() {
			sorted := doc.SortKeys()
			So(sorted, ShouldNotBeNil)
			So(sorted.Has("anything"), ShouldBeFalse)
			So(len(sorted.Paths()), ShouldEqual, 0)
		})

		Convey("ToJSONIndent returns an empty JSON object", func() {
			out, err := doc.ToJSONIndent("  ")
			So(err, ShouldBeNil)
			So(string(out), ShouldEqual, "{}")
		})

		Convey("Prune accepts variadic keys as a no-op", func() {
			result := doc.Prune("key1", "key2")
			So(result, ShouldEqual, doc)
		})

		Convey("existing unsupported-operation behavior is unaffected by embedding", func() {
			_, err := doc.Get("path")
			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "do not support Get operations")
		})
	})
}
