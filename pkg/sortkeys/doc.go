/*
Package sortkeys orders object keys for graft's merge output and for the
jq and yq commands, so that all of them agree on what sorted means.

A Mode names an ordering. None keeps keys where they are, Lexical compares
them by codepoint as jq -S does, and Spruce reproduces the order spruce's
YAML encoder writes, which graft's merge output has always used. Parse
reads a mode name from a flag, Keys and Less order plain key lists, and
Sort reorders every object inside a value.

# Stability

This package is experimental. Until the release that completes graft jq,
its exported names may change between minor releases, and the changelog
lists every such change. From that release on, the module's v1 promise
applies, which is that we add exported names and never remove or change
one. The package imports nothing outside the standard library and
pkg/query/value and pkg/keyorder.
*/
package sortkeys
