#!/usr/bin/env bash
# Salin seluruh keadaan servis: database dan berkas kontennya.
#
# Satu folder, satu perintah, dan itu memang inti dari memilih SQLite. Yang
# mencadangkan database terpisah dari berkasnya cepat atau lambat punya cadangan
# yang tidak cocok — dan itu baru ketahuan saat restore.
set -euo pipefail

STORAGE_DIR="${STORAGE_DIR:-storage}"
BACKUP_DIR="${BACKUP_DIR:-backups}"
STAMP="$(date -u +%Y%m%dT%H%M%SZ)"
TUJUAN="$BACKUP_DIR/$STAMP"

if [ ! -d "$STORAGE_DIR" ]; then
  echo "folder $STORAGE_DIR tidak ada" >&2
  exit 1
fi

mkdir -p "$TUJUAN"

# Database disalin lewat perintah SQLite sendiri, bukan cp.
#
# Menyalin berkas .db yang sedang dipakai bisa menangkapnya di tengah transaksi,
# dan hasilnya berkas yang kelihatan utuh tapi menolak dibuka. `.backup` menunggu
# titik yang aman.
if command -v sqlite3 >/dev/null; then
  sqlite3 "$STORAGE_DIR/stock.db" ".backup '$TUJUAN/stock.db'"
else
  echo "sqlite3 tidak terpasang — hentikan servis dulu sebelum menyalin" >&2
  cp "$STORAGE_DIR/stock.db" "$TUJUAN/stock.db"
fi

cp -r "$STORAGE_DIR/files" "$TUJUAN/files"

echo "cadangan selesai: $TUJUAN"
