/*
Package yq runs yq expressions from Go with the same engine graft yq uses.

Compile parses an expression and applies the options that stand for yq's
flags, and the Evaluator it returns runs over any number of documents, one
at a time or concurrently. Eval runs the expression once for a document
with that document as the only root, as yq's eval does, and EvalAll runs it
once with every document in the candidate set, as eval-all does. Both yield
each output as the expression produces it, so a caller who stops ranging
early stops the run too. A Decoder reads YAML or JSON one document at a
time and keeps the comments, styles, anchors, and tags of what it reads, an
Encoder writes values back the way yq prints them, and ExitCode turns how a
run ended into the code yq would exit with.

# Quick start

	ev, err := yq.Compile(`.a = 2 | .b += ["y"]`)
	if err != nil {
		return err
	}
	dec := yq.NewDecoder(os.Stdin, yq.DecodeOptions{})
	enc := yq.NewEncoder(os.Stdout, yq.EncodeOptions{UnwrapScalar: true})
	defer enc.Flush()
	for {
		doc, err := dec.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		for v, err := range ev.Eval(ctx, doc) {
			if err != nil {
				return err
			}
			if err := enc.Encode(v); err != nil {
				return err
			}
		}
	}

Errors are the types in pkg/query, so a caller tells an expression that did
not compile from one that failed while it ran with errors.As.

# Security

The defaults match the yq command line, so the load operators, which read any
file the process can read, and the env operators, which read the process
environment, are both on. A program that embeds this package to run
expressions it does not trust should pass WithFileOps(false) and
WithEnvOps(false) when it compiles them. It can also pass WithEnv to give the
expressions an environment of its own choosing.

# Stability

This package is stable. The module's v1 promise applies to it, which is
that we add exported names and never remove or change one.
*/
package yq
