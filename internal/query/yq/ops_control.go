package yq

import (
	"os"
	"path/filepath"

	"github.com/fivetwenty-io/graft/internal/query/builtin"
	"github.com/fivetwenty-io/graft/internal/query/eval"
	"github.com/fivetwenty-io/graft/internal/query/jq"
	"github.com/fivetwenty-io/graft/internal/query/parser"
	"github.com/fivetwenty-io/graft/internal/query/runtime"
	"github.com/fivetwenty-io/graft/pkg/query"
	"github.com/fivetwenty-io/graft/pkg/query/value"
)

// opsPrelude defines the yq operators that take the place of jq prelude
// definitions of the same name, which would otherwise answer first. It
// is jq source, as the prelude it follows is.
const opsPrelude = `
def map(f): _yq_like([.[] | f]);
def sort_by(f): _yq_sort_by(f);
def unique_by(f): _yq_unique_by(f);
def group_by(f): _yq_group_by(f);
def reverse: _yq_reverse;
def flatten: _yq_flatten(null);
def flatten($depth): _yq_flatten($depth);
def join($sep): _yq_join($sep);
def match(re): _yq_match(re);
def match(re; flags): _yq_match(re; flags);
def test(re): _yq_test(re);
def test(re; flags): _yq_test(re; flags);
def capture(re): _yq_capture(re);
def capture(re; flags): _yq_capture(re; flags);
def sub(re; repl): _yq_sub(re; repl);
def with(p; f): _yq_with(p | f);
`

// splitDoc is the document index split_doc gives an output, which run
// turns into the output's own document. It is below runtime.Detached, so
// the two never meet.
const splitDoc = runtime.Detached - 1

// errFilesDisabled is yq's message when --security-disable-file-ops is
// set.
var errFilesDisabled = &query.RuntimeError{Msg: "file operations have been disabled"}

// addOps adds yq's collection, string, conversion, environment, control,
// and arithmetic operators to r, which already holds the core natives and
// jq's prelude, and the definitions that take the place of the prelude's.
func addOps(r *runtime.Registry) {
	registerCollections(r)
	registerStrings(r)
	registerConvert(r)
	registerEnv(r)
	registerControl(r)
	registerArith(r)
	prog, err := parser.ParseFile(opsPrelude, preludeFile, jq.Grammar())
	if err != nil {
		panic("yq: operator prelude does not parse: " + err.Error())
	}
	r.AddPrelude(prog)
}

// errorArgMissing is yq's message for error without an argument.
const errorArgMissing = "'error' expects 1 arg but received none"

// registerControl adds error, with's body, eval, split_doc, and the file
// operators. yq's error fails with its argument's text, and with no text
// at all for a collection.
func registerControl(r *runtime.Registry) {
	r.RegisterSingle("error", 1, func(_ *runtime.Ctx, in *value.Value, args []runtime.Thunk, _ runtime.Emit) error {
		return args[0].Each(in, func(v *value.Value, _ value.Path) error {
			return &query.RuntimeError{Msg: nodeText(v)}
		})
	})
	// Parse already refuses error without an argument, as yq does.
	r.RegisterSingle("error", 0, func(*runtime.Ctx, *value.Value, []runtime.Thunk, runtime.Emit) error {
		return &query.RuntimeError{Msg: errorArgMissing}
	})
	// rewrite puts _yq_none in place of a comma's repeated side.
	r.RegisterSingle("_yq_none", 0, func(*runtime.Ctx, *value.Value, []runtime.Thunk, runtime.Emit) error {
		return nil
	})
	r.RegisterSingle("_yq_with", 1, with)
	r.Register("eval", 1, evalOp)
	r.RegisterSingle("split_doc", 0, func(ctx *runtime.Ctx, in *value.Value, _ []runtime.Thunk, emit runtime.Emit) error {
		if c := ctx.Cursor; c != nil {
			return emit(in, runtime.Rooted(c.Root, splitDoc, c.Path))
		}
		return emit(in, runtime.Rooted(in, splitDoc, nil))
	})
	loader(r, "load", func(b []byte) (*value.Value, error) {
		docs, err := parseDocuments(string(b), true)
		switch {
		case err != nil:
			return nil, err
		case len(docs) == 0:
			return emptyNull(), nil
		case len(docs) == 1:
			return docs[0], nil
		default:
		}
		return value.Arr(docs...), nil
	})
	for _, name := range []string{"load_str", "strload"} {
		loader(r, name, func(b []byte) (*value.Value, error) { return value.Str(string(b)), nil })
	}
	loader(r, "load_base64", func(b []byte) (*value.Value, error) {
		d, err := decodeBase64(string(b))
		if err != nil {
			return nil, &query.RuntimeError{Msg: err.Error()}
		}
		return value.Str(string(d)), nil
	})
}

// with runs its argument, which is with's body at each node its path
// names, for the changes that makes to the document, and gives its input
// as the last of the argument's outputs in that document left it. The
// body may change the document anywhere, as yq's does. An output with no
// place in the document, such as a literal, changes nothing.
func with(ctx *runtime.Ctx, in *value.Value, args []runtime.Thunk, emit runtime.Emit) error {
	var c *runtime.Cursor
	if ctx.Cursor != nil {
		cur := *ctx.Cursor
		c = &cur
	}
	var root *value.Value
	err := args[0].Each(in, func(_ *value.Value, p value.Path) error {
		if r, doc, _ := runtime.Unroot(p); r != nil && c != nil && doc == c.Doc {
			root = r
		}
		return nil
	})
	if err != nil {
		return err
	}
	if root == nil || root == c.Root {
		return emit(in, nil)
	}
	base, _ := nodePath(c)
	at, _ := resolve(root, base)
	v, err := builtin.GetPath(ctx, root, at)
	if err != nil {
		return err
	}
	return emit(v, runtime.Rooted(root, c.Doc, at))
}

// evalOp parses each string its argument gives as a yq expression and
// runs it on the input with the run's capabilities. Its outputs keep
// their places, below the input's own, and it fails when the input's
// place cannot hold a document the expression changed.
func evalOp(ctx *runtime.Ctx, in *value.Value, args []runtime.Thunk, emit runtime.Emit) error {
	var c *runtime.Cursor
	if ctx.Cursor != nil {
		cur := *ctx.Cursor
		c = &cur
	}
	return args[0].Each(in, func(src *value.Value, _ value.Path) error {
		node, err := Parse(nodeText(src))
		if err != nil {
			return err
		}
		prog := eval.NewProgram(node, rootEnv, registry())
		return prog.Run(ctx, prog.Env, []*value.Value{in}, func(v *value.Value, p value.Path) error {
			root, _, rel := runtime.Unroot(p)
			if root == nil || c == nil {
				return emit(v, nil)
			}
			top := c.Root
			if root != in {
				var err error
				if top, err = builtin.SetPath(ctx, c.Root, c.Path, root); err != nil {
					return err
				}
			}
			return emit(v, runtime.Rooted(top, c.Doc, extend(c.Path, rel...)))
		})
	})
}

// loader registers a file operator, which reads the file its argument
// names while the capabilities allow it and gives what read makes of it.
func loader(r *runtime.Registry, name string, read func([]byte) (*value.Value, error)) {
	r.RegisterSingle(name, 1, func(ctx *runtime.Ctx, in *value.Value, args []runtime.Thunk, emit runtime.Emit) error {
		if !ctx.Caps.Files {
			return errFilesDisabled
		}
		return args[0].Each(in, func(file *value.Value, _ value.Path) error {
			name := nodeText(file)
			b, err := os.ReadFile(filepath.Clean(name)) // #nosec G703 -- the query names the file to read
			if err != nil {
				return &query.RuntimeError{Msg: "failed to load " + name + ": " + err.Error()}
			}
			v, err := read(b)
			if err != nil {
				return err
			}
			return emit(v, nil)
		})
	})
}
