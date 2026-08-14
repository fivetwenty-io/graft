# Installation

This guide covers all the ways to install Graft on your system.

## Requirements

- **Go 1.26+** (for building from source or `go install`)
- **Linux**, **macOS**, or **FreeBSD** on **amd64** or **arm64**, or
  **Windows** on **amd64**

## Installation Methods

### Homebrew (Recommended on macOS and Linux)

```sh
brew trust fivetwenty-io/tap &&
  brew install --cask fivetwenty-io/tap/graft
```

Installs the `graft` binary and shell completions from the
[fivetwenty-io tap](https://github.com/fivetwenty-io/homebrew-tap). The
macOS binaries are signed and notarized.

### Using Go Install

If you have Go installed, this is the quickest method:

```sh
go install github.com/fivetwenty-io/graft/cmd/graft@latest
```

Verify the installation:

```sh
graft --version
```

### Pre-built Binaries

Download pre-built binaries from the [releases page](https://github.com/fivetwenty-io/graft/releases/).

Release assets are named `graft_<version>_<os>_<arch>`, and the version is part
of both the tag and the filename. The examples below use `GRAFT_VERSION`; set it
to the release you want. Debian and RPM packages are also attached to each
release.

#### Linux (amd64)

```sh
GRAFT_VERSION=1.31.1
curl -L https://github.com/fivetwenty-io/graft/releases/download/v${GRAFT_VERSION}/graft_${GRAFT_VERSION}_linux_amd64.tar.gz | tar xz
sudo mv graft /usr/local/bin/
```

#### Linux (arm64)

```sh
GRAFT_VERSION=1.31.1
curl -L https://github.com/fivetwenty-io/graft/releases/download/v${GRAFT_VERSION}/graft_${GRAFT_VERSION}_linux_arm64.tar.gz | tar xz
sudo mv graft /usr/local/bin/
```

#### macOS (Intel)

```sh
GRAFT_VERSION=1.31.1
curl -L https://github.com/fivetwenty-io/graft/releases/download/v${GRAFT_VERSION}/graft_${GRAFT_VERSION}_darwin_amd64.tar.gz | tar xz
sudo mv graft /usr/local/bin/
```

#### macOS (Apple Silicon)

```sh
GRAFT_VERSION=1.31.1
curl -L https://github.com/fivetwenty-io/graft/releases/download/v${GRAFT_VERSION}/graft_${GRAFT_VERSION}_darwin_arm64.tar.gz | tar xz
sudo mv graft /usr/local/bin/
```

#### Windows (amd64)

Download `graft_<version>_windows_amd64.zip` from the
[releases page](https://github.com/fivetwenty-io/graft/releases/), extract
`graft.exe`, and add its directory to your PATH.

### Building from Source

Clone and build:

```sh
git clone https://github.com/fivetwenty-io/graft.git
cd graft
make build
```

Install to your PATH:

```sh
sudo make install
```

Or copy the binary manually:

```sh
sudo cp graft /usr/local/bin/
```

### Using Docker

No image is published to a registry. The repository ships a [Dockerfile](../../Dockerfile)
that builds a `scratch`-based image with the `graft` binary as its entrypoint, so
build it yourself from a clone:

```sh
git clone https://github.com/fivetwenty-io/graft.git
cd graft
docker build -t graft .
```

Then run it against a mounted directory:

```sh
docker run --rm -v $(pwd):/data graft merge /data/base.yml /data/overlay.yml
```

## Verifying Installation

After installation, verify Graft is working:

```sh
# Check version
graft --version

# Run a simple merge
echo 'key: value' | graft merge /dev/stdin
```

## Shell Completion

### Bash

```sh
graft completion bash > /etc/bash_completion.d/graft
```

### Zsh

```sh
graft completion zsh > "${fpath[1]}/_graft"
```

### Fish

```sh
graft completion fish > ~/.config/fish/completions/graft.fish
```

## Upgrading

### Using Go

```sh
go install github.com/fivetwenty-io/graft/cmd/graft@latest
```

### Using Pre-built Binaries

Download the latest release and replace the existing binary.

### From Source

```sh
cd graft
git pull
make build
sudo make install
```

## Uninstalling

Remove the binary:

```sh
sudo rm /usr/local/bin/graft
```

If installed via Go:

```sh
rm $(go env GOPATH)/bin/graft
```

## Troubleshooting

### "command not found"

Ensure the installation directory is in your PATH:

```sh
export PATH=$PATH:/usr/local/bin
# or for Go install
export PATH=$PATH:$(go env GOPATH)/bin
```

### Permission Denied

On Unix systems, ensure the binary is executable:

```sh
chmod +x /usr/local/bin/graft
```

### Verifying Checksums

Each release includes a single `graft_<version>_SHA256SUMS` file covering
every artifact in that release. Download it alongside the archive and verify:

```sh
GRAFT_VERSION=1.31.1
curl -LO https://github.com/fivetwenty-io/graft/releases/download/v${GRAFT_VERSION}/graft_${GRAFT_VERSION}_SHA256SUMS
sha256sum --ignore-missing -c graft_${GRAFT_VERSION}_SHA256SUMS
```

On macOS, use `shasum -a 256 --ignore-missing -c` instead.

## Next Steps

- [Quick Start Tutorial](quick-start.md) - Get started in 5 minutes
- [CLI Commands](../user-guide/cli/) - Learn the command line interface
