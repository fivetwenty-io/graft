package graft_test

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"regexp"
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/internal/utils/ansi"
	"github.com/fivetwenty-io/graft/pkg/graft"
	_ "github.com/fivetwenty-io/graft/pkg/graft/operators" // registers grab, concat, param, vault, /, etc.
	"github.com/fivetwenty-io/graft/pkg/graft/tree"
)

// regexpMustCompilePathPrefix mirrors genesis's ManifestProvider.pm regex
// used to find the "$.<path>:" prefix of a graft error line.
func regexpMustCompilePathPrefix() *regexp.Regexp {
	return regexp.MustCompile(`(?m)^\s*-\s*\$\.(\S+?):`)
}

// withColorDisabled disables ansi coloring for the duration of fn,
// restoring the previous state afterward, so MultiError.Error() assertions
// can compare against plain (uncolored) text. This mirrors main.go's
// default ("auto") color handling, which disables color whenever stderr
// isn't a tty — the state genesis always observes in practice, since it
// captures graft's stderr to a file.
func withColorDisabled(t *testing.T, fn func()) {
	t.Helper()
	previous := ansi.IsColorEnabled()
	ansi.Color(false)
	defer ansi.Color(previous)
	fn()
}

// mergeExprErr runs a real merge (mirroring `graft merge`) and returns the
// resulting evaluation error, failing the test if evaluation unexpectedly
// succeeded. It reuses the same engine construction path as mergeYAML
// (a6_backward_compat_test.go, same package) so these tests exercise the
// identical code the CLI runs.
func mergeExprErr(t *testing.T, yamlSrc string) error {
	t.Helper()
	_, err := mergeYAML(t, yamlSrc)
	if err == nil {
		t.Fatalf("expected an evaluation error for %q, got none", yamlSrc)
	}
	return err
}

// firstPathError extracts the first *graft.PathError out of err, which is
// either a graft.MultiError (the normal shape for evaluation failures) or
// already a *graft.PathError.
func firstPathError(t *testing.T, err error) *graft.PathError {
	t.Helper()
	var multi graft.MultiError
	var mep *graft.MultiError
	switch {
	case errors.As(err, &multi):
	case errors.As(err, &mep):
		multi = *mep
	}
	if len(multi.Errors) > 0 {
		err = multi.Errors[0]
	}
	var pe *graft.PathError
	if errors.As(err, &pe) {
		return pe
	}
	t.Fatalf("expected a *graft.PathError somewhere in the chain, got %T: %v", err, err)
	return nil
}

// --- Default output is untouched (genesis contract) -----------------------

func TestMultiErrorDefaultFormatUnchanged(t *testing.T) {
	// GRAFT_ERROR_CODES unset: MultiError.Error() must render the exact
	// "N error(s) detected:\n - $.path: msg\n" spruce/genesis format, with
	// no code annotation, regardless of whether the underlying error is
	// classifiable.
	withColorDisabled(t, func() {
		pe := &graft.PathError{Path: "some.path", Cause: errors.New("unknown operator: bogus_op")}
		me := graft.MultiError{Errors: []error{pe}}

		got := me.Error()
		want := "1 error(s) detected:\n - $.some.path: unknown operator: bogus_op\n\n"
		if got != want {
			t.Fatalf("default MultiError.Error() changed:\n got:  %q\n want: %q", got, want)
		}
	})
}

func TestMultiErrorDefaultFormatUnchangedWithFalsyEnv(t *testing.T) {
	for _, v := range []string{"", "0", "false", "off", "nope"} {
		t.Run("GRAFT_ERROR_CODES="+v, func(t *testing.T) {
			t.Setenv("GRAFT_ERROR_CODES", v)
			withColorDisabled(t, func() {
				pe := &graft.PathError{Path: "x", Cause: errors.New("unknown operator: bogus_op")}
				me := graft.MultiError{Errors: []error{pe}}
				got := me.Error()
				want := "1 error(s) detected:\n - $.x: unknown operator: bogus_op\n\n"
				if got != want {
					t.Fatalf("MultiError.Error() with GRAFT_ERROR_CODES=%q changed:\n got:  %q\n want: %q", v, got, want)
				}
			})
		})
	}
}

// --- Opt-in CLI annotation --------------------------------------------------

func TestMultiErrorOptInAnnotatesClassifiedErrors(t *testing.T) {
	t.Setenv("GRAFT_ERROR_CODES", "1")

	withColorDisabled(t, func() {
		pe := &graft.PathError{Path: "some.path", Cause: errors.New("unknown operator: bogus_op")}
		me := graft.MultiError{Errors: []error{pe}}

		got := me.Error()
		want := "1 error(s) detected:\n - $.some.path: [E205] unknown operator: bogus_op\n\n"
		if got != want {
			t.Fatalf("opted-in MultiError.Error():\n got:  %q\n want: %q", got, want)
		}
	})
}

func TestMultiErrorOptInLeavesUnclassifiedErrorsUnchanged(t *testing.T) {
	t.Setenv("GRAFT_ERROR_CODES", "1")

	withColorDisabled(t, func() {
		pe := &graft.PathError{Path: "some.path", Cause: errors.New("something entirely bespoke went wrong")}
		me := graft.MultiError{Errors: []error{pe}}

		got := me.Error()
		want := "1 error(s) detected:\n - $.some.path: something entirely bespoke went wrong\n\n"
		if got != want {
			t.Fatalf("opted-in MultiError.Error() for unclassified error:\n got:  %q\n want: %q", got, want)
		}
	})
}

func TestMultiErrorOptInStillMatchesGenesisPathRegex(t *testing.T) {
	// genesis's ManifestProvider.pm keys its retry logic off
	// ^\s*-\s*\$\.(\S+?): — confirm the opted-in, code-annotated line still
	// matches, with the same captured path.
	t.Setenv("GRAFT_ERROR_CODES", "1")

	pe := &graft.PathError{Path: "database.host", Cause: errors.New("unknown operator: bogus_op")}
	me := graft.MultiError{Errors: []error{pe}}

	line := me.Error()
	re := regexpMustCompilePathPrefix()
	match := re.FindStringSubmatch(line)
	if match == nil {
		t.Fatalf("opted-in line does not match genesis's path regex: %q", line)
	}
	if match[1] != "database.host" {
		t.Fatalf("captured path = %q, want %q", match[1], "database.host")
	}
}

// --- PathError preserves the exact previous fmt.Errorf("$.%s: %w", ...) shape

func TestPathErrorErrorStringMatchesPriorFormat(t *testing.T) {
	pe := &graft.PathError{Path: "a.b.c", Cause: errors.New("boom")}
	if got, want := pe.Error(), "$.a.b.c: boom"; got != want {
		t.Fatalf("PathError.Error() = %q, want %q", got, want)
	}
	if !errors.Is(pe, pe.Cause) {
		// errors.Is with the exact cause value should short-circuit true
		// via direct equality on the first Unwrap hop.
		t.Fatalf("errors.Is(pe, pe.Cause) = false, want true")
	}
}

// --- WithCode / codedError ---------------------------------------------------

func TestWithCodeNilError(t *testing.T) {
	if err := graft.WithCode(nil, graft.CodeParamRequired); err != nil {
		t.Fatalf("WithCode(nil, ...) = %v, want nil", err)
	}
}

func TestWithCodePreservesErrorStringAndUnwrap(t *testing.T) {
	cause := errors.New("underlying")
	tagged := graft.WithCode(cause, graft.CodeParamRequired)

	if tagged.Error() != cause.Error() {
		t.Fatalf("tagged.Error() = %q, want %q", tagged.Error(), cause.Error())
	}
	if !errors.Is(tagged, cause) {
		t.Fatalf("errors.Is(tagged, cause) = false, want true")
	}
	if code := graft.ClassifyError(tagged); code != graft.CodeParamRequired {
		t.Fatalf("ClassifyError(tagged) = %q, want %q", code, graft.CodeParamRequired)
	}
}

// --- ClassifyError: end-to-end, CLI-reachable triggers ---------------------

func TestClassifyError_ReferenceNotFound(t *testing.T) {
	err := mergeExprErr(t, "x: (( grab nonexistent ))\n")
	pe := firstPathError(t, err)
	if pe.Code() != graft.CodeReferenceNotFound {
		t.Fatalf("Code() = %q, want %q (err: %v)", pe.Code(), graft.CodeReferenceNotFound, err)
	}
}

func TestClassifyError_TypeMismatch(t *testing.T) {
	err := mergeExprErr(t, "a: hello\nx: (( grab a.b ))\n")
	pe := firstPathError(t, err)
	if pe.Code() != graft.CodeTypeMismatch {
		t.Fatalf("Code() = %q, want %q (err: %v)", pe.Code(), graft.CodeTypeMismatch, err)
	}
}

func TestClassifyError_UnknownOperator(t *testing.T) {
	// Every operator-call syntax the parser accepts — top-level "((
	// bogus_op ... ))" and nested "(bogus_op ...)" inside another call —
	// gates on OperatorFor(name) being registered before the identifier is
	// even parsed as an OperatorCall (parser.go identifierOpensOpcallAt);
	// an unregistered name never reaches evaluation, so "unknown operator:
	// %s" cannot be triggered end-to-end through a normal merge today. It
	// is real, exported library surface, though: ValidateOperatorArgs
	// (operator_registry.go), used to validate arguments against
	// OperatorInfoRegistry metadata, returns it directly for any name not
	// in that registry.
	err := graft.ValidateOperatorArgs("bogus_nonexistent_op", 1)
	if err == nil {
		t.Fatalf("expected an error for an unregistered operator name")
	}
	if code := graft.ClassifyError(err); code != graft.CodeUnknownOperator {
		t.Fatalf("ClassifyError(err) = %q, want %q (err: %v)", code, graft.CodeUnknownOperator, err)
	}
}

func TestClassifyError_ArgumentCount(t *testing.T) {
	err := mergeExprErr(t, "x: (( concat \"only-one\" ))\n")
	pe := firstPathError(t, err)
	if pe.Code() != graft.CodeArgumentCount {
		t.Fatalf("Code() = %q, want %q (err: %v)", pe.Code(), graft.CodeArgumentCount, err)
	}
}

// TestClassifyError_ArgumentCount_MessageFamilies covers the operator
// error-message shapes that don't fit the "requires exactly/at least N
// argument(s)" or "too few arguments supplied" families already covered by
// TestClassifyError_ArgumentCount: "no arguments specified to (( ... ))"
// (join, grab, keys, cartesian-product, split), "<op> operator expects N
// argument(s)" (empty), and "requires one or two ... arguments" (file).
func TestClassifyError_ArgumentCount_MessageFamilies(t *testing.T) {
	cases := []struct {
		name string
		yaml string
	}{
		{"join zero-arg", "x: (( join ))\n"},
		{"grab zero-arg", "x: (( grab ))\n"},
		{"keys zero-arg", "x: (( keys ))\n"},
		{"cartesian-product zero-arg", "x: (( cartesian-product ))\n"},
		{"split zero-arg", "x: (( split ))\n"},
		{"empty zero-arg", "x: (( empty ))\n"},
		{"file zero-arg", "x: (( file ))\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := mergeExprErr(t, c.yaml)
			pe := firstPathError(t, err)
			if pe.Code() != graft.CodeArgumentCount {
				t.Fatalf("Code() = %q, want %q (err: %v)", pe.Code(), graft.CodeArgumentCount, err)
			}
		})
	}
}

func TestClassifyError_DivisionByZero(t *testing.T) {
	err := mergeExprErr(t, "a: 1\nb: 0\nx: (( a / b ))\n")
	pe := firstPathError(t, err)
	if pe.Code() != graft.CodeDivisionByZero {
		t.Fatalf("Code() = %q, want %q (err: %v)", pe.Code(), graft.CodeDivisionByZero, err)
	}
}

func TestClassifyError_ParamRequired(t *testing.T) {
	err := mergeExprErr(t, "x: (( param \"x is required\" ))\n")
	pe := firstPathError(t, err)
	if pe.Code() != graft.CodeParamRequired {
		t.Fatalf("Code() = %q, want %q (err: %v)", pe.Code(), graft.CodeParamRequired, err)
	}
}

// TestClassifyError_ParamOperator_WrongArgCount pins the explicit
// graft.WithCode(..., graft.CodeArgumentCount) tag op_param.go applies to
// its own "only expects one argument" branch (distinct from the
// param-required branch above, and from the generic message-pattern
// matching TestClassifyError_ArgumentCount_MessageFamilies covers — this
// one is untagged by message pattern, since "param operator only expects
// one argument" contains neither "requires ..." nor "operator expects").
func TestClassifyError_ParamOperator_WrongArgCount(t *testing.T) {
	err := mergeExprErr(t, "x: (( param \"a\" \"b\" ))\n")
	pe := firstPathError(t, err)
	if pe.Code() != graft.CodeArgumentCount {
		t.Fatalf("Code() = %q, want %q (err: %v)", pe.Code(), graft.CodeArgumentCount, err)
	}
}

func TestClassifyError_UnsupportedTarget(t *testing.T) {
	err := mergeExprErr(t, "x: (( concat@sometarget \"a\" \"b\" ))\n")
	pe := firstPathError(t, err)
	if pe.Code() != graft.CodeUnsupportedTarget {
		t.Fatalf("Code() = %q, want %q (err: %v)", pe.Code(), graft.CodeUnsupportedTarget, err)
	}
}

func TestClassifyError_CircularReference(t *testing.T) {
	err := mergeExprErr(t, "a: (( grab b ))\nb: (( grab a ))\n")
	if code := graft.ClassifyError(err); code != graft.CodeCircularReference {
		t.Fatalf("ClassifyError(err) = %q, want %q (err: %v)", code, graft.CodeCircularReference, err)
	}
}

func TestClassifyError_FileNotFound_ViaFileOperator(t *testing.T) {
	// (( load )) checks os.Stat first and returns a generic "not a file or
	// usable URI" error for a missing path, never exposing the underlying
	// fs.ErrNotExist. (( file )) wraps the os.ReadFile error in spruce's
	// "could not be read" wording but keeps the *fs.PathError reachable
	// via Unwrap, so it is the CLI-reachable trigger for CodeFileNotFound.
	err := mergeExprErr(t, "x: (( file \"/nonexistent/path/that/should/never/exist/graft-e2\" ))\n")
	pe := firstPathError(t, err)
	if pe.Code() != graft.CodeFileNotFound {
		t.Fatalf("Code() = %q, want %q (err: %v)", pe.Code(), graft.CodeFileNotFound, err)
	}
}

// --- ClassifyError: constructor/library-level triggers ---------------------

func TestClassifyError_ParseError(t *testing.T) {
	engine, err := graft.NewEngine()
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}
	_, parseErr := engine.ParseYAML([]byte("a: [unterminated\n"))
	if parseErr == nil {
		t.Fatalf("expected a YAML parse error, got none")
	}
	if code := graft.ClassifyError(parseErr); code != graft.CodeParseError {
		t.Fatalf("ClassifyError(parseErr) = %q, want %q (err: %v)", code, graft.CodeParseError, parseErr)
	}
}

func TestClassifyError_PathSyntaxError(t *testing.T) {
	_, err := tree.ParseCursor("a]")
	if err == nil {
		t.Fatalf("expected a path syntax error, got none")
	}
	if code := graft.ClassifyError(err); code != graft.CodePathSyntaxError {
		t.Fatalf("ClassifyError(err) = %q, want %q (err: %v)", code, graft.CodePathSyntaxError, err)
	}
}

func TestClassifyError_EvaluationErrorConstructor(t *testing.T) {
	err := graft.NewEvaluationError("some.path", "failed to evaluate merged document", errors.New("cause"))
	if code := graft.ClassifyError(err); code != graft.CodeEvaluationError {
		t.Fatalf("ClassifyError(err) = %q, want %q (err: %v)", code, graft.CodeEvaluationError, err)
	}
}

func TestClassifyError_MergeErrorConstructor(t *testing.T) {
	err := graft.NewMergeError("failed to merge documents", errors.New("cause"))
	if code := graft.ClassifyError(err); code != graft.CodeMergeError {
		t.Fatalf("ClassifyError(err) = %q, want %q (err: %v)", code, graft.CodeMergeError, err)
	}
}

func TestClassifyError_ValidationError_ViaDocumentAPI(t *testing.T) {
	engine, err := graft.NewEngine()
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}
	doc, err := engine.ParseYAML([]byte("a: 5\n"))
	if err != nil {
		t.Fatalf("failed to parse YAML: %v", err)
	}
	_, getErr := doc.GetString("a")
	if getErr == nil {
		t.Fatalf("expected doc.GetString(\"a\") to fail (a is an int, not a string)")
	}
	if code := graft.ClassifyError(getErr); code != graft.CodeValidationError {
		t.Fatalf("ClassifyError(getErr) = %q, want %q (err: %v)", code, graft.CodeValidationError, getErr)
	}
}

func TestClassifyError_ExternalErrorConstructor(t *testing.T) {
	err := graft.NewExternalError("some-service", "unreachable", errors.New("cause"))
	if code := graft.ClassifyError(err); code != graft.CodeExternalError {
		t.Fatalf("ClassifyError(err) = %q, want %q (err: %v)", code, graft.CodeExternalError, err)
	}
}

func TestClassifyError_ConfigurationError_ViaNewEngine(t *testing.T) {
	_, err := graft.NewEngine(graft.WithConcurrency(-1))
	if err == nil {
		t.Fatalf("expected NewEngine(WithConcurrency(-1)) to fail")
	}
	if code := graft.ClassifyError(err); code != graft.CodeConfigurationError {
		t.Fatalf("ClassifyError(err) = %q, want %q (err: %v)", code, graft.CodeConfigurationError, err)
	}
}

// --- ClassifyError: stdlib filesystem sentinels (hermetic, no OS access) ---

func TestClassifyError_FileNotFound_Sentinel(t *testing.T) {
	err := &fs.PathError{Op: "open", Path: "/does/not/exist", Err: fs.ErrNotExist}
	if code := graft.ClassifyError(err); code != graft.CodeFileNotFound {
		t.Fatalf("ClassifyError(err) = %q, want %q", code, graft.CodeFileNotFound)
	}
}

func TestClassifyError_PermissionDenied_Sentinel(t *testing.T) {
	err := &fs.PathError{Op: "open", Path: "/root/secret", Err: fs.ErrPermission}
	if code := graft.ClassifyError(err); code != graft.CodePermissionDenied {
		t.Fatalf("ClassifyError(err) = %q, want %q", code, graft.CodePermissionDenied)
	}
}

// --- ClassifyError: nil and unclassified ------------------------------------

func TestClassifyError_Nil(t *testing.T) {
	if code := graft.ClassifyError(nil); code != "" {
		t.Fatalf("ClassifyError(nil) = %q, want empty", code)
	}
}

func TestClassifyError_Unclassified(t *testing.T) {
	err := errors.New("some error nobody bothered to classify")
	if code := graft.ClassifyError(err); code != "" {
		t.Fatalf("ClassifyError(err) = %q, want empty", code)
	}
}

// --- GraftError.Code() covers every ErrorType -------------------------------

func TestGraftErrorCodeExhaustive(t *testing.T) {
	cases := []struct {
		name string
		err  *graft.GraftError
		want graft.ErrorCode
	}{
		{"parse", graft.NewParseError("m", nil), graft.CodeParseError},
		{"merge", graft.NewMergeError("m", nil), graft.CodeMergeError},
		{"evaluation", graft.NewEvaluationError("p", "m", nil), graft.CodeEvaluationError},
		// OperatorError wraps arbitrary caller-supplied text and has no
		// real (non-example) construction site today; deliberately left
		// unclassified rather than guessed at (see errors.go GraftError.Code).
		{"operator", graft.NewOperatorError("op", "m", nil), graft.ErrorCode("")},
		{"configuration", graft.NewConfigurationError("m"), graft.CodeConfigurationError},
		{"validation", graft.NewValidationError("m"), graft.CodeValidationError},
		{"external", graft.NewExternalError("svc", "m", nil), graft.CodeExternalError},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.err.Code(); got != c.want {
				t.Fatalf("Code() = %q, want %q", got, c.want)
			}
		})
	}
}

// --- PartialEvaluationError (plans/dennis-feedback-gaps.md Item 2's
// "engineering gap": Evaluate discarded ev.Tree on error) ------------------

// TestEngineEvaluateReturnsPartialEvaluationErrorOnFailure pins the core
// engineering-gap fix: Engine.Evaluate, on an evaluation failure, wraps
// the error in *graft.PartialEvaluationError carrying the partially-
// evaluated tree (every operator that already succeeded holds its real
// value; the one that failed still carries its own "(( ... ))" text) -
// exactly what graft merge --defer-on-error's adaptive loop needs to
// retry with.
func TestEngineEvaluateReturnsPartialEvaluationErrorOnFailure(t *testing.T) {
	engine, err := graft.NewEngine()
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	doc, err := engine.ParseYAML([]byte(`
meta:
  password: (( vault "secret/db:password" ))
database:
  connection: (( grab meta.password ))
`))
	if err != nil {
		t.Fatalf("ParseYAML: %v", err)
	}

	result, evalErr := engine.Evaluate(context.Background(), doc)
	if evalErr == nil {
		t.Fatal("expected an evaluation error (no Vault reachable in this environment)")
	}
	if result != nil {
		t.Fatalf("expected a nil Document alongside the error, got %v", result)
	}

	var partial *graft.PartialEvaluationError
	if !errors.As(evalErr, &partial) {
		t.Fatalf("expected *graft.PartialEvaluationError, got %T: %v", evalErr, evalErr)
	}
	if partial.Tree == nil {
		t.Fatal("PartialEvaluationError.Tree must not be nil")
	}

	treeData, ok := partial.Tree.RawData().(map[string]interface{})
	if !ok {
		t.Fatalf("Tree.RawData() = %T, want map[string]interface{}", partial.Tree.RawData())
	}
	dbSection, ok := treeData["database"].(map[string]interface{})
	if !ok {
		t.Fatalf("treeData[\"database\"] = %T, want map[string]interface{}", treeData["database"])
	}
	// grab's dependent copies the still-raw vault text rather than
	// erroring - a genuinely different failing operator's dependents
	// would still show their own *PathError, but a plain grab/copy of a
	// failed sibling's raw expression text is not itself an error.
	if dbSection["connection"] != `(( vault "secret/db:password" ))` {
		t.Fatalf("database.connection = %v, want the raw vault expression", dbSection["connection"])
	}

	// Error() must delegate verbatim to the wrapped error - the genesis-
	// compat-contract byte format (MultiError.Error()) is unaffected by
	// this wrapping.
	var multi graft.MultiError
	if !errors.As(evalErr, &multi) {
		t.Fatalf("expected the wrapped error to still be reachable as *graft.MultiError via errors.As, got %T", partial.Err)
	}
	if evalErr.Error() != multi.Error() {
		t.Fatalf("PartialEvaluationError.Error() = %q, want the wrapped MultiError's own Error() text %q", evalErr.Error(), multi.Error())
	}

	// Unwrap() exposes the *PathError for the one failing operator.
	var pe *graft.PathError
	if !errors.As(evalErr, &pe) {
		t.Fatalf("expected a *graft.PathError reachable via errors.As, got none")
	}
	if pe.Path != "meta.password" {
		t.Fatalf("PathError.Path = %q, want %q", pe.Path, "meta.password")
	}
}

// TestEngineEvaluateSuccessUnaffectedByPartialEvaluationError confirms a
// clean (non-failing) Evaluate call is completely unaffected by the
// PartialEvaluationError wrapping: still returns (Document, nil).
func TestEngineEvaluateSuccessUnaffectedByPartialEvaluationError(t *testing.T) {
	engine, err := graft.NewEngine()
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	doc, err := engine.ParseYAML([]byte("a: 1\nb: (( grab a ))\n"))
	if err != nil {
		t.Fatalf("ParseYAML: %v", err)
	}
	result, evalErr := engine.Evaluate(context.Background(), doc)
	if evalErr != nil {
		t.Fatalf("Evaluate() error = %v, want nil", evalErr)
	}
	if result == nil {
		t.Fatal("expected a non-nil Document on success")
	}
	b, err := result.GetInt("b")
	if err != nil || b != 1 {
		t.Fatalf("b = %v (err %v), want 1", b, err)
	}
}

// --- MarshalYAMLWithComments (graft merge --report-deferred=inline) -------

func TestMarshalYAMLWithCommentsNilIsIdenticalToMarshalYAML(t *testing.T) {
	treeData := map[string]interface{}{"a": 1, "b": map[string]interface{}{"c": "d"}}
	plain, err := graft.MarshalYAML(treeData)
	if err != nil {
		t.Fatalf("MarshalYAML: %v", err)
	}
	withNil, err := graft.MarshalYAMLWithComments(treeData, nil)
	if err != nil {
		t.Fatalf("MarshalYAMLWithComments(nil): %v", err)
	}
	if !bytes.Equal(plain, withNil) {
		t.Fatalf("MarshalYAMLWithComments(v, nil) = %q, want MarshalYAML(v)'s own output %q", withNil, plain)
	}
}

func TestMarshalYAMLWithCommentsPlacesHeadCommentAboveMapKey(t *testing.T) {
	treeData := map[string]interface{}{
		"meta": map[string]interface{}{
			"password": `(( vault "secret/db:password" ))`,
		},
		"other": "value",
	}
	out, err := graft.MarshalYAMLWithComments(treeData, []graft.YAMLHeadComment{
		{Path: "meta.password", Lines: []string{" graft: deferred $.meta.password: some reason"}},
	})
	if err != nil {
		t.Fatalf("MarshalYAMLWithComments: %v", err)
	}
	want := "meta:\n  # graft: deferred $.meta.password: some reason\n  password: (( vault \"secret/db:password\" ))\nother: value\n"
	if string(out) != want {
		t.Fatalf("output =\n%s\nwant:\n%s", out, want)
	}
}

func TestMarshalYAMLWithCommentsListIndexPath(t *testing.T) {
	treeData := map[string]interface{}{
		"jobs": []interface{}{
			map[string]interface{}{"name": "one"},
			map[string]interface{}{"name": `(( vault "secret/db:name" ))`},
		},
	}
	out, err := graft.MarshalYAMLWithComments(treeData, []graft.YAMLHeadComment{
		{Path: "jobs.1.name", Lines: []string{" graft: deferred $.jobs.1.name: some reason"}},
	})
	if err != nil {
		t.Fatalf("MarshalYAMLWithComments: %v", err)
	}
	if !bytesContainsString(out, "# graft: deferred $.jobs.1.name: some reason") {
		t.Fatalf("output missing the expected comment line:\n%s", out)
	}
	if !bytesContainsString(out, `name: (( vault "secret/db:name" ))`) {
		t.Fatalf("output missing the deferred list-entry value:\n%s", out)
	}
}

func TestMarshalYAMLWithCommentsUnresolvablePathIsSkippedNotFatal(t *testing.T) {
	treeData := map[string]interface{}{"a": 1}
	out, err := graft.MarshalYAMLWithComments(treeData, []graft.YAMLHeadComment{
		{Path: "", Lines: []string{" graft: unreachable"}},
		{Path: "a['weird]", Lines: []string{" graft: also unreachable"}},
	})
	if err != nil {
		t.Fatalf("MarshalYAMLWithComments: %v", err)
	}
	if bytesContainsString(out, "unreachable") {
		t.Fatalf("expected unresolvable comment paths to be silently skipped, got:\n%s", out)
	}
	if string(out) != "a: 1\n" {
		t.Fatalf("output = %q, want %q (document unaffected by the skipped comments)", out, "a: 1\n")
	}
}

func bytesContainsString(b []byte, s string) bool {
	return regexp.MustCompile(regexp.QuoteMeta(s)).Match(b)
}

func TestMultiErrorDoesNotReprocessBodyDirectives(t *testing.T) {
	// An error message carrying a literal @r{...} - as an operator
	// expression quoted from a user's document can - must survive
	// verbatim. Before this fix it was deleted with color off and turned
	// into live ANSI with color on.
	me := graft.MultiError{Errors: []error{
		errors.New(`cycle at (( grab @r{secret} ))`),
	}}

	got := me.Error()

	if !strings.Contains(got, `(( grab @r{secret} ))`) {
		t.Errorf("Error() = %q; the body's @r{...} was reprocessed instead of preserved", got)
	}
	idx := strings.Index(got, "error(s) detected")
	if idx == -1 {
		t.Fatalf("Error() = %q; missing %q", got, "error(s) detected")
	}
	if strings.Contains(got[idx:], "\033[") {
		t.Errorf("Error() = %q; the body must contain no escape bytes", got)
	}
}

func TestMultiErrorKeepsSpruceByteFormat(t *testing.T) {
	// Color defaults to on in this test binary (no tty auto-detection
	// applies), so this comparison against plain bytes needs color
	// disabled explicitly, matching the sibling tests above in this file.
	withColorDisabled(t, func() {
		me := graft.MultiError{Errors: []error{
			errors.New("$.meta.a: bad"),
			errors.New("$.meta.b: worse"),
		}}

		got := me.Error()

		want := "2 error(s) detected:\n - $.meta.a: bad\n - $.meta.b: worse\n\n"
		if got != want {
			t.Errorf("Error() = %q, want %q", got, want)
		}
	})
}
