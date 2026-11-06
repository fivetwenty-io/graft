package graft

import (
	"errors"
	"regexp"
	"strings"

	"github.com/fivetwenty-io/graft/internal/errtext"
	"github.com/fivetwenty-io/graft/internal/pending"
)

// loopBindingKey is the reserved top-level key that control-flow
// expansion stores loop bindings under.
const loopBindingKey = "__graft_loop"

// loopBindingPrefix matches the prefix the loop rewrite puts in front of
// a loop variable, "__graft_loop.<loop id>.<iteration>.", so that
// "svc.name" in a loop body reads "__graft_loop.l1a2b3c4d5e6f_1.0.svc.name".
// The loop id is "l", twelve hex digits of the source's digest, "_", and
// a sequence number (see controlflow's nextUID).
var loopBindingPrefix = regexp.MustCompile(`__graft_loop\.l[0-9a-f]+_\d+\.\d+\.`)

// withoutLoopPrefix returns text with every loop binding prefix taken
// out, so a path reads as the user wrote it in the loop body.
func withoutLoopPrefix(text string) string {
	if !strings.Contains(text, loopBindingKey) {
		return text
	}
	return loopBindingPrefix.ReplaceAllString(text, "")
}

// withoutLoopNames returns err with the loop binding prefix taken out of
// every message in it, when the tree holds loop bindings. Every phase of
// both evaluator modes returns its error through runPhase, which calls
// this once, so an error from an operator in a loop body and a cycle
// through one name the paths and the operator source the user wrote.
// The scrub only removes the prefix and never adds text.
//
// The error keeps its type at the top, so a MultiError stays a
// MultiError, a *PathError a *PathError, and a *CycleError a
// *CycleError. Each error in a MultiError is scrubbed on its own, so a
// caller that walks the list sees clean messages too. Any other error
// whose text holds the prefix is wrapped in a loopNamesError, which
// keeps it on the Unwrap chain, so errors.Is and errors.As match the
// same sentinels and types as before.
func (ev *Evaluator) withoutLoopNames(err error) error {
	if err == nil {
		return nil
	}
	if _, ok := ev.Tree[loopBindingKey]; !ok {
		return err
	}
	_, scrubbed := scrubLoopNames(err)
	return scrubbed
}

// scrubLoopNames is withoutLoopNames for one error. It reports whether
// it changed anything, so that callers never compare errors, which can
// hold types that == panics on.
func scrubLoopNames(err error) (bool, error) {
	switch e := err.(type) { //nolint:errorlint // the shape at the top is kept, and the rest is wrapped
	case MultiError:
		items, changed := scrubLoopNameList(e.Errors)
		if !changed {
			return false, err
		}
		return true, MultiError{Errors: items}
	case *MultiError:
		items, changed := scrubLoopNameList(e.Errors)
		if !changed {
			return false, err
		}
		return true, &MultiError{Errors: items}
	case *PathError:
		if scrubbed, changed := scrubPathError(e); changed {
			return true, scrubbed
		}
		return false, err
	case *CycleError:
		if scrubbed, changed := scrubCycleError(e); changed {
			return true, scrubbed
		}
		return false, err
	}
	plain, detail := err.Error(), errtext.Detail(err)
	newPlain, newDetail := withoutLoopPrefix(plain), withoutLoopPrefix(detail)
	if newPlain == plain && newDetail == detail {
		return false, err
	}
	return true, &loopNamesError{cause: err, plain: newPlain, detail: newDetail}
}

// scrubLoopNameList scrubs each error in errs, returning a new list
// when any of them changed.
func scrubLoopNameList(errs []error) ([]error, bool) {
	out := make([]error, len(errs))
	changed := false
	for i, item := range errs {
		var itemChanged bool
		itemChanged, out[i] = scrubLoopNames(item)
		changed = changed || itemChanged
	}
	if !changed {
		return errs, false
	}
	return out, true
}

// scrubPathError returns a copy of e whose path and cause are scrubbed.
func scrubPathError(e *PathError) (*PathError, bool) {
	path := withoutLoopPrefix(e.Path)
	cause, causeChanged := e.Cause, false
	if cause != nil {
		causeChanged, cause = scrubLoopNames(cause)
	}
	if path == e.Path && !causeChanged {
		return e, false
	}
	return &PathError{Path: path, Cause: cause}, true
}

// scrubCycleError returns a copy of e whose node paths and operator
// sources are scrubbed. The sources then show each operator as the user
// wrote it in the loop body.
func scrubCycleError(e *CycleError) (*CycleError, bool) {
	nodes := make([]CycleNode, len(e.Nodes))
	changed := false
	for i, n := range e.Nodes {
		nodes[i] = n
		nodes[i].Path = withoutLoopPrefix(n.Path)
		nodes[i].Expr = withoutLoopPrefix(n.Expr)
		changed = changed || nodes[i].Path != n.Path || nodes[i].Expr != n.Expr
	}
	if !changed {
		return e, false
	}
	return &CycleError{Inputs: e.Inputs, Nodes: nodes}, true
}

// loopNamesError is an error whose plain and detailed texts had the loop
// binding prefix taken out. cause stays on the Unwrap chain, so
// errors.Is sees every sentinel it matched before. errors.As hands out
// a scrubbed copy of the typed errors that carry paths, so their fields
// and their own text read as the user wrote them too, and finds every
// other type in cause as before.
type loopNamesError struct {
	cause  error
	plain  string
	detail string
}

func (e *loopNamesError) Error() string         { return e.plain }
func (e *loopNamesError) DetailedError() string { return e.detail }
func (e *loopNamesError) Unwrap() error         { return e.cause }

// As finds a typed error that carries paths in cause and hands out a
// scrubbed copy of it. For any other target it reports false, and
// errors.As goes on down cause.
func (e *loopNamesError) As(target any) bool {
	switch t := target.(type) {
	case **PathError:
		var found *PathError
		if !errors.As(e.cause, &found) {
			return false
		}
		*t, _ = scrubPathError(found)
		return true
	case **CycleError:
		var found *CycleError
		if !errors.As(e.cause, &found) {
			return false
		}
		*t, _ = scrubCycleError(found)
		return true
	case **pending.NotEvaluatedError:
		var found *pending.NotEvaluatedError
		if !errors.As(e.cause, &found) {
			return false
		}
		scrubbed := *found
		scrubbed.Grab = withoutLoopPrefix(found.Grab)
		scrubbed.Target = withoutLoopPrefix(found.Target)
		*t = &scrubbed
		return true
	case *MultiError:
		var found MultiError
		if !errors.As(e.cause, &found) {
			return false
		}
		items, _ := scrubLoopNameList(found.Errors)
		*t = MultiError{Errors: items}
		return true
	}
	return false
}

// withoutLoopText takes the loop binding prefix out of every string in
// data, map keys included, in place. Evaluation leaves a loop body's
// text as written when it is not an operator to run, such as a lone
// "(( svc ))", an operator embedded in other text, or an expression the
// parser cannot read, and that text still holds the rewrite's prefix.
// It runs once the bindings are pruned, so the output reads as the user
// wrote it. A literal.Value is left whole, since its content is data.
func withoutLoopText(data interface{}) interface{} {
	switch v := data.(type) {
	case string:
		return withoutLoopPrefix(v)
	case map[string]interface{}:
		for key, item := range v {
			plain := withoutLoopPrefix(key)
			if plain == key {
				v[key] = withoutLoopText(item)
				continue
			}
			if _, taken := v[plain]; taken {
				v[key] = withoutLoopText(item)
				continue
			}
			delete(v, key)
			v[plain] = withoutLoopText(item)
		}
		return v
	case []interface{}:
		for i, item := range v {
			v[i] = withoutLoopText(item)
		}
		return v
	}
	return data
}
