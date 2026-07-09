#!/usr/bin/env bash
set -euo pipefail

# release.sh — build multi-platform k5pw binaries and create a Git tag.
# Usage: ./scripts/release.sh [VERSION]
# Example: ./scripts/release.sh 1.2.0

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
SRC_DIR="${REPO_ROOT}/src"
DIST_DIR="${REPO_ROOT}/dist"
BUILD_DIR="${REPO_ROOT}/build"

VERSION="${1:-}"
if [[ "${VERSION}" == "-h" || "${VERSION}" == "--help" ]]; then
  echo "Usage: $0 <version>"
  echo "Example: $0 1.2.0"
  exit 0
fi
if [[ -z "${VERSION}" ]]; then
  echo "Usage: $0 <version>" >&2
  echo "Example: $0 1.2.0" >&2
  exit 1
fi

# Validate version format (x.y.z or vx.y.z)
if [[ ! "${VERSION}" =~ ^v?[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  echo "error: version must be semver like 1.2.0 or v1.2.0" >&2
  exit 1
fi

# Normalize to "v" prefix for the tag
TAG_VERSION="${VERSION#v}"
TAG="v${TAG_VERSION}"

COMMIT="$(git -C "${REPO_ROOT}" rev-parse --short HEAD 2>/dev/null || echo unknown)"
BUILD_TIME="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

LDFLAGS="-s -w \
  -X main.version=${TAG_VERSION} \
  -X main.commit=${COMMIT} \
  -X main.buildTime=${BUILD_TIME}"

echo "Building k5pw ${TAG} (commit ${COMMIT}, built ${BUILD_TIME})"

mkdir -p "${BUILD_DIR}"

PLATFORMS=(
  "linux/amd64"
  "linux/arm64"
  "linux/386"
  "linux/arm/v7"
  "darwin/amd64"
  "darwin/arm64"
  "windows/amd64"
  "windows/386"
  "freebsd/amd64"
)

for platform in "${PLATFORMS[@]}"; do
  IFS='/' read -r GOOS GOARCH GOARM <<< "${platform}"
  GOARM="${GOARM:-}"
  output_name="k5pw-${TAG_VERSION}-${GOOS}-${GOARCH}"
  if [[ "${GOARCH}" == "arm" && "${GOARM}" == "v7" ]]; then
    output_name="k5pw-${TAG_VERSION}-${GOOS}-armv7"
  fi
  if [[ "${GOOS}" == "windows" ]]; then
    output_name="${output_name}.exe"
  fi

  echo "Building ${output_name}..."
  (
    cd "${SRC_DIR}"
    env GOOS="${GOOS}" GOARCH="${GOARCH}" GOARM="${GOARM}" \
      go build -trimpath -ldflags "${LDFLAGS}" -o "${BUILD_DIR}/${output_name}" .
  )
done

echo "Release artifacts written to ${BUILD_DIR}"

# Create Git tag if it does not already exist
if git -C "${REPO_ROOT}" rev-parse "${TAG}" >/dev/null 2>&1; then
  echo "Tag ${TAG} already exists."
else
  echo "Creating Git tag ${TAG}..."
  git -C "${REPO_ROOT}" tag -a "${TAG}" -m "Release ${TAG}"
  echo "Push the tag with: git push origin ${TAG}"
fi

echo "Done."
