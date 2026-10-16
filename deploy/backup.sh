#!/usr/bin/env bash
# Backup database todo ke file .sql.gz dengan rotasi.
# Pakai:   deploy/backup.sh [direktori-tujuan]   (default: ~/todo-backups)
# Env:     KEEP_DAYS (default 14), DB_CONTAINER (default todo-db-1)
# Jadwal:  crontab -e  →  15 2 * * * /path/ke/todo/deploy/backup.sh >> ~/todo-backups/backup.log 2>&1
# Restore: lihat deploy/restore.md
set -euo pipefail

DEST="${1:-$HOME/todo-backups}"
KEEP_DAYS="${KEEP_DAYS:-14}"
DB_CONTAINER="${DB_CONTAINER:-todo-db-1}"

umask 077
mkdir -p "$DEST"
out="$DEST/todo-$(date +%Y%m%d-%H%M%S).sql.gz"
tmp="$out.partial"
trap 'rm -f "$tmp"' EXIT

docker exec "$DB_CONTAINER" pg_dump -U todo --no-owner --clean --if-exists todo | gzip -9 > "$tmp"

# File kosong/rusak = backup gagal; jangan gantikan yang lama.
if ! gzip -t "$tmp" || [ "$(gzip -dc "$tmp" | wc -c)" -lt 1000 ]; then
  echo "backup gagal: hasil dump tidak valid" >&2
  exit 1
fi
mv "$tmp" "$out"
trap - EXIT
echo "$(date -Is) ok $out ($(du -h "$out" | cut -f1))"

find "$DEST" -name 'todo-*.sql.gz' -mtime +"$KEEP_DAYS" -print -delete
