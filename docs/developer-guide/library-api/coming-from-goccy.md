# Coming from goccy

This page is for programs that decode and encode YAML with `github.com/goccy/go-yaml` and want to use `github.com/fivetwenty-io/graft/pkg/yaml` instead. The package reads and writes YAML the way graft's merge engine does, so a document means the same thing in both places. Nothing in its API asks a caller to import goccy.

We start with what changes when the switch is made, then map the goccy calls we use most to their graft counterparts.

## What changes when we switch

The defaults differ from goccy's in the following ways.

YAML 1.1 booleans

A plain `yes`, `no`, `on`, or `off` decodes as a boolean, because graft's merge reads them that way, and a quoted one stays a string. `yaml.UnmarshalWith(data, &v, yaml.DecodeOptions{YAML12: true})` turns this off, so those words decode as strings.

Repeated keys

A mapping that repeats a key is refused with an error that names both lines and never quotes the key. `DecodeOptions.OnDuplicateKey` accepts a repeated key instead, keeps the last value, and calls our function once for each later occurrence.

Whole floats

A float that holds a whole number marshals as integer digits, so `3.0` becomes `3`. There is no option to change this.

Quoting

Marshal quotes strings by spruce's rules, with a few additional rules of its own. There is no option for this either.

Struct tags

A field takes its key from its `yaml` tag, then from its `json` tag, and then from its name in lower case. The tag options `omitempty`, `inline`, and `flow` work as they do in goccy.

Aliases and merge keys

Aliases and `<<` merge keys are expanded when we decode into a Go value. A `Node` target keeps them as written, and `Node.Decode` expands them.

Marshaler hooks

`MarshalYAML() (any, error)` and `encoding.TextMarshaler` are honored on encode. The `MarshalYAML() ([]byte, error)` form, which returns YAML text, is refused.

## Mapping goccy calls to graft calls

### Indentation

goccy takes indent settings as options, and graft takes them in `EncodeOptions`.

```go
// goccy
out, err := goyaml.MarshalWithOptions(v, goyaml.Indent(4), goyaml.IndentSequence(true))

// graft
out, err := yaml.MarshalWith(v, yaml.EncodeOptions{Indent: 4, IndentSequence: true})
```

`Indent` accepts 0, which means 2, and 2 through 9. Any other value is an error. Without `IndentSequence`, a sequence sits at the column of its key.

### Editing a document in place

goccy programs that change a value in a file usually call `parser.ParseBytes` and then edit the `ast` they get back, or they decode, change, and encode the whole file. graft's counterpart is `yaml.Document`, which holds the file as its original bytes and changes only the bytes that an edit names.

```go
// goccy
file, err := parser.ParseBytes(data, parser.ParseComments)
// ... edit the ast, then file.String()

// graft
d, err := yaml.ParseDocument(data)
err = d.Set("port", 9090)
out := d.Bytes()
```

Comments, blank lines, quoting, and indentation outside the edit stay as written. A path names the node, as in `jobs.name=web.port`, and an edit that can't be made without changing other bytes is refused with an error that says why. See [Editing a File with yaml.Document](yaml-document.md) for the paths, the types `Set` accepts, and every refusal.

### Comparing documents

goccy has no comparison of its own, so programs write their own walk over two decoded values. graft's counterpart is `yaml.Diff`, which compares two YAML or JSON inputs with the engine that `graft diff` uses.

```go
r, err := yaml.Diff(before, after, nil)
for _, c := range r.Changes() {
    fmt.Println(c.Kind, c.Path)
}
```

See [Comparing Raw YAML with yaml.Diff](yaml-diff.md) for the report, the options, and how paths read.

### Ordered maps

goccy's `yaml.MapSlice` and `yaml.MapItem` have graft counterparts with the same names and the same fields.

```go
out, err := yaml.Marshal(yaml.MapSlice{
    {Key: "name", Value: "api"},
    {Key: "instances", Value: 2},
})
```

Marshal writes the items in the order we gave them, at any depth. The Go maps inside the values still follow the key order in `EncodeOptions`. `MapSlice` is for encoding only, so `Unmarshal` into one returns an error that says to decode into a `Node`. Only the exact types `MapSlice` and `*MapSlice` are ordered, and a named type built on `MapSlice` is an ordinary slice. A key that is a collection is refused, and so are two items whose keys write the same text.

### Streams of documents

goccy's `NewEncoder` and `NewDecoder` take options as arguments. graft's versions come in two forms, one with defaults and one that takes `EncodeOptions` or `DecodeOptions`.

```go
// goccy
enc := goyaml.NewEncoder(w, goyaml.Indent(4))
dec := goyaml.NewDecoder(r, goyaml.AllowDuplicateMapKey())

// graft
enc := yaml.NewEncoderWith(w, yaml.EncodeOptions{Indent: 4})
dec := yaml.NewDecoderWith(r, yaml.DecodeOptions{OnDuplicateKey: func(yaml.DuplicateKey) {}})
```

An `Encoder` writes `---` before every document after the first. `Close` writes nothing, may be called twice, and makes later calls to `Encode` fail. A `Decoder` returns `io.EOF` when the documents run out, as goccy's does.

The graft `Decoder` follows these stream rules.

- On its first `Decode`, it reads all of the reader, and then cuts the stream into documents.

- Only a framing error ends the stream, and the error stays latched. A framing error is a reader error, invalid UTF-16 after a byte order mark, or content after a `...` with no `---` before it.

- Any other error, such as a repeated key, a type mismatch, a bad merge, or an error that a hook returns, fails only its own document. The next `Decode` moves on to the next one.

- Lines in errors, in duplicate-key reports, and in the `Node` values a hook reads count from the start of the stream.

- An alias can't point at an anchor in an earlier document, because YAML scopes an anchor to its own document. goccy and yaml.v3 allow it.

Two differences from goccy's decoder are worth knowing. First, content after a `...` with no `---` fails in graft, where goccy accepts it. Second, graft skips a document that holds only comments, directives, or markers, such as a `---` followed by `...`, wherever it sits in the stream. goccy skips such a document only at the very end, and it fails when one sits between two documents. Take `a: 1`, `---`, `...`, `---`, and `b: 2` on separate lines, where graft returns both documents.

### Types that decode themselves

goccy has several hook forms. graft has one, which is the same shape as yaml.v3's.

```go
type Unmarshaler interface {
    UnmarshalYAML(node *Node) error
}
```

The method receives the `Node` that holds its value, so it can read `Line`, `Column`, and `Tag`, and it can call `node.Decode` to read the value into another type. `Node.Decode`, `Unmarshal`, `UnmarshalWith`, and `Decoder.Decode` all call it, wherever a value of the type sits in the target.

```go
// goccy
func (e *Endpoint) UnmarshalYAML(unmarshal func(any) error) error {
    type plain Endpoint
    p := plain{Port: 8080}
    if err := unmarshal(&p); err != nil {
        return err
    }
    *e = Endpoint(p)
    return nil
}

// graft
func (e *Endpoint) UnmarshalYAML(node *yaml.Node) error {
    type plain Endpoint
    p := plain{Port: 8080}
    if err := node.Decode(&p); err != nil {
        return err
    }
    *e = Endpoint(p)
    return nil
}
```

The private type `plain` has the same fields as `Endpoint` and none of its methods. Decoding into it keeps the hook from calling itself. We need this pattern whenever the method reads its own type.

Several rules go with the hook.

- Embedding a type that has the method promotes it to the outer type. The outer type then decodes through it, and its other fields stay unset unless the method fills them. A struct that embeds `Endpoint` and adds a `Name` field loses the name for this reason. Give the outer type its own method, or hold the inner type in a named field.

- A null never reaches the method, whether it has no tag or the tag `!!null`, as in yaml.v3. The null then decodes as it would for a type without the method, so a struct field and the value at the root keep what they held, a pointer below the root becomes nil, and a map value or a list item becomes the zero value.

- A type that has both this method and `UnmarshalText` decodes through this one.

- A type that implements `Unmarshaler` and is used as a map key is refused with an error. So is one used as a named `,inline` field.

- When a struct has a named field that implements the hook and also an `,inline` map or a self-decoding inline field, the inline part no longer sees the hook field's key. This matches yaml.v3.

- When a named field and an `,inline` struct both read the same key, the hook doesn't run for that key, and the fields get a plain decode. yaml.v3 refuses such a struct. Two fields with the same `yaml` name never get this far, because goccy fails them with "duplicated struct field name".

- If goccy's own decode of the document fails, no hook runs. That error is the one we get, even when a hook would also have failed.

- Errors from `node.Decode(&plain)` inside a hook name the private type and field, as in `plain.Port`. We give the private types readable names because those names reach the message.

- The `DecodeError` that a failed hook produces has an `Unwrap` method, so `errors.Is` and `errors.As` reach the error the hook returned. `Error` never prints that cause.

A target that reaches an `Unmarshaler` decodes by a second route that parses the document once more, so hook targets cost a little more to decode. Targets without a hook cost what they did before.

### goccy's own hook forms

goccy's hook interfaces, which are `BytesUnmarshaler`, `InterfaceUnmarshaler`, `NodeUnmarshaler`, and the variants that take a context, still fire in 1.47.0. What a `BytesUnmarshaler` receives depends on the route that decodes the target. When the target holds no `Unmarshaler`, `Unmarshal`, `UnmarshalWith`, and `Decoder.Decode` give it the source text of its value, without its comments, as `Unmarshal` did in 1.46.0. Under `Node.Decode`, and whenever the target also reaches an `Unmarshaler`, it receives flow-style text, which keeps every value but not the layout. That second case is new in 1.47.0, so a `BytesUnmarshaler` that reads the layout of its bytes can behave differently once we add an `Unmarshaler` field beside it. A method that takes a callback decodes the same values on either route. `Unmarshaler` is the supported hook, and a major release may stop calling the others.

## Behavior that existing callers can notice

A type that already has an `UnmarshalYAML(*yaml.Node) error` method now has it called by `Node.Decode`, `Unmarshal`, `UnmarshalWith`, and `Decoder.Decode`. If our code already has such a method, we should read it once before upgrading, because it now runs in places it did not run before. The other three changes, which cover map keys, inline fields, and the cost of the second decode route, are listed under the hook rules above and in the Changed section of the CHANGELOG entry for 1.47.0.

## Concurrency

A `Decoder` or an `Encoder` is not safe for concurrent use. The graft `Engine` has its own rule, which we restate here from `pkg/graft/api.go`. One `Engine` may be shared by any number of goroutines that parse, merge, evaluate, and diff at the same time, because each `Merge().Execute()` and `Evaluate` call keeps its working state to itself. Three rules go with that.

- `Configure` and `UpdateOptions` rewrite the engine's configuration, so they must not run while another call on the engine is in progress.

- A `Document` belongs to one call at a time, because `Evaluate` resolves operators in the `Document` it is given.

- The engine's own operator state holds the outputs of finished runs. The vault references, deferred paths, and vault placeholders it reports accumulate across every run that has finished, and `GetLastEvaluatedPrunedPaths` reports the run that finished last. A caller that needs exactly one run's outputs uses an engine of its own.
