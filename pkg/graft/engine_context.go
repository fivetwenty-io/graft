package graft

import (
	"context"
	"fmt"
	"io"
)

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
// ctx.Err().
func (e *DefaultEngine) ParseYAMLContext(ctx context.Context, data []byte) (Document, error) {
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
	doc, err := e.parseFile(ctx, path)
	if cerr := parseCanceled(ctx, "returning the document"); cerr != nil {
		return nil, cerr
	}
	return doc, err
}

// ParseReaderContext is ParseReader under ctx. Each Read checks ctx
// first, so a cancel that lands mid-read stops the read.
func (e *DefaultEngine) ParseReaderContext(ctx context.Context, r io.Reader) (Document, error) {
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
