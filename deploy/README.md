# Operasional

Pasang dua baris ini di `crontab -e` (ganti path):

```
15 2 * * *  /path/ke/todo/deploy/backup.sh  >> ~/todo-backups/backup.log 2>&1
* * * * *   NOTIFY_URL=https://ntfy.sh/topik-rahasia-mu /path/ke/todo/deploy/monitor.sh >> ~/todo-backups/monitor.log 2>&1
```

- `backup.sh`: dump database harian, simpan 14 hari. Restore: [restore.md](restore.md).
- `monitor.sh`: memeriksa `/healthz` dan kesegaran backup tiap menit; alert hanya saat status berubah (butuh 3 kegagalan berturut-turut). `NOTIFY_URL` opsional: tanpa itu hanya menulis ke log, tidak ada yang dikirim keluar. Dengan ntfy.sh, pesan lewat server publik mereka, jadi pakai nama topik yang sulit ditebak atau jalankan server ntfy sendiri.
- Monitor di mesin yang sama tidak menangkap mati total server; untuk itu tambahkan pemeriksa eksternal (layanan uptime) ke URL publik `/healthz`.
