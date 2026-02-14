#!/usr/bin/env bash
set -euo pipefail

TAG="${1:-}"
if [[ -z "${TAG}" ]]; then
  echo "Usage: ./release.sh <tag>"
  echo "Example: ./release.sh v0.1.0"
  exit 1
fi

for cmd in git gh /usr/local/go/bin/go; do
  if ! command -v "${cmd}" >/dev/null 2>&1; then
    echo "Missing required command: ${cmd}"
    exit 1
  fi
done

if ! git diff --quiet || ! git diff --cached --quiet; then
  echo "Working tree is not clean. Commit or stash changes before releasing."
  exit 1
fi

mkdir -p dist
rm -f dist/*

targets=(
  "darwin amd64"
  "darwin arm64"
  "linux amd64"
  "linux arm64"
  "windows amd64 .exe"
)

echo "Building binaries for ${TAG}..."
for target in "${targets[@]}"; do
  read -r goos goarch ext <<<"${target}"
  ext="${ext:-}"
  out="dist/cf-${goos}-${goarch}${ext}"
  echo "  - ${out}"
  GOOS="${goos}" GOARCH="${goarch}" CGO_ENABLED=0 /usr/local/go/bin/go build -o "${out}" ./cmd/cf
done

if ! git rev-parse -q --verify "refs/tags/${TAG}" >/dev/null; then
  echo "Creating tag ${TAG}..."
  git tag "${TAG}"
fi

echo "Pushing main + tag..."
git push origin main
git push origin "${TAG}"

if gh release view "${TAG}" >/dev/null 2>&1; then
  echo "Release ${TAG} exists. Uploading assets (clobber enabled)..."
  gh release upload "${TAG}" dist/* --clobber
else
  echo "Creating release ${TAG}..."
  gh release create "${TAG}" dist/* --title "${TAG}" --notes "Release ${TAG}"
fi

echo "Done: ${TAG}"
