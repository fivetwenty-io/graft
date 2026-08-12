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
	"time"

	"github.com/fivetwenty-io/graft/pkg/graft"
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
