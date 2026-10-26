package jq_test

import (
	"errors"
	"testing"

	"github.com/fivetwenty-io/graft/internal/query/jq"
	"github.com/fivetwenty-io/graft/pkg/query"
)

func TestParseChecksSyntaxAlone(t *testing.T) {
	if _, err := jq.Parse(`$undefined | nosuchfn`); err != nil {
		t.Errorf("names are not resolved by Parse, got %v", err)
	}
	_, err := jq.Parse("1 |\n .[")
	var ce *query.CompileError
	if !errors.As(err, &ce) {
		t.Fatalf("got %T %v, want a *query.CompileError", err, err)
	}
	wantEqual(t, ce.Line, 2)
}
