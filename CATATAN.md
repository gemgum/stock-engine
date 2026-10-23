# Catatan — 24 Oktober 2026

Kode selesai, diuji, di-commit, di-push, dan **sudah dideploy ke VPS.**

## Keadaan sekarang

| | |
|---|---|
| Git | sejajar dengan `origin/main` (`6ba8879 Arahkan koleksi Postman ke alamat prod`) |
| Uji | 42 uji Go lulus, `make lint` 0 issues |
| VPS | **menjalankan versi ini** — hash binary sama dengan hasil `make build` dari `HEAD` |
| Alamat | `https://stock.trengguliboys27.cloud` |
| Redis | jalan, `content.ready`, tiga grup pembaca integrator |

## Yang terjadi hari ini

**Redis ternyata sudah jalan sejak 18 Oktober.** Dugaan "belum naik ke prod"
salah. Yang membuatnya terlihat diam: stream cuma berisi konten yang **sudah
waktunya**, dan unggahan paling cepat dijadwalkan 1 jam ke depan — jadi sesudah
unggah, stream memang kosong minimal sejam. Untuk tes cepat, pindahkan jadwalnya
ke sekarang lewat `POST /v1/contents/{id}/schedule`.

Redis cuma bisa dijangkau dari dalam VPS (`127.0.0.1:6379`). Integrator yang
jalan di luar VPS tidak bisa membaca stream langsung.

**`platform` tidak lagi teks bebas** (`c1987fb`). Sebelumnya salah ketik —
`Instagram`, `instgram` — tetap bisa diunggah tapi diam-diam hilang dari
`?platform=instagram`. Sekarang:

- `platform` **wajib** saat unggah, dan harus nama aktif di daftar platform.
  Nama lain ditolak 400.
- `?platform=` dengan nama yang tidak terdaftar ditolak 400, bukan daftar kosong.
- Alamat baru:

| Alamat | Siapa |
|---|---|
| `GET /v1/platforms` | semua kunci |
| `POST /v1/platforms` `{"name":"…"}` — daftar, atau aktifkan lagi yang diarsipkan | kunci induk |
| `POST /v1/platforms/{name}/archival` — tutup untuk unggahan baru | kunci induk |

Tidak ada `DELETE`, sesuai `ARCHITECTURE.md` §4. Migrasi `0004_platforms.sql`
mendaftarkan `instagram` dan `tiktok`.

Usulan "apps = platform" ditolak: satu aplikasi bisa memposting ke beberapa
platform, dan mengikat keduanya sama dengan memecah gudang per integrator.

**Data konten di VPS dibersihkan.** 10 konten dan 10 berkas dihapus (di
antaranya platform salah tulis `tiktok-apaan`, `tiktok-tester`), stream
dikosongkan dengan `XTRIM` supaya grup pembaca integrator tetap ada. Admin key
dan 4 kunci aplikasi **tidak** disentuh. Cadangan sebelum dibersihkan:
`/var/backups/stock-engine/20261023T232352Z`.

**Koleksi Postman** (`api/stock-engine.postman_collection.json`) siap dikirim
ke integrator: `base_url` ke alamat prod, folder Platform baru, contoh unggahan
menyebut `platform` wajib.

## Hasil deploy

Dari laptop lewat SSH — kuncinya sudah terpasang. Urutannya: cadangan
(`20261023T230111Z`), binary lama disimpan sebagai `stock-engine.lama`, binary
baru dipasang, restart. Pemeriksaan sesudahnya:

```
healthz              200 Service is running.
GET /v1/platforms    instagram, tiktok
?platform=instgram   400 Unknown platform 'instgram'. See GET /v1/platforms.
```

## Yang masih terbuka

| | Kapan layak |
|---|---|
| **Kabari integrator** | Sekarang. `platform` jadi wajib — unggahan tanpa itu mulai ditolak |
| Cron pencadangan harian di VPS | **Sekarang — masih belum ada** (`crontab -l` kosong per 24 Okt). Barisnya di `README.md` bagian Deploy |
| Pemantauan disk VPS | Sekarang, kalau mau. Disk dipakai bersama `sas-compro` serta `trengguli` |
| Menyalin cadangan keluar dari VPS | Saat isinya mulai berharga |
| Jenis event `uploaded` di stream | Kalau ada integrator yang benar-benar butuh kabar sebelum jadwal tiba. Untuk melihat seluruh stok, pakai `GET /v1/contents?scope=all` |
| Panel web | Repo terpisah. Belum ada menu ganti jadwal — sementara lewat `/schedule` di Postman |
| `client-errors`, thumbnail, OpenAPI | Alasannya di `ARCHITECTURE.md` §9 |
