#!/usr/bin/env bash
set -euo pipefail

root_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
mkdir -p "$root_dir/bin"

go build -o "$root_dir/bin/tping" "$root_dir"
printf 'Built %s\n' "$root_dir/bin/tping"
