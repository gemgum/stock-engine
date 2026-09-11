#!/usr/bin/env bash
# Kembalikan satu cadangan. Servis harus dimatikan lebih dulu.
#
# Tidak menimpa diam-diam: yang ada sekarang dipindahkan, bukan dihapus. Restore
# yang ternyata salah pilih cadangan masih bisa dibatalkan.
set -euo pipefail

if [ $# -ne 1 ]; then
  echo "pemakaian: $0 backups/20260911T120000Z" >&2
  exit 1
fi

SUMBER="$1"
STORAGE_DIR="${STORAGE_DIR:-storage}"

[ -d "$SUMBER" ] || { echo "cadangan $SUMBER tidak ada" >&2; exit 1; }

if [ -d "$STORAGE_DIR" ]; then
  mv "$STORAGE_DIR" "$STORAGE_DIR.sebelum-restore-$(date -u +%Y%m%dT%H%M%SZ)"
fi

mkdir -p "$STORAGE_DIR"
cp "$SUMBER/stock.db" "$STORAGE_DIR/stock.db"
cp -r "$SUMBER/files" "$STORAGE_DIR/files"

echo "restore selesai dari $SUMBER"
