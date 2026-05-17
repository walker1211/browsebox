#!/usr/bin/env bash
set -euo pipefail

go test ./...
go build -o browsebox ./cmd/browsebox
