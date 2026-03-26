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

func TestNatsOperatorSkipMode(t *testing.T) {
	Convey("NATS Operator Skip Mode", t, func() {
		Convey("when SkipNats is true via engine option", func() {
			Convey("nats should return REDACTED", func() {
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
				// When SkipNats is true, returns REDACTED
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
