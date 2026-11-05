package graft_test

import (
	"context"
	"errors"
	"fmt"

	"github.com/fivetwenty-io/graft/pkg/graft"
	_ "github.com/fivetwenty-io/graft/pkg/graft/operators"
)

// ExampleNewLiteral merges parameters that a user supplied over a plan's
// defaults. The wrapped value replaces the default whole, its "(( ... ))"
// text is never evaluated, and the plan can still read into it.
func ExampleNewLiteral() {
	engine, err := graft.NewEngine()
	if err != nil {
		fmt.Println("error:", err)
		return
	}

	plan, err := engine.ParseYAML([]byte(`
params:
  size: small
  tags: [a, b]
meta:
  size: (( grab params.size ))
`))
	if err != nil {
		fmt.Println("error:", err)
		return
	}

	user := graft.NewDocument(map[string]interface{}{
		"params": graft.NewLiteral(map[string]interface{}{
			"size": "large",
			"note": `(( vault "secret/never:read" ))`,
		}),
	})

	result, err := engine.Merge(context.Background(), plan, user).Execute()
	if err != nil {
		fmt.Println("error:", err)
		return
	}

	out, err := result.ToYAML()
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Print(string(out))
	// Output:
	// meta:
	//   size: large
	// params:
	//   note: (( vault "secret/never:read" ))
	//   size: large
}

// ExampleErrLiteralRefused tells a refusal of a caller's literal apart
// from every other merge failure. Here the parameter that a user supplied
// reaches (( calc )) as an expression, which graft never runs.
func ExampleErrLiteralRefused() {
	engine, err := graft.NewEngine()
	if err != nil {
		fmt.Println("error:", err)
		return
	}

	plan, err := engine.ParseYAML([]byte(`
meta:
  secret: 21
size: (( calc params.size ))
`))
	if err != nil {
		fmt.Println("error:", err)
		return
	}

	user := graft.NewDocument(map[string]interface{}{
		"params": graft.NewLiteral(map[string]interface{}{
			"size": "meta.secret * 2",
		}),
	})

	_, err = engine.Merge(context.Background(), plan, user).Execute()
	if errors.Is(err, graft.ErrLiteralRefused) {
		fmt.Println("refused: a parameter was used as code")
		return
	}
	fmt.Println("merged:", err)
	// Output:
	// refused: a parameter was used as code
}
