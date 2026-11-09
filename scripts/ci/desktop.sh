#!/bin/bash
# The desktop app (desktop/, docs/desktop/desktop.md): format, vet, the unit and view tests (the views run headless
# against scripts/mock/mock_hub.py; DESKTOP_SHOTS=<dir> keeps their screenshots) and a build for every platform (MyGo
# needs no cgo, so one machine builds them all). Not yet a hosted CI lane.
set -euo pipefail
source "$(dirname "$0")/common.sh"
export GOTOOLCHAIN="${GOTOOLCHAIN:-auto}"
ci_run 'Desktop gofmt' 60 python3 scripts/ci/gofmt.py desktop
ci_run 'Desktop vet' 600 bash -c 'cd desktop && go vet ./...'
ci_run 'Desktop tests' 300 bash -c "cd desktop && PERSONA_DESKTOP_SHOTS='${DESKTOP_SHOTS:-}' go test ./internal/..."
for platform in darwin/arm64 darwin/amd64 windows/amd64 windows/arm64 linux/amd64 linux/arm64; do
  ci_run "Desktop build $platform" 600 bash -c "cd desktop && CGO_ENABLED=0 GOOS=${platform%/*} GOARCH=${platform#*/} go build -o /dev/null ."
done
