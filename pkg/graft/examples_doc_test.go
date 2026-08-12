package graft_test

// Testable examples backing docs/developer-guide/library-api/*.md. Every
// pattern shown in engine.md, document.md, diff-api.md, and options.md has
// a compiling counterpart here (go test ./pkg/graft/ -run Example), so a
// doc snippet that stops compiling against the real API fails this suite
// instead of shipping silently wrong.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fivetwenty-io/graft/pkg/graft"
	// Registers vault/awsparam/awssecret/nats (and every other built-in
	// operator) via init(), so ExampleWithBackend's "(( vault ... ))" call
	// resolves. Other _test.go files in this test binary already import
	// operators (e.g. errors_test.go), so this is redundant with the
	// running binary today, but this file should not depend on that.
	_ "github.com/fivetwenty-io/graft/pkg/graft/operators"
)

// ExampleNewEngine constructs an engine with functional options and
// reconfigures it at runtime via Configure (a *DefaultEngine method, not
// part of the Engine interface).
func ExampleNewEngine() {
	engine, err := graft.NewEngine(
		graft.WithCacheSize(500),
		graft.WithCacheTTL(1*time.Minute),
		graft.WithTraceLevel(graft.TraceLevelNone),
	)
	if err != nil {
		fmt.Println("error:", err)
		return
	}

	de, ok := engine.(*graft.DefaultEngine)
	if !ok {
		fmt.Println("not a *DefaultEngine")
		return
	}

	if err := de.Configure(graft.WithCacheSize(1000)); err != nil {
		fmt.Println("configure error:", err)
		return
	}

	fmt.Println("engine configured")
	// Output:
	// engine configured
}

// ExampleEngine_ParseFile parses a file from disk, auto-detecting the
// format from its extension.
func ExampleEngine_ParseFile() {
	dir, err := os.MkdirTemp("", "graft-example-parsefile")
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	defer os.RemoveAll(dir)

	path := filepath.Join(dir, "config.yml")
	if err := os.WriteFile(path, []byte("name: myapp\nport: 8080\n"), 0o644); err != nil {
		fmt.Println("error:", err)
		return
	}

	engine, _ := graft.NewEngine()
	doc, err := engine.ParseFile(path)
	if err != nil {
		fmt.Println("error:", err)
		return
	}

	fmt.Println(doc.String("name"))
	fmt.Println(doc.Int("port"))
	// Output:
	// myapp
	// 8080
}

// ExampleEngine_MergeFiles loads and merges two files in one call.
func ExampleEngine_MergeFiles() {
	dir, err := os.MkdirTemp("", "graft-example-mergefiles")
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	defer os.RemoveAll(dir)

	basePath := filepath.Join(dir, "base.yml")
	overlayPath := filepath.Join(dir, "overlay.yml")
	if err := os.WriteFile(basePath, []byte("name: myapp\ndatabase:\n  host: localhost\n  port: 5432\n"), 0o644); err != nil {
		fmt.Println("error:", err)
		return
	}
	if err := os.WriteFile(overlayPath, []byte("database:\n  host: prod.example.com\n"), 0o644); err != nil {
		fmt.Println("error:", err)
		return
	}

	engine, _ := graft.NewEngine()
	result, err := engine.MergeFiles(context.Background(), basePath, overlayPath).Execute()
	if err != nil {
		fmt.Println("error:", err)
		return
	}

	fmt.Println(result.String("name"))
	fmt.Println(result.String("database.host"))
	fmt.Println(result.Int("database.port"))
	// Output:
	// myapp
	// prod.example.com
	// 5432
}

// ExampleMergeBuilder_Base backs merge-builder.md's Base/Overlay/OverlayFile
// section: Base sets position 0 in the builder's document list, Overlay
// appends in-memory documents, and OverlayFile loads and appends a file,
// all composing with the same precedence documents passed directly to
// Engine.Merge would have.
func ExampleMergeBuilder_Base() {
	dir, err := os.MkdirTemp("", "graft-example-overlayfile")
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	defer os.RemoveAll(dir)

	overridesPath := filepath.Join(dir, "overrides.yml")
	if err := os.WriteFile(overridesPath, []byte("database:\n  port: 5433\n"), 0o644); err != nil {
		fmt.Println("error:", err)
		return
	}

	engine, _ := graft.NewEngine()
	defaults, _ := engine.ParseYAML([]byte("name: myapp\ndatabase:\n  host: localhost\n  port: 5432\n"))
	production, _ := engine.ParseYAML([]byte("database:\n  host: prod.example.com\n"))

	// Merge(ctx) with no documents, then Base/Overlay/OverlayFile build up
	// the document list: base first, overlays after, in call order.
	result, err := engine.Merge(context.Background()).
		Base(defaults).
		Overlay(production).
		OverlayFile(overridesPath).
		Execute()
	if err != nil {
		fmt.Println("error:", err)
		return
	}

	fmt.Println(result.String("name"))
	fmt.Println(result.String("database.host"))
	fmt.Println(result.Int("database.port"))
	// Output:
	// myapp
	// prod.example.com
	// 5433
}

// ExampleMergeBuilder_OverlayFile_missingFile shows the documented error
// shape for a load failure: OverlayFile never panics or returns a nil
// builder, and the error surfaces from Execute().
func ExampleMergeBuilder_OverlayFile_missingFile() {
	engine, _ := graft.NewEngine()
	base, _ := engine.ParseYAML([]byte("name: myapp\n"))

	_, err := engine.Merge(context.Background(), base).
		OverlayFile("missing.yml").
		Execute()

	fmt.Println(err != nil)
	fmt.Println(strings.Contains(err.Error(), "failed to load overlay file"))
	// Output:
	// true
	// true
}

// ExampleDocument shows checked getters (zero value on any failure) next
// to the type-safe getters, whose errors are comparable with errors.Is
// against graft's sentinel errors.
func ExampleDocument() {
	engine, _ := graft.NewEngine()
	doc, err := engine.ParseYAML([]byte("name: myapp\nport: 8080\n"))
	if err != nil {
		fmt.Println("error:", err)
		return
	}

	// Checked getters: zero value, no error, on a missing path or a type
	// mismatch.
	fmt.Println(doc.String("name"))
	fmt.Println(doc.String("missing"))
	fmt.Println(doc.Int("port"))
	fmt.Println(doc.Has("port"), doc.Has("missing"))

	// Type-safe getters: an error identifiable via errors.Is, independent
	// of the error's printed message.
	_, err = doc.GetString("missing")
	fmt.Println(errors.Is(err, graft.ErrNotFound))

	_, err = doc.GetString("port")
	fmt.Println(errors.Is(err, graft.ErrTypeMismatch))
	// Output:
	// myapp
	//
	// 8080
	// true false
	// true
	// true
}

// ExampleDiffDocuments compares two documents and renders the result.
func ExampleDiffDocuments() {
	engine, _ := graft.NewEngine()
	before, _ := engine.ParseYAML([]byte("database:\n  host: localhost\n  port: 5432\n"))
	after, _ := engine.ParseYAML([]byte("database:\n  host: db.example.com\n  port: 5432\n"))

	result, err := graft.DiffDocuments(before, after, nil)
	if err != nil {
		fmt.Println("error:", err)
		return
	}

	fmt.Println(result.HasChanges())
	for _, change := range result.Changes() {
		fmt.Printf("%s %s: %v -> %v\n", change.Type, change.Path, change.OldValue, change.NewValue)
	}

	var buf bytes.Buffer
	renderErr := result.WriteChangeList(&buf, &graft.DiffOptions{OmitHeader: true})
	fmt.Println(renderErr == nil, buf.Len() > 0)
	// Output:
	// true
	// modified database.host: localhost -> db.example.com
	// true true
}

// ExampleMergeBuilder_TrackHistory activates document-memory tracking for
// one merge chain and reads the result back via Document.History(). A
// merge-phase overwrite of an existing key records exactly one entry -
// see history-api.md's "What Is Actually Recorded" for the full list of
// what does and does not get tracked.
func ExampleMergeBuilder_TrackHistory() {
	engine, _ := graft.NewEngine()
	base, _ := engine.ParseYAML([]byte("host: localhost\n"))
	overlay, _ := engine.ParseYAML([]byte("host: production.com\n"))

	result, err := engine.Merge(context.Background(), base, overlay).
		TrackHistory().
		Execute()
	if err != nil {
		fmt.Println("error:", err)
		return
	}

	entries := result.History().ForPath("host")
	fmt.Println(len(entries))
	fmt.Println(entries[0].Phase, entries[0].NewValue)
	// Output:
	// 1
	// MERGE production.com
}

// ExampleDocument_History shows that Document.History() never returns a
// nil interface: a Document produced without tracking active returns an
// empty, valid History whose methods return empty results.
func ExampleDocument_History() {
	engine, _ := graft.NewEngine()
	base, _ := engine.ParseYAML([]byte("host: localhost\n"))
	overlay, _ := engine.ParseYAML([]byte("host: production.com\n"))

	// No TrackHistory() call and no engine-level history option: tracking
	// stays off.
	result, err := engine.Merge(context.Background(), base, overlay).Execute()
	if err != nil {
		fmt.Println("error:", err)
		return
	}

	history := result.History()
	fmt.Println(history != nil)
	fmt.Println(len(history.Timeline()))
	// Output:
	// true
	// 0
}

// exampleMapBackend is a minimal graft.Backend backed by an in-memory map,
// used only by ExampleWithBackend and ExampleDefaultEngine_RegisterBackend
// below. See docs/developer-guide/custom-backends.md for a fuller
// implementation sketch (retry, caching, a real upstream client).
type exampleMapBackend struct {
	name string
	data map[string]string
}

func (b *exampleMapBackend) Name() string { return b.name }

func (b *exampleMapBackend) Get(_ context.Context, path string) (interface{}, error) {
	v, ok := b.data[path]
	if !ok {
		return nil, graft.ErrBackendNotFound
	}
	return v, nil
}

// GetBatch delegates to Get per path via graft.SequentialGetBatch - see
// its doc comment for why: no graft operator calls GetBatch today, so
// there is nothing to design real batching against.
func (b *exampleMapBackend) GetBatch(ctx context.Context, paths []string) (map[string]interface{}, error) {
	return graft.SequentialGetBatch(ctx, paths, b.Get)
}

func (b *exampleMapBackend) Health(context.Context) error { return nil }
func (b *exampleMapBackend) Close() error                 { return nil }

// ExampleWithBackend registers a custom backend under the name "vault" and
// enables features.FeatureBackendRegistry via WithBackendRegistry (the
// only way to enable it from outside this module - WithFeatureFlags takes
// an internal/features type), so a "(( vault ... ))" call resolves through
// the custom backend instead of a real Vault instance.
func ExampleWithBackend() {
	backend := &exampleMapBackend{
		name: "vault",
		data: map[string]string{"secret/db:password": "s3cr3t"},
	}

	engine, err := graft.NewEngine(
		graft.WithBackend(backend),
		graft.WithBackendRegistry(true),
	)
	if err != nil {
		fmt.Println("error:", err)
		return
	}

	doc, err := engine.ParseYAML([]byte("password: (( vault \"secret/db:password\" ))\n"))
	if err != nil {
		fmt.Println("error:", err)
		return
	}

	result, err := engine.Evaluate(context.Background(), doc)
	if err != nil {
		fmt.Println("error:", err)
		return
	}

	password, _ := result.GetString("password")
	fmt.Println(password)
	// Output:
	// s3cr3t
}

// ExampleDefaultEngine_RegisterBackend registers a backend at runtime
// (rather than via WithBackend at construction) and looks it up again with
// GetBackend/ListBackends.
func ExampleDefaultEngine_RegisterBackend() {
	engine, err := graft.NewEngine(graft.WithBackendRegistry(true))
	if err != nil {
		fmt.Println("error:", err)
		return
	}

	backend := &exampleMapBackend{name: "redis", data: map[string]string{"k": "v"}}
	if err := engine.RegisterBackend(backend); err != nil {
		fmt.Println("error:", err)
		return
	}

	fmt.Println(engine.ListBackends())

	got, ok := engine.GetBackend("redis")
	fmt.Println(ok, got.Name())
	// Output:
	// [redis]
	// true redis
}
