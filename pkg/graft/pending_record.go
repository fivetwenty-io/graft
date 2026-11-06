package graft

import (
	"github.com/fivetwenty-io/graft/internal/oploc"
	"github.com/fivetwenty-io/graft/internal/pending"
	"github.com/fivetwenty-io/graft/pkg/graft/tree"
)

//nolint:gochecknoinits // the operators package reads the pending record and the path index through these hooks from its first call
func init() {
	pending.SetLookup(func(ev any) *pending.Record {
		if e, ok := ev.(*Evaluator); ok && e != nil {
			return e.pending
		}
		return nil
	})
	oploc.SetLookup(func(ev any) *oploc.Index {
		if e, ok := ev.(*Evaluator); ok && e != nil {
			return e.locIndex
		}
		return nil
	})
}

// startPending gives ev a record of ops as operators that have not run
// yet, and returns a function that puts back the record ev held before.
// RunOp marks each op done as it runs, so while a phase runs the record
// holds exactly the operators still to run.
func (ev *Evaluator) startPending(ops []*Opcall) func() {
	rec := pending.NewRecord()
	for _, op := range ops {
		if at := pendingPath(op); at != nil && op.where != nil {
			rec.Add(at.Nodes, op.where.String())
		}
	}
	saved := ev.pending
	ev.pending = rec
	return func() { ev.pending = saved }
}

// donePending records on ev that op has run, whether or not it
// succeeded.
func (ev *Evaluator) donePending(op *Opcall) {
	if at := pendingPath(op); at != nil {
		ev.pending.Done(at.Nodes)
	}
}

// pendingPath returns the path op is recorded under, which is its
// canonical path, or its own path when it has none.
func pendingPath(op *Opcall) *tree.Cursor {
	if op == nil {
		return nil
	}
	if op.canonical != nil {
		return op.canonical
	}
	return op.where
}
