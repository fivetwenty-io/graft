# Aliases

Graft looks at the name it was run under, and it behaves as a different tool when that name is `jq` or `spruce`.

## Why aliases exist

Scripts and tools call `jq` and `spruce` by name. When graft runs under one of those names, those callers get graft without any change to the script. Genesis relies on this for `spruce`, and `jq` now works the same way through `graft jq`.

The [jq page](jq.md) describes which names dispatch and what changes under the `jq` name.

## Linking by hand

On Linux and macOS, a symbolic link is enough.

```sh
ln -s "$(command -v graft)" /usr/local/bin/jq
ln -s "$(command -v graft)" /usr/local/bin/spruce
```

Then `jq '.name' file.yml` runs `graft jq '.name' file.yml`, and `spruce merge a.yml b.yml` runs `graft merge a.yml b.yml`.

## graft install-aliases

The `graft install-aliases` command makes the links for us.

```sh
graft install-aliases
graft install-aliases --dir /usr/local/bin
graft install-aliases --dry-run
graft install-aliases jq
```

With no names, it installs both `jq` and `spruce`. Name one or more aliases to install only those. The links go in the directory that holds graft unless `--dir` says otherwise. They point at the path we ran graft by, or at the resolved path on systems such as Linux that report only that.

| Flag | Meaning |
|------|---------|
| `--dir DIR` | Install into this directory |
| `--dry-run` | Show what would happen without changing anything |
| `--force` | Replace a target that is not graft's |
| `--remove` | Remove the aliases instead of installing them |

A target that already exists and is not graft's is skipped among the defaults, and refused when we name it. `--force` replaces it, except that a directory, or the running binary itself, is never touched.

```sh
$ graft install-aliases --dry-run
would link jq -> /usr/local/bin/graft at /usr/local/bin/jq
would link spruce -> /usr/local/bin/graft at /usr/local/bin/spruce
would link 2, already installed 0, skipped 0
```

## Windows

Windows links need privileges that many accounts lack, so `graft install-aliases` installs copies named `jq.exe` and `spruce.exe` in place of links. A copy does not follow a graft upgrade, so we run `graft install-aliases --force` again after upgrading.

## Removing aliases

```sh
graft install-aliases --remove
```

This removes only links to this graft and copies of it. A file with the same name that belongs to some other tool stays where it is.

## Coexisting with spruce and jq

A link named `jq` shadows a real jq that sits elsewhere on `PATH`, and which one wins depends on the order of the directories. If a script behaves differently after we install the alias, `command -v jq` shows which one runs. `jq --version` under the alias prints jq's version line first, then graft's own, so the two are easy to tell apart.

When we run it from `PATH`, the `spruce` alias leads its version output with the exact line spruce prints, which is what the Genesis version gate reads.

```sh
$ spruce --version
spruce - Version X.Y.Z
graft version X.Y.Z (commit: ..., built: ..., go: ..., os/arch: ...)
```

Under the `jq` name, graft's own persistent flags such as `--debug` and `--trace` are not registered, because jq's flag set takes precedence. The `graft jq` form accepts them.

## Related

- [jq](jq.md) describes the command that the `jq` alias runs.
