#!/usr/bin/env bash
set -euo pipefail
ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd -P)
cd "$ROOT"
# Verification fixtures provide their own data, identities and local providers.
for name in ${!BLOG_@}; do unset "$name"; done
export CGO_ENABLED=1
go test -gcflags='all=-l -N' -count=1 ./...
go test -race -gcflags='all=-l -N' -count=1 ./...
go vet ./...
mkdir -p bin/verify
go build -o bin/verify/blog-server ./cmd/blog
echo "Automated checks passed. Record actual C/B page evidence separately in docs/local-verification.md."
