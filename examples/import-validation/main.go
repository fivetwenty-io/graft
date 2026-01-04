// Package main provides a standalone example that validates
// graft can be imported and used correctly as an external dependency.
//
// Run this example with: go run examples/import-validation/main.go
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/fivetwenty-io/graft/pkg/graft"

	// Import operators package to register all built-in operators.
	// This blank import triggers the init() functions that register
	// the operator parser and all operator implementations.
	_ "github.com/fivetwenty-io/graft/pkg/graft/operators"
)

func main() {
	fmt.Println("=== Graft Import Validation Example ===")
	fmt.Println()

	exitCode := 0

	// Test 1: Engine Creation
	fmt.Print("1. Creating engine... ")
	engine, err := graft.NewEngine()
	if err != nil {
		fmt.Printf("FAILED: %v\n", err)
		exitCode = 1
	} else {
		fmt.Println("OK")
	}

	// Test 2: YAML Parsing
	fmt.Print("2. Parsing YAML... ")
	yamlContent := []byte(`
app:
  name: validation-test
  version: 1.0.0
`)
	doc, err := engine.ParseYAML(yamlContent)
	if err != nil {
		fmt.Printf("FAILED: %v\n", err)
		exitCode = 1
	} else {
		fmt.Println("OK")
	}

	// Test 3: Document Access
	fmt.Print("3. Accessing document values... ")
	name, err := doc.GetString("app.name")
	if err != nil || name != "validation-test" {
		fmt.Printf("FAILED: expected 'validation-test', got %q (err: %v)\n", name, err)
		exitCode = 1
	} else {
		fmt.Println("OK")
	}

	// Test 4: Document Merging
	fmt.Print("4. Merging documents... ")
	base := []byte(`config: {port: 8080}`)
	override := []byte(`config: {host: localhost}`)
	baseDoc, _ := engine.ParseYAML(base)
	overrideDoc, _ := engine.ParseYAML(override)
	ctx := context.Background()
	merged, err := engine.Merge(ctx, baseDoc, overrideDoc).Execute()
	if err != nil {
		fmt.Printf("FAILED: %v\n", err)
		exitCode = 1
	} else {
		port, _ := merged.GetInt("config.port")
		host, _ := merged.GetString("config.host")
		if port != 8080 || host != "localhost" {
			fmt.Printf("FAILED: unexpected values port=%d, host=%q\n", port, host)
			exitCode = 1
		} else {
			fmt.Println("OK")
		}
	}

	// Test 5: Operator Evaluation
	fmt.Print("5. Evaluating operators... ")
	opDoc, _ := engine.ParseYAML([]byte(`
meta:
  app: test
  env: dev
computed:
  name: (( concat meta.app "-" meta.env ))
`))
	evaluated, err := engine.Evaluate(ctx, opDoc)
	if err != nil {
		fmt.Printf("FAILED: %v\n", err)
		exitCode = 1
	} else {
		computedName, _ := evaluated.GetString("computed.name")
		if computedName != "test-dev" {
			fmt.Printf("FAILED: expected 'test-dev', got %q\n", computedName)
			exitCode = 1
		} else {
			fmt.Println("OK")
		}
	}

	// Test 6: Output Generation (using Document methods)
	fmt.Print("6. Generating YAML output... ")
	outputYAML, err := doc.ToYAML()
	if err != nil || len(outputYAML) == 0 {
		fmt.Printf("FAILED: %v\n", err)
		exitCode = 1
	} else {
		fmt.Println("OK")
	}

	fmt.Print("7. Generating JSON output... ")
	outputJSON, err := doc.ToJSON()
	if err != nil || len(outputJSON) == 0 {
		fmt.Printf("FAILED: %v\n", err)
		exitCode = 1
	} else {
		fmt.Println("OK")
	}

	// Summary
	fmt.Println()
	if exitCode == 0 {
		fmt.Println("All import validation checks PASSED")
		fmt.Println()
		fmt.Println("The graft package is correctly configured for external imports.")
		fmt.Println("External users can import it with:")
		fmt.Println()
		fmt.Println("    import \"github.com/fivetwenty-io/graft/pkg/graft\"")
	} else {
		fmt.Println("Some import validation checks FAILED")
	}

	os.Exit(exitCode)
}
