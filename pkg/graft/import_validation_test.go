//go:build integration

package graft_test

import (
	"context"
	"testing"

	"github.com/fivetwenty-io/graft/pkg/graft"
)

// TestExternalImport validates that the graft package can be imported
// and used correctly as an external dependency. This test simulates
// how external users would consume the library.
func TestExternalImport(t *testing.T) {
	t.Run("NewEngine creates valid engine", func(t *testing.T) {
		engine, err := graft.NewEngine()
		if err != nil {
			t.Fatalf("NewEngine() failed: %v", err)
		}
		if engine == nil {
			t.Fatal("NewEngine() returned nil engine")
		}
	})

	t.Run("Engine parses YAML correctly", func(t *testing.T) {
		engine, err := graft.NewEngine()
		if err != nil {
			t.Fatalf("NewEngine() failed: %v", err)
		}

		yamlContent := []byte(`
app:
  name: test-app
  version: 1.0.0
  enabled: true
  port: 8080
`)

		doc, err := engine.ParseYAML(yamlContent)
		if err != nil {
			t.Fatalf("ParseYAML() failed: %v", err)
		}

		name, err := doc.GetString("app.name")
		if err != nil {
			t.Fatalf("GetString() failed: %v", err)
		}
		if name != "test-app" {
			t.Errorf("expected 'test-app', got %q", name)
		}

		port, err := doc.GetInt("app.port")
		if err != nil {
			t.Fatalf("GetInt() failed: %v", err)
		}
		if port != 8080 {
			t.Errorf("expected 8080, got %d", port)
		}

		enabled, err := doc.GetBool("app.enabled")
		if err != nil {
			t.Fatalf("GetBool() failed: %v", err)
		}
		if !enabled {
			t.Error("expected enabled to be true")
		}
	})

	t.Run("Engine merges documents correctly", func(t *testing.T) {
		engine, err := graft.NewEngine()
		if err != nil {
			t.Fatalf("NewEngine() failed: %v", err)
		}

		base := []byte(`
config:
  port: 8080
  debug: false
`)

		override := []byte(`
config:
  debug: true
  host: localhost
`)

		baseDoc, err := engine.ParseYAML(base)
		if err != nil {
			t.Fatalf("ParseYAML(base) failed: %v", err)
		}

		overrideDoc, err := engine.ParseYAML(override)
		if err != nil {
			t.Fatalf("ParseYAML(override) failed: %v", err)
		}

		ctx := context.Background()
		result, err := engine.Merge(ctx, baseDoc, overrideDoc).Execute()
		if err != nil {
			t.Fatalf("Merge().Execute() failed: %v", err)
		}

		port, err := result.GetInt("config.port")
		if err != nil {
			t.Fatalf("GetInt(port) failed: %v", err)
		}
		if port != 8080 {
			t.Errorf("expected port 8080, got %d", port)
		}

		debug, err := result.GetBool("config.debug")
		if err != nil {
			t.Fatalf("GetBool(debug) failed: %v", err)
		}
		if !debug {
			t.Error("expected debug to be true (from override)")
		}

		host, err := result.GetString("config.host")
		if err != nil {
			t.Fatalf("GetString(host) failed: %v", err)
		}
		if host != "localhost" {
			t.Errorf("expected host 'localhost', got %q", host)
		}
	})

	t.Run("Engine evaluates operators correctly", func(t *testing.T) {
		engine, err := graft.NewEngine()
		if err != nil {
			t.Fatalf("NewEngine() failed: %v", err)
		}

		yamlContent := []byte(`
meta:
  app: myapp
  env: prod

config:
  name: (( concat meta.app "-" meta.env ))
`)

		doc, err := engine.ParseYAML(yamlContent)
		if err != nil {
			t.Fatalf("ParseYAML() failed: %v", err)
		}

		ctx := context.Background()
		result, err := engine.Evaluate(ctx, doc)
		if err != nil {
			t.Fatalf("Evaluate() failed: %v", err)
		}

		name, err := result.GetString("config.name")
		if err != nil {
			t.Fatalf("GetString() failed: %v", err)
		}
		if name != "myapp-prod" {
			t.Errorf("expected 'myapp-prod', got %q", name)
		}
	})

	t.Run("Document interface methods work", func(t *testing.T) {
		engine, err := graft.NewEngine()
		if err != nil {
			t.Fatalf("NewEngine() failed: %v", err)
		}

		doc, err := engine.ParseYAML([]byte(`key: value`))
		if err != nil {
			t.Fatalf("ParseYAML() failed: %v", err)
		}

		// Test Keys()
		keys := doc.Keys()
		if len(keys) != 1 || keys[0] != "key" {
			t.Errorf("Keys() = %v, want [key]", keys)
		}

		// Test Clone()
		cloned := doc.Clone()
		if cloned == nil {
			t.Fatal("Clone() returned nil")
		}

		// Test ToYAML()
		yaml, err := doc.ToYAML()
		if err != nil {
			t.Fatalf("ToYAML() failed: %v", err)
		}
		if len(yaml) == 0 {
			t.Error("ToYAML() returned empty bytes")
		}

		// Test ToJSON()
		jsonBytes, err := doc.ToJSON()
		if err != nil {
			t.Fatalf("ToJSON() failed: %v", err)
		}
		if len(jsonBytes) == 0 {
			t.Error("ToJSON() returned empty bytes")
		}
	})

	t.Run("Engine options work correctly", func(t *testing.T) {
		engine, err := graft.NewEngine(
			graft.WithCache(true, 500),
			graft.WithConcurrency(5),
			graft.WithDebugLogging(false),
		)
		if err != nil {
			t.Fatalf("NewEngine with options failed: %v", err)
		}
		if engine == nil {
			t.Fatal("NewEngine with options returned nil")
		}
	})
}

// TestExternalImportInterfaces validates interface satisfaction.
func TestExternalImportInterfaces(t *testing.T) {
	t.Run("Engine interface is public", func(t *testing.T) {
		engine, err := graft.NewEngine()
		if err != nil {
			t.Fatalf("NewEngine() failed: %v", err)
		}

		// Verify it satisfies the Engine interface
		var _ graft.Engine = engine
	})

	t.Run("Document interface is public", func(t *testing.T) {
		engine, err := graft.NewEngine()
		if err != nil {
			t.Fatalf("NewEngine() failed: %v", err)
		}

		doc, err := engine.ParseYAML([]byte(`key: value`))
		if err != nil {
			t.Fatalf("ParseYAML() failed: %v", err)
		}

		// Verify it satisfies the Document interface
		var _ graft.Document = doc
	})

	t.Run("MergeBuilder interface is public", func(t *testing.T) {
		engine, err := graft.NewEngine()
		if err != nil {
			t.Fatalf("NewEngine() failed: %v", err)
		}

		doc, err := engine.ParseYAML([]byte(`key: value`))
		if err != nil {
			t.Fatalf("ParseYAML() failed: %v", err)
		}

		ctx := context.Background()

		// Get MergeBuilder and verify interface
		builder := engine.Merge(ctx, doc)
		var _ graft.MergeBuilder = builder
	})
}
