package graft

import (
	"context"
	"os"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

// withArrayMergeKeyEnv sets DEFAULT_ARRAY_MERGE_KEY for the duration of fn and
// restores the prior value (or absence) afterward.
func withArrayMergeKeyEnv(t *testing.T, key string, fn func()) {
	t.Helper()
	original, wasSet := os.LookupEnv("DEFAULT_ARRAY_MERGE_KEY")
	if err := os.Setenv("DEFAULT_ARRAY_MERGE_KEY", key); err != nil {
		t.Fatalf("failed to set DEFAULT_ARRAY_MERGE_KEY: %v", err)
	}
	defer func() {
		if wasSet {
			_ = os.Setenv("DEFAULT_ARRAY_MERGE_KEY", original)
		} else {
			_ = os.Unsetenv("DEFAULT_ARRAY_MERGE_KEY")
		}
	}()
	fn()
}

// TestMergeBuilderKeyMergesArrayOfMapsUsingConfiguredKey is the end-to-end
// case for DEFAULT_ARRAY_MERGE_KEY: merging two documents whose array-of-maps
// entries are identified by a custom field (not "name") must key-merge on
// that field once the environment variable is set, matching spruce's
// mergeArrayByKey semantics (matched entries merge in place, unmatched new
// entries append).
func TestMergeBuilderKeyMergesArrayOfMapsUsingConfiguredKey(t *testing.T) {
	Convey("DEFAULT_ARRAY_MERGE_KEY makes the CLI merge path key-merge on a custom field", t, func() {
		withArrayMergeKeyEnv(t, "sku", func() {
			base := NewDocument(map[string]interface{}{
				"items": []interface{}{
					map[string]interface{}{"sku": "alpha", "value": 1},
					map[string]interface{}{"sku": "beta", "value": 2},
				},
			})
			overlay := NewDocument(map[string]interface{}{
				"items": []interface{}{
					map[string]interface{}{"sku": "alpha", "value": 100},
					map[string]interface{}{"sku": "gamma", "value": 3},
				},
			})

			engine, err := NewEngine()
			So(err, ShouldBeNil)

			result, err := engine.Merge(context.Background(), base, overlay).Execute()
			So(err, ShouldBeNil)

			data, ok := result.RawData().(map[string]interface{})
			So(ok, ShouldBeTrue)
			items, ok := data["items"].([]interface{})
			So(ok, ShouldBeTrue)

			// Key-merge on "sku": alpha updated in place, beta preserved
			// (unmatched original), gamma appended (unmatched new).
			So(len(items), ShouldEqual, 3)

			byKey := make(map[string]int)
			for _, raw := range items {
				entry, ok := raw.(map[string]interface{})
				So(ok, ShouldBeTrue)
				sku, ok := entry["sku"].(string)
				So(ok, ShouldBeTrue)
				value, ok := entry["value"].(int)
				So(ok, ShouldBeTrue)
				byKey[sku] = value
			}

			So(byKey["alpha"], ShouldEqual, 100)
			So(byKey["beta"], ShouldEqual, 2)
			So(byKey["gamma"], ShouldEqual, 3)
		})
	})
}

// TestMergeBuilderHonorsDefaultArrayMergeKeyForNamedLookup verifies that the
// MergeBuilder's array-entry-by-name lookup (used by --cherry-pick path
// segments such as "items.beta") honors the DEFAULT_ARRAY_MERGE_KEY
// environment variable instead of only recognizing the hardcoded
// "name"/"id"/"key" identifier fields.
func TestMergeBuilderHonorsDefaultArrayMergeKeyForNamedLookup(t *testing.T) {
	Convey("DEFAULT_ARRAY_MERGE_KEY overrides the array-entry identifier key", t, func() {
		withArrayMergeKeyEnv(t, "sku", func() {
			doc := NewDocument(map[string]interface{}{
				"items": []interface{}{
					map[string]interface{}{"sku": "alpha", "value": 1},
					map[string]interface{}{"sku": "beta", "value": 2},
				},
			})

			engine, err := NewEngine()
			So(err, ShouldBeNil)

			Convey("WithCherryPick selects the array entry by the configured key", func() {
				result, err := engine.Merge(context.Background(), doc).
					WithCherryPick("items.beta").
					Execute()

				So(err, ShouldBeNil)
				data, ok := result.RawData().(map[string]interface{})
				So(ok, ShouldBeTrue)
				items, ok := data["items"].([]interface{})
				So(ok, ShouldBeTrue)
				So(len(items), ShouldEqual, 1)
				entry, ok := items[0].(map[string]interface{})
				So(ok, ShouldBeTrue)
				So(entry["sku"], ShouldEqual, "beta")
				So(entry["value"], ShouldEqual, 2)
			})
		})
	})
}
