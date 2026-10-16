#!/usr/bin/env bash
# Pemeriksaan kesehatan todo + kesegaran backup. Jalankan tiap menit lewat cron.
# Mengirim notifikasi hanya saat status BERUBAH (mati → pulih), bukan tiap menit.
#
# Env:
#   HEALTH_URL     default http://127.0.0.1:8083/healthz
#   FAILS_BEFORE   gagal berturut-turut sebelum alert (default 3)
#   BACKUP_DIR     default ~/todo-backups ; BACKUP_MAX_AGE_H default 26 (kosongkan BACKUP_DIR check dengan BACKUP_MAX_AGE_H=0)
#   NOTIFY_URL     URL tujuan notifikasi. Isi pesan dikirim sebagai body POST (cocok untuk ntfy.sh/topik-rahasia-mu
#                  atau server ntfy sendiri). Kosong = hanya tulis ke log; tidak ada yang dikirim keluar.
#   STATE_DIR      default ~/.cache/todo-monitor
# Cron:  * * * * * /path/ke/todo/deploy/monitor.sh >> ~/todo-backups/monitor.log 2>&1
set -uo pipefail

HEALTH_URL="${HEALTH_URL:-http://127.0.0.1:8083/healthz}"
FAILS_BEFORE="${FAILS_BEFORE:-3}"
BACKUP_DIR="${BACKUP_DIR:-$HOME/todo-backups}"
BACKUP_MAX_AGE_H="${BACKUP_MAX_AGE_H:-26}"
STATE_DIR="${STATE_DIR:-$HOME/.cache/todo-monitor}"
mkdir -p "$STATE_DIR"

notify() {
  echo "$(date -Is) ALERT: $1"
  if [ -n "${NOTIFY_URL:-}" ]; then
    curl -fsS -m 10 -H "Title: todo" -d "$1" "$NOTIFY_URL" >/dev/null || echo "$(date -Is) gagal mengirim notifikasi" >&2
  fi
}

# check NAME OK(0/1) FAIL_MESSAGE RECOVER_MESSAGE
# Alert saat gagal >= FAILS_BEFORE kali berturut-turut; kabar pulih saat kembali normal.
check() {
  local name="$1" ok="$2" failmsg="$3" okmsg="$4"
  local cf="$STATE_DIR/$name.fails" af="$STATE_DIR/$name.alerted"
  local fails; fails=$(cat "$cf" 2>/dev/null || echo 0)
  if [ "$ok" = 1 ]; then
    echo 0 > "$cf"
    if [ -f "$af" ]; then rm -f "$af"; notify "$okmsg"; fi
  else
    fails=$((fails + 1)); echo "$fails" > "$cf"
    if [ "$fails" -ge "$FAILS_BEFORE" ] && [ ! -f "$af" ]; then touch "$af"; notify "$failmsg"; fi
  fi
}

# 1. App + database
if [ "$(curl -s -o /dev/null -m 5 -w '%{http_code}' "$HEALTH_URL")" = 200 ]; then ok=1; else ok=0; fi
check app "$ok" "todo TIDAK merespons ($HEALTH_URL)" "todo pulih"

# 2. Backup terakhir tidak terlalu lama
if [ "$BACKUP_MAX_AGE_H" != 0 ]; then
  latest=$(find "$BACKUP_DIR" -name 'todo-*.sql.gz' -mmin -$((BACKUP_MAX_AGE_H * 60)) 2>/dev/null | head -n1)
  if [ -n "$latest" ]; then ok=1; else ok=0; fi
  check backup "$ok" "Backup todo terakhir lebih tua dari ${BACKUP_MAX_AGE_H} jam (atau belum ada) di $BACKUP_DIR" "Backup todo kembali berjalan"
fi
