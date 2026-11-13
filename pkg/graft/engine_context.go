package graft

import (
	"context"
	"fmt"
	"io"
)

// orBackground returns ctx, or context.Background when ctx is nil, so
// every Context method treats a nil ctx as Evaluate does.
func orBackground(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

// parseCanceled is stopped for the parse methods.
func parseCanceled(ctx context.Context, step string) error {
	return stopped(ctx, "parse", step)
}

// ParseYAML parses YAML data into a Document.
func (e *DefaultEngine) ParseYAML(data []byte) (Document, error) {
	return e.ParseYAMLContext(context.Background(), data)
}

// ParseJSON parses JSON data into a Document.
func (e *DefaultEngine) ParseJSON(data []byte) (Document, error) {
	return e.ParseJSONContext(context.Background(), data)
}

// ParseYAMLContext is ParseYAML under ctx. It checks ctx on entry and
// again after parsing, and a canceled ctx yields an error that wraps
// ctx.Err(). Like every Context method, it treats a nil ctx as
// context.Background.
func (e *DefaultEngine) ParseYAMLContext(ctx context.Context, data []byte) (Document, error) {
	ctx = orBackground(ctx)
	if err := parseCanceled(ctx, "parsing YAML"); err != nil {
		return nil, err
	}
	doc, err := e.parseYAML(data)
	if cerr := parseCanceled(ctx, "returning the YAML document"); cerr != nil {
		return nil, cerr
	}
	return doc, err
}

// ParseJSONContext is ParseJSON under ctx, with the checks of
// ParseYAMLContext.
func (e *DefaultEngine) ParseJSONContext(ctx context.Context, data []byte) (Document, error) {
	ctx = orBackground(ctx)
	if err := parseCanceled(ctx, "parsing JSON"); err != nil {
		return nil, err
	}
	doc, err := e.parseJSON(data)
	if cerr := parseCanceled(ctx, "returning the JSON document"); cerr != nil {
		return nil, cerr
	}
	return doc, err
}

// ParseFileContext is ParseFile under ctx, with the checks of
// ParseYAMLContext.
func (e *DefaultEngine) ParseFileContext(ctx context.Context, path string) (Document, error) {
	ctx = orBackground(ctx)
	doc, err := e.parseFile(ctx, path)
	if cerr := parseCanceled(ctx, "returning the document"); cerr != nil {
		return nil, cerr
	}
	return doc, err
}

// ParseReaderContext is ParseReader under ctx. Each Read checks ctx
// first, so a cancel that lands mid-read stops the read.
func (e *DefaultEngine) ParseReaderContext(ctx context.Context, r io.Reader) (Document, error) {
	ctx = orBackground(ctx)
	if err := parseCanceled(ctx, "reading input"); err != nil {
		return nil, err
	}
	data, err := io.ReadAll(&contextReader{ctx: ctx, r: r})
	if err != nil {
		if isCancel(ctx, err) {
			return nil, err
		}
		return nil, fmt.Errorf("error reading input: %w", err)
	}
	return e.ParseYAMLContext(ctx, data)
}

// contextReader checks ctx before each Read.
type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (c *contextReader) Read(p []byte) (int, error) {
	if err := parseCanceled(c.ctx, "reading input"); err != nil {
		return 0, err
	}
	return c.r.Read(p)
}

// DiffContext is DiffWithOptions under ctx. It checks ctx before and
// after the comparison, and a canceled ctx yields a nil DiffResult and an
// error that wraps ctx.Err().
func (e *DefaultEngine) DiffContext(ctx context.Context, a, b Document, opts *DiffOptions) (DiffResult, error) {
	ctx = orBackground(ctx)
	if err := stopped(ctx, "diff", "comparing the documents"); err != nil {
		return nil, err
	}
	result, err := DiffDocuments(a, b, opts)
	if cerr := stopped(ctx, "diff", "returning the result"); cerr != nil {
		return nil, cerr
	}
	if err != nil {
		return nil, err
	}
	return result, nil
}

// encodeContext evaluates doc under ctx, which stops the evaluation
// between operators, and then serializes the result with encode. A nil
// doc is a validation error, as in the plain methods.
func (e *DefaultEngine) encodeContext(ctx context.Context, name string, doc Document, encode func(Document) ([]byte, error)) ([]byte, error) {
	ctx = orBackground(ctx)
	if doc == nil {
		return nil, NewValidationError(name + ": document must be non-nil")
	}
	if err := stopped(ctx, "encoding", "evaluating the document"); err != nil {
		return nil, err
	}
	evaluated, err := e.Evaluate(ctx, doc)
	if err != nil {
		return nil, fmt.Errorf("failed to evaluate document: %w", err)
	}
	if err := stopped(ctx, "encoding", "serializing the document"); err != nil {
		return nil, err
	}
	out, err := encode(evaluated)
	if cerr := stopped(ctx, "encoding", "returning the output"); cerr != nil {
		return nil, cerr
	}
	return out, err
}

// ToYAMLContext is ToYAML under ctx. A canceled ctx stops the evaluation
// between operators and yields an error that wraps ctx.Err().
func (e *DefaultEngine) ToYAMLContext(ctx context.Context, doc Document) ([]byte, error) {
	return e.encodeContext(ctx, "ToYAML", doc, Document.ToYAML)
}

// ToJSONContext is ToJSON under ctx, with the behavior of ToYAMLContext.
func (e *DefaultEngine) ToJSONContext(ctx context.Context, doc Document) ([]byte, error) {
	return e.encodeContext(ctx, "ToJSON", doc, Document.ToJSON)
}

// ToJSONIndentContext is ToJSONIndent under ctx, with the behavior of
// ToYAMLContext.
func (e *DefaultEngine) ToJSONIndentContext(ctx context.Context, doc Document, indent string) ([]byte, error) {
	return e.encodeContext(ctx, "ToJSONIndent", doc, func(d Document) ([]byte, error) {
		return d.ToJSONIndent(indent)
	})
}
