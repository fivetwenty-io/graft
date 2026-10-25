/*
Package regex is a backtracking regular expression engine with PCRE syntax
and an Oniguruma flavor, written for graft's jq and yq commands and usable on
its own. It imports nothing outside the standard library.

# Stability

This package is experimental. Until the release that completes graft jq,
its exported names may change between minor releases, and the changelog
lists every such change. From that release on, the module's v1 promise
applies, which is that we add exported names and never remove or change
one.
*/
package regex
