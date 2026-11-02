/*
Package yaml reads and writes YAML the way graft's merge engine does, so a
program that decodes configuration with this package sees the same values
that graft merge sees.

The package decodes with Unmarshal and UnmarshalWith, encodes with Marshal
and MarshalWith, and exposes a parsed document as a Node tree. It is built
on the decoder and encoder that graft uses internally. A document therefore
reads the same here as it does in a merge, and a value written here reads
back the same in both.

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

Unmarshal reads YAML 1.1 booleans by default, because graft's merge does. A
plain yes, Yes, YES, no, No, NO, on, On, ON, off, Off, or OFF decodes as a
boolean, and a quoted one stays a string. Other spellings of those words,
such as oFF, stay strings, and so do the single letters y and n.

UnmarshalWith with DecodeOptions{YAML12: true} reads those words as strings,
as YAML 1.2 does.

A string target holds a scalar as written, so 1.10 stays "1.10" and 0x1F
stays "0x1F", as in yaml.v2 and yaml.v3.

# Differences from yaml.v2 and yaml.v3

A program moving from yaml.v2 or yaml.v3 meets these differences.

Unmarshal refuses a map[interface{}]interface{} anywhere in the target type,
and the error says to use map[string]any. Every nested map in a generic
value is a map[string]any, so a caller never type-asserts a map twice.

Every error that Unmarshal and Node.Decode return is a *DecodeError, and it
carries a Line, a Column, a Kind, and a Msg. The text of a DecodeError never
quotes the input, so it is safe to log when the input holds credentials. For
that reason the messages are shorter than the ones yaml.v2 and yaml.v3 give,
which often print the offending scalar.

Marshal writes map keys in spruce's order with two spaces of indentation, as
graft merge does. MarshalWith with EncodeOptions{KeyOrder: sortkeys.Lexical}
writes keys in codepoint order instead.

Merge keys follow graft's engine. When a mapping holds a << key, the merged
values win over a key written before the << in the same mapping, and a key
written after the << wins over the merged values. When the value of << is a
list of maps, a later map wins over an earlier one.

yaml.v3 differs from graft in two ways. A key written in the mapping always
wins over a merged value there, wherever the << sits, and an earlier map in
the list wins over a later one. yaml.v2 agrees with graft about a key
written before the << (the merged value wins) and about a key written after
it, but it also lets an earlier map in the list win.

A time.Time target accepts a quoted date such as "2001-12-14". yaml.v2 and
yaml.v3 refuse a quoted date that is not RFC 3339.

A named bool type, such as type Flag bool, reads YAML 1.2 booleans only, so
a plain yes is a type error for it. An unnamed bool field reads yes as true.

A !!str ~ or a !!str null decodes as the empty string, not as the text. When
a mapping holds both !!str null and ~ as keys, a map[string]T target
collapses them into one key, the empty string. A generic map keeps them
apart, with !!str null as the key "" and ~ as the key "null".

A !!timestamp tag on a value that decodes into a string target is refused
with an ErrorKindType error. The same value decoded into an any gives a
time.Time.

A panic that the decoder raises on a target shape it cannot fill is
recovered and returned as an ErrorKindType DecodeError with Line 0. A panic
raised by a type's own UnmarshalYAML or MarshalYAML method is not the
decoder's, and it is not recovered, even when the method misuses package
reflect and the panic starts inside it.

A type with an UnmarshalText method receives the formatted text of a scalar,
not the source text, so 1.10 arrives as "1.1". A type with an UnmarshalYAML
method receives the YAML as flow-style text, which keeps every value but not
the layout or the comments of the input.

A whole float inside the int64 range marshals as integer digits, so 3.0
becomes 3 and 1e6 becomes 1000000. Reading that output into an any gives an
int. A whole float outside that range prints with an exponent, so 1e20
becomes 1.0e+20, and any other float keeps its decimal point. These outputs
differ from yaml.v2 for values at or above 1e6, which it writes as 1e+06.

Marshal chooses quotes by spruce's rules plus a few more of its own. It
double-quotes a string that ends in <<, a string that starts with -, ?, or :
followed by any character other than a space, such as -foo, ?x, or -#, and a
multi-line string whose first line starts with whitespace. Inside a field
tagged ,flow, a string in a list or map is double-quoted when it needs
quotes, and a string field that is itself tagged ,flow is single-quoted. A
key is written in the "? key" form when its double-quoted form would be
longer than 1024 bytes, so a plain key of 1023 bytes already takes it.

Marshal refuses a value that holds a Node, because writing a Node tree back
out is not supported. It also refuses a cycle and a key order other than the
zero value, sortkeys.Spruce, and sortkeys.Lexical. Each refusal is an error
that names a Go type and a field path, and it never quotes a value.

# Known limitations

These behaviors follow from the decoder that graft shares with its merge.

A scalar with an explicit !!str, !!bool, !!int, !!float, !!null, or !!binary
tag reads as spruce reads it, in both modes, and graft merge gives the same
result. So !!str yes stays the word yes, !!int 0x1f is 31, and !!binary
decodes base64 to the text of its bytes. When the tag cannot hold its text,
as in a: !!int word, the document fails with an ErrorKindSyntax error that
names the tags and the tag's position and never quotes the text. A second
tag on one node and a tag on an alias are syntax errors too, as they are in
spruce.

A scalar under a tag that spruce doesn't resolve reads as its text when it
decodes into a string or a generic value, as it does in spruce. That covers
!!timestamp, a custom tag such as !foo, and the bare ! tag. So !foo yes
stays the word yes, and !!timestamp word stays the word rather than becoming
the zero time. A time.Time field still reads a date written under
!!timestamp.

The decoder refuses a few tagged documents that spruce reads. A !!str or
other scalar tag on a flow or block collection is a syntax error, where
spruce ignores the tag. So is a tag with no value before the next line of a
mapping or sequence, as in "a: !!str\nb: 1", where spruce reads the empty
string. A !!set, !!omap, or !!pairs tag on a scalar, as in a: !!set yes, is a
syntax error as well, where spruce reads the text. And a tag with nothing
after it at the end of the document, as in a: !!str, reads as null, because
the parser drops the tag before the decoder sees it, where spruce reads the
empty string.

A tagged mapping key reads as a string key, because a decoded map's keys are
strings. So "!!bool yes: a" gives the key "true", and graft merge prints it
quoted, as "true": a, where spruce prints true: a. Untagged keys such as true
and 1 print quoted in the same way.

An anchored mapping key that is quoted or tagged !!str loses that quoting
when an alias uses it as a value. So in "&k !!str yes: 1\nb: *k", b reads as
true in the default mode, where spruce reads the word yes.

A !!binary scalar whose bytes aren't valid UTF-8, as in a: !!binary /w==,
decodes to a string that holds those bytes. Marshal and graft merge write
that string out as raw bytes, so the output isn't valid UTF-8, where spruce
writes it back as !!binary with its base64 text.

graft merge prints a whole float with a decimal point, so !!float 1e3 and an
untagged 1.0 print as 1000.0 and 1.0, where spruce prints 1000 and 1. The
decoded float64 is the same either way, and Marshal in this package writes
1000.

A literal or folded block scalar holding only a YAML 1.1 boolean word, as in
"a: |-\n  yes", reads as that word in both modes, as it does in spruce.

Unmarshal reads a document that starts with a UTF-16 byte order mark, in
either byte order, by converting it to UTF-8 first. A UTF-16 document with
no content, an odd number of bytes, or an unpaired surrogate is a syntax
error.

Node.Decode matches Unmarshal on a tree that Unmarshal returned, with a few
exceptions. They arise because the tree reader follows yaml.v3 while
Unmarshal follows goccy.

The two disagree on a node with two tags, as in a: !!str !!int 1, which
Unmarshal refuses and Decode reads as the integer. They disagree on a tag
written on an alias, as in b: !!str *x, which Unmarshal refuses and Decode
reads as the value the alias names. They also disagree on a tag with nothing
after it at the end of the document, as in a: !!str, which Unmarshal reads
as null and Decode reads as the empty string.

The last disagreement is a block scalar in a document with no final line
break. There Unmarshal drops the last line break, and it also fails to fold
a > scalar. For the document "a: >\n  x\n  y", Unmarshal gives "x\ny" and
Decode gives "x y\n". Finally, the column of a continuation line in a
multi-line quoted scalar is approximate when the input uses tabs.

A Node that Unmarshal returned keeps its whole input document, and the first
Decode on that tree parses it one more time. The cost is one extra parse and
the memory of one document for each tree that is read.

A type that decodes through its own UnmarshalYAML method, or that keeps what
it decodes in an unexported field, receives goccy's raw values. In the
default mode, a quoted or block scalar holding yes, no, on, or off can reach
that method with an internal marker character in front of it when the
target also holds a value of interface type.

# Stability

This package is stable, and the module's v1 promise applies to it. We add
exported names and never remove or change one. The exported fields of
DecodeError are part of that promise, but the text that Error returns may
change between releases. The package imports only the standard library,
goccy's go-yaml, graft's internal packages, and pkg/sortkeys with the
packages it imports. It never imports pkg/graft.
*/
package yaml
