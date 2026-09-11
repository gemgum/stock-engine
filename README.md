# Stock Engine

Gudang konten social media beserta jadwalnya. Satu servis Go, satu folder
penyimpanan, tanpa database server.

Servis ini **tidak memposting apa pun**. Ia menyimpan konten, menjaga jadwalnya,
dan memberi tahu aplikasi integrator mana yang sudah waktunya tayang. Yang
memposting aplikasi mereka.

Aturan yang berlaku di repo ini ada di [`ARCHITECTURE.md`](ARCHITECTURE.md) —
baca sebelum menambah berkas.

---

## Menjalankan

```bash
cp .env.example .env
make key            # tempel hasilnya ke ADMIN_KEY di .env
make dev            # servis menyala di :8080
```

Yang dibuat sendiri saat pertama jalan: folder `storage/`, berkas
`storage/stock.db`, dan skemanya. Tidak ada langkah migrasi terpisah.

| Perintah | Guna |
|---|---|
| `make dev` | jalankan servis |
| `make test` | uji seluruh servis di atas SQLite sekali pakai |
| `make lint` | gofmt, go vet, golangci-lint |
| `make build` | rakit binary untuk VPS |
| `make backup` | salin seluruh isi `storage/` |

---

## Kunci

Dua tingkat.

**Kunci induk** (`ADMIN_KEY`, dari environment) mendaftarkan dan mencabut
aplikasi. Ia juga bisa menyentuh konten, supaya gudang bisa diisi sebelum ada
satu pun integrator.

**Kunci aplikasi** diterbitkan lewat `POST /v1/apps`, dan **cuma terlihat satu
kali** — yang disimpan cuma hashnya. Kalau hilang, aplikasinya dicabut lalu
didaftarkan lagi.

Keduanya dikirim sebagai `Authorization: Bearer <kunci>`.

```bash
# daftarkan satu aplikasi integrator
curl -X POST localhost:8080/v1/apps \
  -H "Authorization: Bearer $ADMIN_KEY" \
  -H 'Content-Type: application/json' \
  -d '{"name":"poster-instagram"}'
```

---

## Alamat

Semua butuh `Authorization: Bearer <kunci>`. Seluruh perubahan pakai `POST` —
tidak ada `PATCH`, `PUT`, atau `DELETE`; alasannya di `ARCHITECTURE.md` §4.

| Alamat | Guna |
|---|---|
| `POST /v1/contents` | unggah (multipart: `file`, `scheduled_at`, `caption`, `platform`) |
| `GET /v1/contents` | daftar; `?scope=`, `?platform=`, `?limit=`, `?offset=` |
| `GET /v1/contents/{id}` | satu konten |
| `GET /v1/contents/{id}/file` | berkasnya |
| `POST /v1/contents/{id}/schedule` | pindahkan jadwal, sekaligus kembalikan ke antrean |
| `POST /v1/contents/{id}/claim` | kunci sebelum memposting |
| `POST /v1/contents/{id}/publication` | lapor hasilnya |
| `POST /v1/contents/{id}/archival` | keluarkan dari antrean |
| `GET /v1/apps` · `POST /v1/apps` · `POST /v1/apps/{id}/revocation` | kunci induk saja |
| `GET /v1/me` | siapa saya |
| `GET /healthz` | terbuka, ikut memeriksa database |

`scope` yang dikenal: `due`, `scheduled`, `claimed`, `published`, `failed`,
`archived`, `all` (bawaan). Nilai lain ditolak dengan menyebut yang ada.

---

## Yang dikerjakan integrator

Empat langkah, dan urutannya tidak boleh dibalik.

```bash
K="Authorization: Bearer $APP_KEY"

# 1. tanya mana yang sudah waktunya — jangan hitung jadwalnya sendiri
curl -H "$K" "localhost:8080/v1/contents?scope=due&platform=instagram"

# 2. kunci dulu. 409 already_claimed berarti aplikasi lain sudah mengambilnya —
#    lewati, ambil yang berikutnya
curl -X POST -H "$K" localhost:8080/v1/contents/<id>/claim

# 3. ambil berkasnya
curl -H "$K" -o konten.jpg localhost:8080/v1/contents/<id>/file

# 4. lapor hasilnya. Gagal wajib menyebut alasan
curl -X POST -H "$K" -H 'Content-Type: application/json' \
  -d '{"published":true}' localhost:8080/v1/contents/<id>/publication
```

**Langkah 2 bukan formalitas.** Tanpa claim, dua aplikasi yang menanyakan
antrean di detik yang sama akan memposting konten yang sama, dan tidak ada galat
yang muncul — yang melapor pembacanya.

---

## Bentuk jawaban

Setiap balasan, berhasil maupun gagal, punya empat field yang sama:

```json
{ "code": 200, "status": "success", "message": "Contents loaded.", "data": [] }
{ "code": 409, "status": "already_claimed", "message": "This content is already claimed by another app.", "data": null }
```

**Pilih tindakan dari `status`, bukan dari `code`.** Golongan yang ada:
`success`, `invalid_input`, `unauthorized`, `forbidden`, `not_found`,
`already_claimed`, `too_many_requests`, `database_error`, `server_error`.

Dua yang terakhir layak dicoba lagi nanti. `invalid_input` tidak — mengulang
kiriman yang sama akan gagal lagi.

---

## Perjalanan satu konten

```
scheduled ──claim──→ claimed ──publication──→ published
    ↑                                     └─→ failed
    └────────────── schedule ─────────────────┘
```

Yang gagal **tidak** kembali sendiri ke antrean. Konten yang ditolak platform
biasanya ditolak lagi kalau dikirim ulang apa adanya, dan antrean yang mengisi
ulang dirinya sendiri akan mencoba selamanya tanpa ada yang tahu. Yang
mengembalikannya manusia, lewat `/schedule`.

---

## Deploy

```bash
make build
scp bin/stock-engine vps:/opt/stock-engine/
```

```ini
# /etc/systemd/system/stock-engine.service
[Service]
Environment=ADMIN_KEY=ganti-ini STORAGE_DIR=/var/lib/stock-engine
ExecStart=/opt/stock-engine/stock-engine
Restart=always
[Install]
WantedBy=multi-user.target
```

Taruh di belakang nginx atau caddy untuk TLS, dan **naikkan batas ukuran badan
request di sana** — bawaan nginx 1MB, dan unggahan video akan ditolak sebelum
sampai ke servis ini.

**Pencadangan bukan opsional, dan bukan cuma databasenya.** Database yang utuh
tanpa berkasnya adalah daftar konten yang semuanya rusak. `make backup` menyalin
keduanya sekaligus; jalankan lewat cron.

---

## Batas yang sudah diketahui

| Batas | Kapan jadi soal |
|---|---|
| SQLite: satu proses yang menulis | Saat servisnya berjalan lebih dari satu salinan. Saat itu juga pembatas laju berhenti benar — keduanya jatuh tempo bersamaan |
| Berkas di disk servis | Saat `storage/` melewati kapasitas VPS. Itu satu-satunya alasan sah pindah ke S3/R2 |
| Berkas yang diarsipkan tidak ikut dibuang | Saat disk menipis. Butuh pekerjaan sapuan tersendiri |
