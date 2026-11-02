/*
Package yaml reads and writes YAML the way graft's merge engine does, so
a program that decodes configuration with this package sees the same
values that graft merge sees.

The package decodes with Unmarshal and UnmarshalWith, encodes with Marshal
and MarshalWith, and exposes a parsed document as a Node tree. It is built
on the decoder and encoder that graft uses internally, which means a
document reads the same here as it does in a merge, and a value written
here reads back the same in both.

# Quick start

Decode a document into a struct, a map, or a slice, and look at the error
with errors.As when the document is wrong.

	type Config struct {
		Name    string   `yaml:"name"`
		Workers int      `yaml:"workers"`
		Hosts   []string `yaml:"hosts,omitempty"`
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		var de *yaml.DecodeError
		if errors.As(err, &de) {
			log.Printf("line %d: %s", de.Line, de.Msg)
		}
		return err
	}

	out, err := yaml.Marshal(cfg)

Decode into a *any, a *map[string]any, or a *[]any when the shape is not
known in advance. Maps come back as map[string]any, sequences as []any,
integers as int, and floats as float64.

# YAML 1.1 and YAML 1.2

Unmarshal reads YAML 1.1 booleans by default, because graft's merge does.
A plain yes, Yes, YES, no, No, NO, on, On, ON, off, Off, or OFF decodes as
a boolean, and a quoted one stays a string. Other spellings of those
words, such as oFF, and the single letters y and n, stay strings.

UnmarshalWith with DecodeOptions{YAML12: true} reads those words as
strings, as YAML 1.2 does.

A string target holds a scalar as written, so 1.10 stays "1.10" and 0x1F
stays "0x1F", as in yaml.v2 and yaml.v3.

# Differences from yaml.v2 and yaml.v3

A program moving from yaml.v2 or yaml.v3 meets these differences.

Unmarshal refuses a map[interface{}]interface{} anywhere in the target
type, and the error says to use map[string]any. Every nested map in a
generic value is a map[string]any, so a caller never type-asserts a map
twice.

Every error is a *DecodeError with a Line, a Column, a Kind, and a Msg.
The text of a DecodeError never quotes the input, so it is safe to log when
the input holds credentials. For that reason the messages are shorter than
the ones yaml.v2 and yaml.v3 give, which often print the offending scalar.

Marshal writes map keys in spruce's order with two spaces of indentation,
as graft merge does. MarshalWith with EncodeOptions{KeyOrder:
sortkeys.Lexical} writes keys in codepoint order instead.

Merge keys follow graft's engine. When a mapping holds a << key, the merged
values win over a key written before the << in the same mapping, and a key
written after the << wins over the merged values. When the value of << is a
list of maps, a later map wins over an earlier one. In yaml.v2 and yaml.v3,
a key written in the mapping always wins over a merged value, wherever the
<< sits, and an earlier map in the list wins.

A time.Time target accepts a quoted date such as "2001-12-14", where yaml.v2
and yaml.v3 refuse a quoted date that is not RFC 3339.

A named bool type, such as type Flag bool, reads YAML 1.2 booleans only, so
a plain yes is a type error for it. An unnamed bool field reads yes as true.

A !!str ~ or a !!str null decodes as the empty string, not as the text.
When a mapping holds both !!str null and ~ as keys, a map[string]T target
collapses them into one key, the empty string. A generic map keeps them
apart, with !!str null as the key "" and ~ as the key "null".

A !!timestamp tag on a value that decodes into a string target is refused
with an ErrorKindType error. The same value decoded into an any gives a
time.Time.

A panic that the decoder raises on a target shape it cannot fill is
recovered and returned as an ErrorKindType DecodeError with Line 0. A panic
raised by a type's own UnmarshalYAML or MarshalYAML method is not the
decoder's, and it is not recovered.

A type with an UnmarshalText method receives the formatted text of a
scalar, not the source text, so 1.10 arrives as "1.1". A type with an
UnmarshalYAML method receives the YAML as flow-style text, which keeps every
value but not the layout or the comments of the input.

A whole float inside the int64 range marshals as integer digits, so 3.0
becomes 3 and 1e6 becomes 1000000, and reading that output into an any
gives an int. A whole float outside that range prints with an exponent, so
1e20 becomes 1.0e+20, and any other float keeps its decimal point. These
outputs differ from yaml.v2 for values at or above 1e6, which it writes as
1e+06.

Marshal chooses quotes by spruce's rules plus a few more of its own. It
double-quotes a string that ends in <<, a string that starts with -#, and
a multi-line string whose first line starts with whitespace. Inside a field
tagged ,flow, a string that needs quotes is always double-quoted. A quoted
key longer than 1024 bytes is written in the "? key" form.

Marshal refuses a value that holds a Node, because writing a Node tree back
out is not supported. It also refuses a cycle and a key order other than the
zero value, sortkeys.Spruce, and sortkeys.Lexical. Each refusal is an error
that names a Go type and a field path and never quotes a value.

# Known limitations

These behaviors follow from the decoder that graft shares with its merge.

An explicitly tagged !!str yes decodes as true in the default mode, because
the merge engine reads the word before it looks at the tag. A quoted "yes"
stays a string.

A tab inside a plain scalar, as in a: x<TAB>y, is dropped, so the value is
"xy". This is how the decoder reads such a scalar, and graft merge gives
the same result. A quoted scalar keeps its tab.

Node.Decode matches Unmarshal on a tree that Unmarshal returned, with a few
exceptions that come from the tree reader following yaml.v3 and Unmarshal
following goccy. The two can disagree on a document that starts with a
UTF-16 byte order mark, on a node with two tags or two anchors, on a tag or
an anchor written on an alias, and on a tag that ends its line or ends the
document. They can also disagree on a bool target next to a comment or a
--- or ... line in the default mode, and on a document with no final line
break, where Decode reads the text as if the break were there. The column of
a continuation line in a multi-line quoted scalar is approximate when the
input uses tabs.

A Node that Unmarshal returned keeps its whole input document, and the
first Decode on that tree parses it one more time. The cost is one extra
parse and the memory of one document for each tree that is read.

A type that decodes through its own UnmarshalYAML method, or that keeps
what it decodes in an unexported field, receives goccy's raw values. In the
default mode a quoted yes, no, on, or off can reach that method with an
internal marker character in front of it when the target also holds a
value of interface type.

# Stability

This package is stable. The module's v1 promise applies to it, which is
that we add exported names and never remove or change one. The exported
fields of DecodeError are part of that promise, but the text Error returns
may change between releases. The package imports nothing outside the
standard library, goccy's go-yaml, graft's internal packages, and
pkg/sortkeys with the packages it imports. It never imports pkg/graft.
*/
package yaml
