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

# Stability

This package is experimental. Until the release that completes graft jq,
which is the J3 release, its exported names may change between minor
releases, and the changelog lists every such change. From that release on,
the module's v1 promise applies, which is that we add exported names and
never remove or change one.
*/
package jq
