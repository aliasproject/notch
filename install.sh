#!/bin/sh
# Installs the latest notch release from GitHub for the current OS/arch.
set -e

repo="aliasproject/notch"
bin="notch"

os=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$os" in
  linux) os="linux" ;;
  darwin) os="darwin" ;;
  *) echo "notch: unsupported OS: $os" >&2; exit 1 ;;
esac

arch=$(uname -m)
case "$arch" in
  x86_64|amd64) arch="amd64" ;;
  arm64|aarch64) arch="arm64" ;;
  *) echo "notch: unsupported architecture: $arch" >&2; exit 1 ;;
esac

tag=$(curl -fsSL "https://api.github.com/repos/$repo/releases/latest" | grep '"tag_name":' | head -1 | sed -E 's/.*"tag_name": *"([^"]+)".*/\1/')
if [ -z "$tag" ]; then
  echo "notch: could not determine the latest release" >&2
  exit 1
fi

archive="${bin}_${os}_${arch}.tar.gz"
url="https://github.com/$repo/releases/download/$tag/$archive"

tmpdir=$(mktemp -d)
trap 'rm -rf "$tmpdir"' EXIT

echo "Downloading notch $tag for $os/$arch..."
curl -fsSL "$url" -o "$tmpdir/$archive"
tar -xzf "$tmpdir/$archive" -C "$tmpdir"

install_dir="/usr/local/bin"
if [ ! -w "$install_dir" ]; then
  install_dir="$HOME/.local/bin"
  mkdir -p "$install_dir"
fi

mv "$tmpdir/$bin" "$install_dir/$bin"
chmod +x "$install_dir/$bin"

echo "Installed notch to $install_dir/$bin"
case ":$PATH:" in
  *":$install_dir:"*) ;;
  *) echo "Note: $install_dir is not on your PATH. Add it with: export PATH=\"$install_dir:\$PATH\"" ;;
esac
