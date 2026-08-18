FROM golang:1.26.6 AS builder

# VERSION is empty on purpose: with no value the build stamps nothing and the
# binary reports cmd/graft/main.go's compiled-in default, so this file is not
# a third place the version has to be kept in sync. Release builds pass
# --build-arg VERSION=<tag> to match the Makefile's LDFLAGS.
ARG VERSION=

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build ${VERSION:+-ldflags=-X=main.Version=$VERSION} -o /out/graft ./cmd/graft

FROM scratch

COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=builder /out/graft /graft

# Numeric uid:gid (nobody): scratch has no /etc/passwd to resolve names
# against, and graft needs no privileges - it only reads the files and
# environment it is handed.
USER 65534:65534

ENTRYPOINT ["/graft"]
