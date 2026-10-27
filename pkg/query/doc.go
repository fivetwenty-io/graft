/*
Package query holds the types that graft's jq and yq engines share with
their callers, which are the errors a program can end with and the Status
a command ends with.

A CompileError means the program never ran because it did not parse or
did not compile, and it carries the line and column where the problem
starts. A RuntimeError is an error raised while the program ran, either by
the program itself through error or by an operation such as indexing a
number, and it carries the value the program raised when there is one. A
HaltError ends the run on purpose, through halt_error, with an exit code
and the message to print. A Status pairs the exit code a command returns
with the diagnostic it prints.

# Stability

This package is stable. The module's v1 promise applies to it, which is
that we add exported names and never remove or change one. The package
imports nothing outside the standard library and pkg/query/value.
*/
package query
