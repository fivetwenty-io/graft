/*
Package jq runs jq programs from Go with the same engine graft jq uses.

Compile parses a program and binds its variables, and the Query it returns
runs over any number of inputs, one at a time or concurrently. Run yields
each output as the program produces it, so a caller who stops ranging
early stops the program too. A Decoder reads JSON, YAML, or raw lines one
value at a time, Slurp gathers several decoders into one value as -s does,
and an Encoder writes values the way jq prints them. ExitCode turns how a
run ended into the code jq would exit with.

# Quick start

	q, err := jq.Compile(`.items[] | .name`)
	if err != nil {
		return err
	}
	dec := jq.NewDecoder(os.Stdin, jq.DecodeOptions{})
	enc := jq.NewEncoder(os.Stdout, jq.EncodeOptions{Raw: true})
	defer enc.Flush()
	for {
		in, err := dec.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		for v, err := range q.Run(ctx, in) {
			if err != nil {
				return err
			}
			if err := enc.Encode(v); err != nil {
				return err
			}
		}
	}

Errors are the types in pkg/query, so a caller tells a program that did
not compile from one that failed while it ran with errors.As.

# Security

A program reads no module unless the caller passes WithModulePaths, and a
module path that climbs out of those directories is refused. The process
environment is another matter, because $ENV and env read it by default. A
program that embeds this package to run jq programs it does not trust should
pass WithEnv with the entries the programs may see, or with none.

# Stability

This package is stable. The module's v1 promise applies to it, which is
that we add exported names and never remove or change one.
*/
package jq
