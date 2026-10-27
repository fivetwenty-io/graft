# Aliases

Graft looks at the name it was run under, and it behaves as a different tool when that name is `jq`, `yq`, or `spruce`.

## Why aliases exist

Scripts and tools call `jq`, `yq`, and `spruce` by name. When graft runs under one of those names, those callers get graft without any change to the script. Genesis relies on this for `spruce`, and `jq` and `yq` work the same way through `graft jq` and `graft yq`.

The [jq page](jq.md) and the [yq page](yq.md) describe which names dispatch and what changes under each name.

## Linking by hand

On Linux and macOS, a symbolic link is enough.

```sh
ln -s "$(command -v graft)" /usr/local/bin/jq
ln -s "$(command -v graft)" /usr/local/bin/yq
ln -s "$(command -v graft)" /usr/local/bin/spruce
```

Then `jq '.name' file.yml` runs `graft jq '.name' file.yml`, `yq '.name' file.yml` runs `graft yq '.name' file.yml`, and `spruce merge a.yml b.yml` runs `graft merge a.yml b.yml`.

## graft install-aliases

The `graft install-aliases` command makes the links for us.

```sh
graft install-aliases
graft install-aliases --dir /usr/local/bin
graft install-aliases --dry-run
graft install-aliases jq
```

With no names, it installs `jq`, `yq`, and `spruce`. Name one or more aliases to install only those. The links go in the directory that holds graft unless `--dir` says otherwise. They point at the path we ran graft by, as the shell found it on `PATH`, so on a Homebrew install they land beside the `graft` link in the Homebrew `bin` directory and keep working after an upgrade.

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
would link yq -> /usr/local/bin/graft at /usr/local/bin/yq
would link spruce -> /usr/local/bin/graft at /usr/local/bin/spruce
would link 3, already installed 0, skipped 0
```

## Windows

Windows links need privileges that many accounts lack, so `graft install-aliases` installs copies named `jq.exe`, `yq.exe`, and `spruce.exe` in place of links. A copy does not follow a graft upgrade, so we run `graft install-aliases --force` again after upgrading.

## Removing aliases

```sh
graft install-aliases --remove
```

This removes only links to this graft and copies of it. A file with the same name that belongs to some other tool stays where it is.

## Coexisting with spruce, jq, and yq

A link named `jq` or `yq` shadows a real jq or yq that sits elsewhere on `PATH`, and which one wins depends on the order of the directories. If a script behaves differently after we install the alias, `command -v jq` or `command -v yq` shows which one runs. `--version` under either alias prints the original tool's version line first, then graft's own, so the two are easy to tell apart.

When we run it from `PATH`, the `spruce` alias leads its version output with the exact line spruce prints, which is what the Genesis version gate reads.

```sh
$ spruce --version
spruce - Version X.Y.Z
graft version X.Y.Z (commit: ..., built: ..., go: ..., os/arch: ...)
```

Under the `jq` and `yq` names, graft's own persistent flags such as `--debug` and `--trace` are not registered, because the original tool's flag set takes precedence. The `graft jq` and `graft yq` forms accept them.

## Related

- [jq](jq.md) describes the command that the `jq` alias runs.

- [yq](yq.md) describes the command that the `yq` alias runs.
