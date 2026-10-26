/*
Package value defines the ordered, immutable value model that the jq and
yq engines share.

A Value is null, a boolean, a number, a string, an array, or an object,
and Kind says which. Numbers are exact decimals from pkg/decimal, so a
literal such as 100000000000000000001 keeps every digit, and an unmodified
number prints as it was written. Objects keep their keys in insertion
order. Constructors such as Null, Num, Str, Arr, and Obj build values, the
accessors read them, and the updaters, such as Set and SetIndex, return a
changed copy that shares every untouched part with the original. An
ArrBuilder or ObjBuilder fills a new container in place before sealing it,
which is cheaper than a run of updates. Compare and Equal follow jq's sort
order and its == operator. Meta carries the YAML presentation details yq
keeps, such as comments, anchors, and scalar styles.

# Stability

This package is experimental. Until the release that completes graft jq,
its exported names may change between minor releases, and the changelog
lists every such change. From that release on, the module's v1 promise
applies, which is that we add exported names and never remove or change
one. The package imports nothing outside the standard library and
pkg/decimal.
*/
package value
