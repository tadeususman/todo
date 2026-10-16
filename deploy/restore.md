# Restore database

1. Hentikan app agar tidak menulis saat restore: `docker compose stop app`
2. Pulihkan (ganti nama file):
   ```
   gunzip -c ~/todo-backups/todo-YYYYMMDD-HHMMSS.sql.gz | docker exec -i todo-db-1 psql -U todo -d todo -v ON_ERROR_STOP=1
   ```
3. Jalankan lagi: `docker compose start app`

Uji restore ke database sementara sebelum mengandalkan backup:
```
docker exec todo-db-1 createdb -U todo todo_restore_test
gunzip -c FILE | docker exec -i todo-db-1 psql -U todo -d todo_restore_test -v ON_ERROR_STOP=1 -q
docker exec todo-db-1 psql -U todo -d todo_restore_test -c 'select count(*) from users'
docker exec todo-db-1 dropdb -U todo todo_restore_test
```
Backup berisi password hash dan isi chat: simpan di tempat terbatas (file dibuat mode 600).
