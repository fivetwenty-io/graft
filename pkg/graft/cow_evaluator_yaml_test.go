package graft

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestCOWTreeFactory_CreateFromYAML_SimpleMap(t *testing.T) {
	Convey("CreateFromYAML with simple map", t, func() {
		factory := NewCOWTreeFactory()
		tree, err := factory.CreateFromYAML([]byte("name: test\nversion: 1\n"))
		So(err, ShouldBeNil)
		So(tree, ShouldNotBeNil)

		name, err := tree.Get("name")
		So(err, ShouldBeNil)
		So(name, ShouldEqual, "test")

		version, err := tree.Get("version")
		So(err, ShouldBeNil)
		So(version, ShouldEqual, 1)
	})
}

func TestCOWTreeFactory_CreateFromYAML_NestedMap(t *testing.T) {
	Convey("CreateFromYAML with nested map", t, func() {
		factory := NewCOWTreeFactory()
		yaml := "meta:\n  name: graft\n  version: 2\n"
		tree, err := factory.CreateFromYAML([]byte(yaml))
		So(err, ShouldBeNil)
		So(tree, ShouldNotBeNil)

		meta, err := tree.Get("meta")
		So(err, ShouldBeNil)
		So(meta, ShouldNotBeNil)

		metaMap, ok := meta.(map[string]interface{})
		So(ok, ShouldBeTrue)
		So(metaMap["name"], ShouldEqual, "graft")
		So(metaMap["version"], ShouldEqual, 2)
	})
}

func TestCOWTreeFactory_CreateFromYAML_EmptyInput(t *testing.T) {
	Convey("CreateFromYAML with empty input returns empty tree", t, func() {
		factory := NewCOWTreeFactory()
		tree, err := factory.CreateFromYAML([]byte{})
		So(err, ShouldBeNil)
		So(tree, ShouldNotBeNil)
	})
}

func TestCOWTreeFactory_CreateFromYAML_NilInput(t *testing.T) {
	Convey("CreateFromYAML with nil input returns empty tree", t, func() {
		factory := NewCOWTreeFactory()
		tree, err := factory.CreateFromYAML(nil)
		So(err, ShouldBeNil)
		So(tree, ShouldNotBeNil)
	})
}

func TestCOWTreeFactory_CreateFromYAML_InvalidYAML(t *testing.T) {
	Convey("CreateFromYAML with invalid YAML returns error", t, func() {
		factory := NewCOWTreeFactory()
		tree, err := factory.CreateFromYAML([]byte("{{invalid"))
		So(err, ShouldNotBeNil)
		So(tree, ShouldBeNil)
	})
}

func TestCOWTreeFactory_CreateFromYAML_NonMapRoot(t *testing.T) {
	Convey("CreateFromYAML with non-map root returns error", t, func() {
		factory := NewCOWTreeFactory()
		tree, err := factory.CreateFromYAML([]byte("- item1\n- item2\n"))
		So(err, ShouldNotBeNil)
		So(tree, ShouldBeNil)
	})
}

func TestCOWTreeFactory_CreateFromYAML_WithArrayValues(t *testing.T) {
	Convey("CreateFromYAML with array values in map", t, func() {
		factory := NewCOWTreeFactory()
		yaml := "items:\n  - one\n  - two\n  - three\n"
		tree, err := factory.CreateFromYAML([]byte(yaml))
		So(err, ShouldBeNil)
		So(tree, ShouldNotBeNil)

		items, err := tree.Get("items")
		So(err, ShouldBeNil)
		So(items, ShouldNotBeNil)

		slice, ok := items.([]interface{})
		So(ok, ShouldBeTrue)
		So(len(slice), ShouldEqual, 3)
		So(slice[0], ShouldEqual, "one")
		So(slice[1], ShouldEqual, "two")
		So(slice[2], ShouldEqual, "three")
	})
}
