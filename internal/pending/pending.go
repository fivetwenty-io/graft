// Package pending records which operators of an evaluation phase have
// not run yet, so an operator that reads the tree can tell an operator
// still waiting to run from text that only looks like one. The evaluator
// keeps the record. The operators package reads it through a hook that
// pkg/graft installs, so the evaluator exports no field for it.
package pending

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/fivetwenty-io/graft/internal/literal"
)

// Record holds the operators of one phase that are scheduled and have
// not run yet, keyed by their canonical path. It is safe for concurrent
// use, and a nil *Record holds nothing.
type Record struct {
	mu sync.Mutex
	at map[string]string
}

// NewRecord returns an empty Record.
func NewRecord() *Record {
	return &Record{at: map[string]string{}}
}

// Add records that the operator at the canonical path nodes, written in
// the document at where, has not run yet.
func (r *Record) Add(nodes []string, where string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.at[key(nodes)] = where
}

// Done records that the operator at the canonical path nodes has run,
// whether or not it succeeded.
func (r *Record) Done(nodes []string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.at, key(nodes))
}

// At reports whether an operator at the canonical path nodes has not run
// yet, and if so the path its document wrote it at.
func (r *Record) At(nodes []string) (string, bool) {
	if r == nil {
		return "", false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	where, ok := r.at[key(nodes)]
	return where, ok
}

// key encodes nodes so that no two paths share a key, even when a node
// holds a dot.
func key(nodes []string) string {
	var b strings.Builder
	for _, n := range nodes {
		b.WriteString(strconv.Itoa(len(n)))
		b.WriteByte(':')
		b.WriteString(n)
	}
	return b.String()
}

// lookup returns the Record of the evaluator ev, or nil.
var lookup func(ev any) *Record

// SetLookup installs the function For calls. pkg/graft calls it once,
// from an init function.
func SetLookup(f func(ev any) *Record) {
	lookup = f
}

// For returns the Record of the evaluator ev, or nil when ev keeps none,
// as when it runs operators outside a phase.
func For(ev any) *Record {
	if lookup == nil {
		return nil
	}
	return lookup(ev)
}

// ErrNotEvaluated is the error every NotEvaluatedError matches with
// errors.Is.
var ErrNotEvaluated = errors.New("a grab read a path whose operator has not run yet")

// NotEvaluatedError is the error a grab returns when the path it reads
// holds an operator that has not run yet, and the dependency graph could
// not order the grab after it. It names both paths and quotes no value.
type NotEvaluatedError struct {
	// Grab is the path of the operator call that ran the grab, without
	// a leading "$.".
	Grab string
	// Target is the path of the operator the grab reached, without a
	// leading "$.".
	Target string
}

func (e *NotEvaluatedError) Error() string {
	return fmt.Sprintf("(( grab )) at $.%s reads $.%s before the operator there has run, and graft could not order the grab after it", e.Grab, e.Target)
}

// Is reports whether target is ErrNotEvaluated.
func (e *NotEvaluatedError) Is(target error) bool {
	return target == ErrNotEvaluated
}

// StopsFallback reports whether err must not be answered by trying the
// other side of a || fallback: a refused literal, or a grab of an
// operator that has not run, whose fallback would hide a wrong order.
func StopsFallback(err error) bool {
	return literal.IsRefusal(err) || errors.Is(err, ErrNotEvaluated)
}
