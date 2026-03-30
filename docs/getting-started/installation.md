# Installation

This guide covers all the ways to install Graft on your system.

## Requirements

- **Go 1.26+** (for building from source or `go install`)
- **Linux**, **macOS**, or **Windows**
- **amd64** or **arm64** architecture

## Installation Methods

### Using Go Install (Recommended)

If you have Go installed, this is the quickest method:

```sh
go install github.com/wayneeseguin/graft/cmd/graft@latest
```

Verify the installation:

```sh
graft --version
```

### Pre-built Binaries

Download pre-built binaries from the [releases page](https://github.com/wayneeseguin/graft/releases/).

#### Linux (amd64)

```sh
curl -L https://github.com/wayneeseguin/graft/releases/latest/download/graft-linux-amd64.tar.gz | tar xz
sudo mv graft /usr/local/bin/
```

#### Linux (arm64)

```sh
curl -L https://github.com/wayneeseguin/graft/releases/latest/download/graft-linux-arm64.tar.gz | tar xz
sudo mv graft /usr/local/bin/
```

#### macOS (Intel)

```sh
curl -L https://github.com/wayneeseguin/graft/releases/latest/download/graft-darwin-amd64.tar.gz | tar xz
sudo mv graft /usr/local/bin/
```

#### macOS (Apple Silicon)

```sh
curl -L https://github.com/wayneeseguin/graft/releases/latest/download/graft-darwin-arm64.tar.gz | tar xz
sudo mv graft /usr/local/bin/
```

#### Windows

Download `graft-windows-amd64.zip` from the [releases page](https://github.com/wayneeseguin/graft/releases/) and add to your PATH.

### Building from Source

Clone and build:

```sh
git clone https://github.com/wayneeseguin/graft.git
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

Run Graft in a container:

```sh
docker run --rm -v $(pwd):/data graft merge /data/base.yml /data/overlay.yml
```

Build your own image:

```dockerfile
FROM golang:1.26-alpine AS builder
RUN go install github.com/wayneeseguin/graft/cmd/graft@latest

FROM alpine:latest
COPY --from=builder /go/bin/graft /usr/local/bin/
ENTRYPOINT ["graft"]
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
go install github.com/wayneeseguin/graft/cmd/graft@latest
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

Each release includes SHA256 checksums. Verify your download:

```sh
sha256sum -c graft-linux-amd64.tar.gz.sha256
```

## Next Steps

- [Quick Start Tutorial](quick-start.md) - Get started in 5 minutes
- [CLI Commands](../user-guide/cli/) - Learn the command line interface
