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

### Pengaturan

Semuanya lewat environment; `.env` cuma dibaca kalau ada, dan **yang sudah ada di
environment selalu menang.** Itu yang membuat berkas `.env` yang tidak sengaja
ikut ke server tidak bisa diam-diam mengembalikan pengaturan lama.

| Variabel | Bawaan | Guna |
|---|---|---|
| `ADMIN_KEY` | — **wajib** | kunci induk, minimal 32 karakter. Servis menolak menyala tanpa ini |
| `STORAGE_DIR` | `storage` | satu folder berisi `stock.db` dan `files/` |
| `ADDRESS` | `:8080` | di server isi `127.0.0.1:8099` supaya cuma nginx yang bisa masuk |
| `MAX_UPLOAD_MB` | `200` | batas satu berkas |
| `RETENTION_DAYS` | `90` | umur berkas konten yang sudah selesai. `0` = jangan pernah dibuang |
| `CLAIM_TIMEOUT_HOURS` | `2` | ambang melepas claim yang ditinggalkan. `0` = jangan pernah dilepas |
| `TRUSTED_PROXIES` | `127.0.0.0/8,::1/128` | dari mana header alamat asli dipercaya |
| `ALLOWED_ORIGINS` | `http://localhost:3000` | alamat panel web yang boleh memanggil |
| `REDIS_ADDR` | kosong | Redis tempat konten yang sudah waktunya dikabarkan, misalnya `127.0.0.1:6379`. Kosong = tidak dipakai. Lihat **Integrasi lewat Redis Stream** |
| `REDIS_PASSWORD` | kosong | diisi kalau Redis-nya memakai `requirepass` |
| `REDIS_STREAM` | `content.ready` | nama stream-nya |

Kalau `ADMIN_KEY` kurang dari 32 karakter, servis **berhenti saat start** dan
menyebut namanya — bukan jalan setengah lalu menolak permintaan di lapangan.

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
| `GET /v1/platforms` | nama platform yang sah |
| `POST /v1/platforms` · `POST /v1/platforms/{name}/archival` | daftarkan (`{"name":"youtube"}`, juga mengaktifkan lagi yang diarsipkan) · arsipkan. Kunci induk saja |
| `GET /v1/me` | siapa saya |
| `GET /healthz` | terbuka, ikut memeriksa database |

| `scope=` | Isinya |
|---|---|
| `all` (bawaan) | semuanya |
| `unused` | belum tayang dan belum dibatalkan — **stok yang masih punya giliran** |
| `due` | sudah waktunya dan belum diambil siapa pun — **antrean kerja integrator** |
| `overdue` | jadwalnya lewat tapi tidak pernah tayang — **yang terlewat** |
| `scheduled` | menunggu jadwalnya |
| `claimed` | sedang dipegang satu aplikasi |
| `published` | sudah tayang |
| `failed` | gagal tayang, alasannya ada di `failure_reason` |
| `archived` | dibatalkan |

Bisa digabung dengan `?platform=`, `?limit=`, dan `?offset=`.

Balasannya satu halaman, bukan larik telanjang:

```json
{
  "items": [ ... ],
  "total": 128,
  "limit": 50,
  "offset": 0
}
```

`total` jumlah **seluruh** yang cocok dengan penyaringnya, bukan yang ada di
halaman ini. Itu yang dipakai menghitung jumlah halaman — jumlah baris yang
diterima tidak cukup, karena halaman yang kebetulan pas penuh tidak bisa
dibedakan dari halaman yang masih ada sambungannya.

`limit` dan `offset` dipantulkan kembali karena servis membetulkan nilai yang
tidak masuk akal: `limit=9999` dilayani sebagai 50.

Nilai lain ditolak dengan menyebut yang ada — daftar kosong untuk permintaan yang
salah ketik adalah jenis salah yang tidak ketahuan sampai ada yang membandingkan
angkanya.

**`due` dan `overdue` menjawab pertanyaan yang berbeda.** `due` antrean kerja:
sengaja tidak memuat konten yang sedang dipegang aplikasi lain, supaya tidak ada
yang tayang dua kali. `overdue` pertanyaan manusia — "kenapa konten ini tidak
muncul?" — jadi yang macet di tangan integrator dan yang gagal justru ikut
kelihatan.

### Bentuk satu konten

```json
{
  "id": "c6afafa86fe41cb916d82bc4",
  "filename": "poster.jpg",
  "mime": "image/jpeg",
  "size": 184320,
  "checksum": "ad0df4bb262e87b5...",
  "caption": "Diskon akhir pekan",
  "platform": "instagram",
  "scheduled_at": "2026-09-13 09:00:00",
  "status": "scheduled",
  "claimed_by": null,
  "claimed_at": null,
  "published_at": null,
  "failure_reason": null,
  "archived_at": null,
  "file_removed_at": null,
  "streamed_at": null,
  "uploaded_by": "aecbce013903ed3cd3e7a932",
  "created_at": "2026-09-11 22:37:42"
}
```

| Field | Isinya |
|---|---|
| `id` | penanda acak, bukan nomor urut — dipakai di semua alamat |
| `filename` · `mime` · `size` | keterangan berkasnya. Nama aslinya tidak dipakai sebagai nama berkas di disk |
| `checksum` | SHA-256 berkasnya. Cocokkan setelah mengunduh kalau kamu tidak mau mempercayai jaringan |
| `caption` | teks bebas yang kamu isi saat mengunggah. Servis tidak menafsirkannya |
| `platform` | salah satu nama di `GET /v1/platforms` |
| `status` | lihat **Perjalanan satu konten** di bawah |
| `claimed_by` · `claimed_at` | aplikasi yang sedang memegangnya |
| `published_at` | terisi saat dilaporkan tayang |
| `failure_reason` | alasan yang **dilaporkan integrator**, bukan karangan servis. Kosong kalau kegagalannya tidak pernah dilaporkan siapa pun |
| `archived_at` | terisi saat dibatalkan lewat `/archival` |
| `file_removed_at` | terisi kalau berkasnya sudah dibuang karena umur. Barisnya tetap; berkasnya tidak bisa diunduh lagi |
| `streamed_at` | terisi saat konten dikabarkan ke Redis Stream, kosong lagi begitu ia kembali ke antrean. Selalu kosong kalau Redis tidak dipakai |
| `uploaded_by` | aplikasi yang mengunggahnya. Catatan saja — gudangnya satu, semua aplikasi melihat semuanya |

### Mengunggah

Multipart, bukan JSON berisi base64: base64 membengkakkan video 200MB jadi 267MB.

| Field | Wajib | Isinya |
|---|---|---|
| `file` | ya | berkasnya |
| `scheduled_at` | ya | `YYYY-MM-DD HH:MM:SS`, waktu server, **paling cepat 1 jam dari saat diunggah**. `/schedule` tidak terikat batas ini |
| `caption` | tidak | teks bebas |
| `platform` | ya | salah satu nama aktif di `GET /v1/platforms`, huruf kecil persis. Nama lain ditolak — `?platform=` dengan nama yang tidak terdaftar juga |

### Waktu

Satu bentuk saja, dan itu **waktu server**:

```
2026-09-13 09:00:00
```

Tanpa zona waktu, tanpa huruf `T`. Bentuk yang dikirim, yang tersimpan, dan yang
dibalas sama persis — tidak ada konversi yang perlu kamu pikirkan. Bentuk lain
(termasuk RFC3339 `2026-09-13T09:00:00+07:00`) **ditolak**, bukan
diterjemahkan diam-diam: jadwal yang salah tafsir baru ketahuan saat kontennya
tayang di jam yang keliru.

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

Koleksi Postman siap pakai ada di `api/stock-engine.postman_collection.json` —
import, lalu isi `base_url`, `admin_key`, dan `app_key` di tab **Variables**.
Folder **Alur integrator** berisi empat langkah di atas, berurutan.

**Langkah 2 bukan formalitas.** Tanpa claim, dua aplikasi yang menanyakan
antrean di detik yang sama akan memposting konten yang sama, dan tidak ada galat
yang muncul — yang melapor pembacanya.

Yang ditegakkan engine: **cuma aplikasi pemegang claim yang boleh melapor.**
Yang belum mengclaim dijawab `400`; yang mengabaikan `409` lalu memposting
sendiri tetap ditolak saat melapor, dengan `409 already_claimed`.

**Mengunduh berkas tidak butuh claim**, dan itu disengaja: mengunduh tidak
mengubah apa pun, boleh diulang kalau koneksinya putus, dan panel perlu
menampilkan pratinjau tanpa mengklaim. Yang dijaga bukan siapa boleh melihat
berkasnya, melainkan siapa boleh menyatakan konten ini sudah dikerjakan.

---

## Integrasi lewat Redis Stream

Pilihan kedua selain `?scope=due`, untuk integrator yang berjalan **di VPS yang
sama**. Servis yang mengabarkan konten yang sudah waktunya; integrator tidak
perlu bertanya berkala, dan berkasnya dibaca langsung dari disk.

Nyalakan dengan mengisi `REDIS_ADDR`. Kosong berarti tidak ada yang dikabarkan,
dan sisa servis tetap sama.

**Tiap menit**, servis mengirim satu event untuk setiap konten yang sudah
waktunya dan belum diambil — isinya sama persis dengan `?scope=due`. Satu konten
dikabarkan sekali, dan dikabarkan **lagi** kalau ia kembali ke antrean: dipindah
jadwalnya lewat `/schedule`, atau claimnya dilepas karena ditinggalkan.

### Isi satu event

| Field | Isinya |
|---|---|
| `id` | id konten — dipakai untuk claim dan lapor |
| `filename` · `mime` · `size` · `checksum` | keterangan berkasnya, sama dengan di API |
| `caption` · `platform` | sama dengan di API |
| `scheduled_at` | jadwalnya, waktu server |
| `path` | letak berkasnya di disk, path mutlak. Namanya cuma id, tanpa ekstensi — jenisnya dilihat dari `mime` |

### Yang dikerjakan integrator

```
# sekali saja: buat consumer group. "0" supaya event yang sudah ada ikut terbaca
XGROUP CREATE content.ready tiktok 0 MKSTREAM

# tunggu event — terpicu begitu ada, tanpa bertanya berkala
XREADGROUP GROUP tiktok worker-1 BLOCK 0 STREAMS content.ready >
```

Lalu untuk tiap event:

1. **Claim lewat API**: `POST /v1/contents/{id}/claim`. Dijawab `409` berarti
   lewati.
2. **XACK** event-nya — berhasil claim maupun `409`.
3. **Baca berkasnya** dari `path`, **sesudah** claim. Cocokkan dengan `checksum`
   kalau mau yakin utuh.
4. Unggah ke platformnya.
5. **Lapor hasilnya lewat API**: `POST /v1/contents/{id}/publication`.

Jadi API tetap dipanggil **dua kali per konten** — claim dan lapor. Keduanya
tidak bisa dilewati: claim yang mencegah konten tayang dua kali, dan cuma
pemegang claim yang boleh melapor.

### Yang perlu diketahui

**Event bisa datang dua kali.** Kalau servis mati tepat sesudah mengirim, event
yang sama dikirim lagi. Claim kedua dijawab `409` — lewati saja.

**Event bisa basi.** Konten yang sudah dikabarkan lalu dipindah ke besok masih
punya event lamanya, dan claim-nya tetap berhasil. Karena itu **pakai
`scheduled_at` dari jawaban claim, bukan dari event.** Kalau ternyata masih di
masa depan, jangan diposting — biarkan saja: claimnya dilepas setelah
`CLAIM_TIMEOUT_HOURS`, dan kontennya dikabarkan lagi saat jadwal barunya tiba.

**XACK segera sesudah claim, bukan sesudah lapor.** Worker yang mati di tengah
tidak perlu mengulang event-nya: claimnya dilepas setelah
`CLAIM_TIMEOUT_HOURS`, dan kontennya dikabarkan ulang sebagai event baru.
Mengulang event lama tidak menolong — claim kedua dari aplikasi yang sama tetap
`409`.

**Redis yang mati tidak menghilangkan apa pun.** Konten yang belum sempat
dikabarkan tidak ditandai, dan dikirim begitu Redis kembali. Sementara itu ia
tetap ada di `?scope=due`. Galatnya tercatat di log servis tiap menit.

**Stream dipangkas sekitar 10.000 event.** Yang terpangkas sebelum dibaca tetap
ada di `?scope=due`.

### Izin membaca berkas

Berkas konten ditulis dengan izin `0640`: pemiliknya user servis, **grupnya
boleh membaca**, yang lain tidak. User aplikasi integrator harus masuk grup itu:

```bash
usermod -aG stock-engine www-data     # sesuaikan nama grup servis dan user integrator
```

Berkas yang diunggah sebelum rilis ini masih `0600` — buka sekali:

```bash
chmod 640 /var/lib/stock-engine/files/*
```

Yang dibuka cuma `files/`. Database jangan ikut terbaca grup itu:

```bash
chmod 600 /var/lib/stock-engine/stock.db*
```

Berkasnya cuma untuk dibaca. Integrator tidak pernah menulis, memindahkan, atau
menghapus apa pun di folder itu.

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

## Batas yang perlu diketahui integrator

| Batas | Angkanya | Kalau dilewati |
|---|---|---|
| Permintaan per menit | 60, **per alamat IP** | `429 too_many_requests` — tunggu, jangan ubah apa pun |
| Ukuran satu berkas | `MAX_UPLOAD_MB`, bawaan 200MB | `400` dari servis, atau `413` dari nginx kalau batasnya di sana lebih kecil |
| Isi satu halaman daftar | `limit` maksimal 200 | dibetulkan diam-diam jadi 50, dan nilai betulannya dipantulkan |
| Badan JSON | 1MB | `400 invalid_input` |

Jatah 60/menit dihitung **per pemanggil**, bukan per kunci. Kalau engine ini di
balik nginx, nginx wajib meneruskan alamat asli (`X-Real-IP`) — tanpa itu semua
integrator berbagi satu jatah, dan satu aplikasi yang beruntun mematikan yang
lain.

Untuk `?scope=due` yang dipanggil berkala, sekali tiap beberapa menit sudah
cukup. Menanyakan tiap detik tidak membuat konten tayang lebih tepat waktu —
jadwalnya tetap jadwal.

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

## Empat pekerjaan yang jalan sendiri

Tidak ada alamat untuk keduanya, dan tidak ada tombol. Kalau menghapus dan
membereskan harus diingat manusia, disknya tetap penuh dan kontennya tetap
tersangkut.

**Konten yang sudah waktunya dikabarkan ke Redis Stream**, tiap menit, kalau
`REDIS_ADDR` diisi. Rinciannya di **Integrasi lewat Redis Stream**.

**Claim yang ditinggalkan dilepas** setelah `CLAIM_TIMEOUT_HOURS` (bawaan 2
jam), diperiksa tiap 5 menit. Integrator yang mati sesudah mengclaim membuat
kontennya diam di `claimed` selamanya — tidak pernah tayang, dan tidak pernah
muncul lagi di antrean kerja. Jadwalnya tidak diubah, jadi begitu dilepas ia
langsung masuk antrean lagi.

Ambangnya jangan dipendekkan tanpa alasan: integrator yang **masih** mengunggah
video besar di jaringan pelan akan direbut aplikasi lain, dan kontennya tayang
dua kali.

**Potongan unggahan yang ditinggalkan dibuang** setelah 6 jam, sekali sehari.
Servis yang mati **mendadak** — OOM, crash, listrik padam — meninggalkan berkas
separuh yang tidak punya baris di database; tidak ada sapuan lain yang bisa
menemukannya. Berhenti normal tidak menghasilkan ini. Sapuan ini **tidak** ikut
dimatikan `RETENTION_DAYS=0`: itu sampah, bukan kebijakan penyimpanan.

**Berkas yang sudah selesai dibuang** setelah `RETENTION_DAYS` (bawaan 90 hari),
diperiksa sekali sehari. Yang dibuang cuma berkasnya:

```
published / archived  ──lewat 90 hari──→  berkas dibuang, baris tetap
```

Riwayatnya tetap terbaca, dengan `file_removed_at` terisi. Mengunduhnya dijawab
`404` yang menyebut retensi, dan menjadwalkannya ulang ditolak — kalau lolos, ia
masuk antrean lagi lalu gagal diunduh selamanya.

**Stok yang belum pernah tayang tidak pernah disapu**, berapa lama pun sudah
diunggah.

Keduanya bisa dimatikan dengan mengisi `0`, dan keduanya jalan sekali saat
servis menyala.

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

**nginx wajib meneruskan alamat asli pemanggil** (`proxy_set_header X-Real-IP
$remote_addr;`). Tanpa itu servis melihat `127.0.0.1` untuk semua permintaan,
dan seluruh integrator berbagi satu jatah pembatas laju — satu aplikasi yang
menembak beruntun mematikan yang lain.

**Pencadangan bukan opsional, dan bukan cuma databasenya.** Database yang utuh
tanpa berkasnya adalah daftar konten yang semuanya rusak. `make backup` menyalin
keduanya sekaligus; jalankan lewat cron:

```
0 2 * * * STORAGE_DIR=/var/lib/stock-engine BACKUP_DIR=/var/backups/stock-engine bash /opt/stock-engine/ops/backup.sh >> /var/log/stock-engine-backup.log 2>&1
```

Memulihkannya — **servis harus dimatikan dulu**:

```bash
systemctl stop stock-engine
STORAGE_DIR=/var/lib/stock-engine bash ops/restore.sh /var/backups/stock-engine/20260911T190608Z
systemctl start stock-engine
```

Yang ada sekarang tidak ditimpa, tapi dipindahkan ke `storage.sebelum-restore-…`
— restore yang ternyata salah pilih cadangan masih bisa dibatalkan.

Cadangan yang belum pernah dicoba bukan cadangan. Dan cadangan di disk yang sama
dengan aslinya juga bukan: kalau VPS-nya hilang, keduanya hilang bersamaan.

---

## Kalau ada yang salah

Bentuk jawabannya sendiri sudah menyebut salahnya di mana:

- **teks polos** `404 page not found` → alamatnya salah ketik
- **amplop JSON** → alamatnya benar, isinya yang bermasalah

| Yang terlihat | Sebabnya biasanya |
|---|---|
| `401 unauthorized` | kunci belum diisi, salah, atau sudah dicabut. Ketiganya dijawab sama — membedakannya cuma berguna bagi yang menebak-nebak |
| `409 already_claimed` | konten sedang dipegang aplikasi lain, sudah tayang, atau sudah dibatalkan. Lewati, ambil berikutnya — ini bukan kegagalan |
| `400 invalid_input` | kiriman yang tidak bisa diproses. Mengulang yang sama akan gagal lagi; `message` menyebut sebab persisnya |
| `413` tanpa amplop | ditolak nginx sebelum sampai ke servis. Naikkan `client_max_body_size` |
| `429` | terlalu sering memanggil. Tunggu, jangan ubah apa pun |
| `500 database_error` / `server_error` | salah kami, layak dicoba lagi nanti. Sebab aslinya ada di log servis, tidak pernah dikirim keluar |

Membaca log servis di server:

```bash
journalctl -u stock-engine -n 50 --no-pager     # terakhir
journalctl -u stock-engine -f                   # ikuti langsung
journalctl -u stock-engine -p err --no-pager    # yang perlu dilihat orang saja
```

Tiap baris JSON. Yang `"msg":"request"` adalah lalu lintas biasa; yang
`"msg":"operasi gagal"` menyebut **fungsi mana** yang gagal dan atas nama siapa.
Isi request tidak pernah ikut — yang lewat servis ini berkas konten dan kunci
API, dan log adalah tempat paling gampang untuk membocorkan keduanya tanpa
sadar.

Servis menolak menyala? Sebabnya disebut di baris pertama:

```bash
systemctl status stock-engine --no-pager
```

Kalau berbunyi `Start request repeated too quickly`, systemd sudah menyerah —
perbaiki sebabnya lalu `systemctl reset-failed stock-engine` sebelum
menyalakannya lagi.

---

## Batas yang sudah diketahui

| Batas | Kapan jadi soal |
|---|---|
| SQLite: satu proses yang menulis | Saat servisnya berjalan lebih dari satu salinan. Saat itu juga pembatas laju berhenti benar — keduanya jatuh tempo bersamaan |
| Berkas di disk servis | Saat `storage/` melewati kapasitas VPS. Itu satu-satunya alasan sah pindah ke S3/R2 |
| Tidak ada peringatan sebelum disk habis | Retensi membatasi konten lama, bukan kecepatan konten baru masuk. Itu pekerjaan pemantauan VPS |
