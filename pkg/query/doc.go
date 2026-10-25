/*
Package query holds the types that graft's jq and yq engines share with
their callers, which are the errors a program can end with.

A CompileError means the program never ran because it did not parse or
did not compile, and it carries the line and column where the problem
starts. A RuntimeError is an error raised while the program ran, either by
the program itself through error or by an operation such as indexing a
number, and it carries the value the program raised when there is one. A
HaltError ends the run on purpose, through halt_error, with an exit code
and the message to print.

# Stability

This package is experimental. Until the release that completes graft jq,
its exported names may change between minor releases, and the changelog
lists every such change. From that release on, the module's v1 promise
applies, which is that we add exported names and never remove or change
one. The package imports nothing outside the standard library and
pkg/query/value.
*/
package query
