#!/usr/bin/env bash
set -euo pipefail

REPO="1024XEngineer/neo-code"
DEFAULT_FLAVOR="full"

flavor="$DEFAULT_FLAVOR"
dry_run=0

usage() {
  cat <<'USAGE'
Usage: install.sh [--flavor full|gateway] [--dry-run]

Options:
  --flavor   Install artifact flavor. Default: full
  --dry-run  Print resolved asset URLs/checksum URL and exit without installing
  -h, --help Show this help message
USAGE
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --flavor)
      if [[ $# -lt 2 ]]; then
        echo "Error: --flavor requires a value" >&2
        exit 1
      fi
      flavor="$(echo "$2" | tr '[:upper:]' '[:lower:]')"
      shift 2
      ;;
    --dry-run)
      dry_run=1
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "Error: unknown argument: $1" >&2
      usage
      exit 1
      ;;
  esac
done

case "$flavor" in
  full)
    asset_prefix="neocode"
    binary_name="neocode"
    ;;
  gateway)
    asset_prefix="neocode-gateway"
    binary_name="neocode-gateway"
    ;;
  *)
    echo "Error: unsupported --flavor value: $flavor (expected full|gateway)" >&2
    exit 1
    ;;
esac

os="$(uname -s)"
arch="$(uname -m)"

case "$os" in
  Linux) os_name="Linux" ;;
  Darwin) os_name="Darwin" ;;
  *)
    echo "Error: unsupported operating system: $os" >&2
    exit 1
    ;;
esac

case "$arch" in
  x86_64|amd64) arch_name="x86_64" ;;
  aarch64|arm64) arch_name="arm64" ;;
  *)
    echo "Error: unsupported architecture: $arch" >&2
    exit 1
    ;;
esac

if [[ -n "${NEOCODE_INSTALL_LATEST_TAG:-}" ]]; then
  latest_tag="${NEOCODE_INSTALL_LATEST_TAG}"
else
  echo "Resolving latest release metadata..."
  latest_tag="$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" | sed -n 's/.*"tag_name":[[:space:]]*"\([^"]*\)".*/\1/p' | head -n1)"
  if [[ -z "$latest_tag" ]]; then
    echo "Error: failed to resolve latest release tag from GitHub API" >&2
    exit 1
  fi
fi

asset_name="${asset_prefix}_${os_name}_${arch_name}.tar.gz"
download_url="https://github.com/${REPO}/releases/download/${latest_tag}/${asset_name}"
checksum_url="https://github.com/${REPO}/releases/download/${latest_tag}/checksums.txt"

if [[ "$dry_run" -eq 1 ]]; then
  echo "flavor=${flavor}"
  echo "asset=${asset_name}"
  echo "download_url=${download_url}"
  echo "checksum_url=${checksum_url}"
  exit 0
fi

temp_dir="$(mktemp -d)"
cleanup() {
  rm -rf "${temp_dir}"
}
trap cleanup EXIT

archive_path="${temp_dir}/${asset_name}"
checksums_path="${temp_dir}/checksums.txt"

echo "Downloading ${asset_name}..."
curl -fsSL -o "${archive_path}" "${download_url}"

echo "Downloading checksums..."
curl -fsSL -o "${checksums_path}" "${checksum_url}"

expected_checksum="$(awk -v asset="${asset_name}" '$2 == asset || $2 == "*"asset { print $1; exit }' "${checksums_path}")"
if [[ -z "${expected_checksum}" ]]; then
  echo "Error: failed to find checksum entry for ${asset_name}" >&2
  exit 1
fi

if command -v sha256sum >/dev/null 2>&1; then
  actual_checksum="$(sha256sum "${archive_path}" | awk '{print $1}')"
elif command -v shasum >/dev/null 2>&1; then
  actual_checksum="$(shasum -a 256 "${archive_path}" | awk '{print $1}')"
else
  echo "Error: sha256sum/shasum is required to verify checksums" >&2
  exit 1
fi

if [[ "${actual_checksum}" != "${expected_checksum}" ]]; then
  echo "Error: checksum verification failed for ${asset_name}" >&2
  echo "Expected: ${expected_checksum}" >&2
  echo "Actual:   ${actual_checksum}" >&2
  exit 1
fi

echo "Extracting ${binary_name}..."
tar -xzf "${archive_path}" -C "${temp_dir}" "${binary_name}"

echo "Installing ${binary_name} to /usr/local/bin (sudo may prompt)..."
sudo mv "${temp_dir}/${binary_name}" /usr/local/bin/

echo "Installed ${binary_name} (${flavor}) from ${latest_tag}."
