#!/bin/sh
set -eu

repo="${WHAT_TTFT_REPO:-gabrielmbmb/what-ttft}"
binary="what-ttft"
version="${VERSION:-latest}"
install_dir="${INSTALL_DIR:-/usr/local/bin}"

err() {
  printf 'what-ttft install: %s\n' "$*" >&2
  exit 1
}

need() {
  command -v "$1" >/dev/null 2>&1 || err "missing required command: $1"
}

fetch() {
  url="$1"
  output="$2"
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL "$url" -o "$output"
    return
  fi
  if command -v wget >/dev/null 2>&1; then
    wget -qO "$output" "$url"
    return
  fi
  err "missing required command: curl or wget"
}

need uname
need tar
need install

os=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$os" in
  linux) ;;
  *) err "unsupported OS: $os" ;;
esac

arch=$(uname -m)
case "$arch" in
  x86_64 | amd64) arch="amd64" ;;
  aarch64 | arm64) arch="arm64" ;;
  *) err "unsupported architecture: $arch" ;;
esac

if [ "$version" = "latest" ]; then
  tmp_latest=$(mktemp)
  fetch "https://api.github.com/repos/$repo/releases/latest" "$tmp_latest"
  version=$(sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "$tmp_latest" | head -n 1)
  rm -f "$tmp_latest"
  [ -n "$version" ] || err "could not determine latest release version"
fi

case "$version" in
  v*) release_tag="$version" ;;
  *) release_tag="v$version" ;;
esac

version_without_v=${release_tag#v}
asset="${binary}_${version_without_v}_${os}_${arch}.tar.gz"
base_url="https://github.com/$repo/releases/download/$release_tag"

tmp_dir=$(mktemp -d)
trap 'rm -rf "$tmp_dir"' EXIT INT TERM

fetch "$base_url/$asset" "$tmp_dir/$asset"
fetch "$base_url/checksums.txt" "$tmp_dir/checksums.txt"

if command -v sha256sum >/dev/null 2>&1; then
  checksum_line=$(grep -F "  $asset" "$tmp_dir/checksums.txt" || true)
  [ -n "$checksum_line" ] || err "checksums.txt did not contain $asset"
  printf '%s\n' "$checksum_line" | (cd "$tmp_dir" && sha256sum -c -) >/dev/null
else
  printf 'what-ttft install: sha256sum not found; skipping checksum verification\n' >&2
fi

tar -xzf "$tmp_dir/$asset" -C "$tmp_dir"
[ -f "$tmp_dir/$binary" ] || err "archive did not contain $binary"

if [ ! -d "$install_dir" ]; then
  if command -v sudo >/dev/null 2>&1; then
    sudo mkdir -p "$install_dir"
  else
    mkdir -p "$install_dir"
  fi
fi

if [ -w "$install_dir" ]; then
  install -m 0755 "$tmp_dir/$binary" "$install_dir/$binary"
elif command -v sudo >/dev/null 2>&1; then
  sudo install -m 0755 "$tmp_dir/$binary" "$install_dir/$binary"
else
  err "$install_dir is not writable and sudo is unavailable; set INSTALL_DIR to a writable directory"
fi

printf 'what-ttft %s installed to %s/%s\n' "$release_tag" "$install_dir" "$binary"
