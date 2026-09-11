# Arsitektur — Stock Engine

Aturan yang berlaku di repo ini, beserta alasannya. Ditulis lebih dulu supaya
ada yang bisa ditunjuk saat sesuatu terasa janggal.

Kalau kode dan berkas ini berbeda, salah satunya harus diperbaiki. Jangan
dibiarkan.

Polanya diambil dari YourChoice — lapisan, amplop jawaban, bentuk alamat. Yang
**tidak** ikut dibawa dicatat di §3, supaya jelas itu keputusan, bukan kelupaan.

Status: **berlaku.** Terakhir diperbarui 11 September 2026.

---

## 1. Bentuk sistem

Satu servis Go, satu database. Panel webnya **repo terpisah**, bukan folder di
sini.

```
stock-engine/     servis Go — satu-satunya yang menyentuh database dan berkas
(repo lain)       panel web, memanggil servis ini seperti integrator lain
```

Panel web dipisah karena ia bukan bagian istimewa: ia memakai alamat yang sama
dengan aplikasi developer lain. Kalau ia tinggal serumah, cepat atau lambat akan
ada alamat yang "khusus panel" — dan sejak itu ada dua permukaan API yang harus
sama-sama benar.

Konsekuensinya harus diterima: **tidak ada tipe yang bisa dibagi** antara servis
dan panel. Sumber kebenarannya skema database, seperti di YourChoice.

---

## 2. Susunan folder

Servisnya langsung di akar repo, bukan di dalam `backend/`. Di YourChoice folder
itu ada karena bertetangga dengan `mobile/` dan `frontend/`; di sini tidak ada
tetangganya, dan satu lapis folder yang isinya cuma satu hal hanya menambah
panjang tiap path tanpa memisahkan apa pun.

```
CLAUDE.md          aturan kerja + status keputusan
README.md          cara menjalankan + panduan integrator
ARCHITECTURE.md    berkas ini

Makefile           satu pintu perintah
Dockerfile
.env.example
.golangci.yml

cmd/api/           titik masuk

pkg/               tidak tahu apa-apa tentang konten
  httpx/           amplop jawaban, golongan galat, pembungkus rute
  middleware/      recover, CORS, log, pembatas laju
  ratelimit/       ember token per pemanggil
  security/        terbitkan dan cocokkan kunci API

internal/          tahu ini engine konten
  api/             merakit semuanya jadi satu servis, dan ujinya
  routes/          alamat apa saja yang ada
  handlers/        baca HTTP, panggil satu method service, balas amplop
  services/        aturan main, plus penyimpanan berkas
  repository/      satu-satunya tempat yang menulis SQL
  models/          bentuk data + antarmuka Repository
  config/          baca pengaturan
  database/        koneksi + transaksi
  problem/         terjemahan penolakan database

migrations/        berkas SQL biasa, ikut masuk binary lewat go:embed

ops/               backup.sh, restore.sh
api/               koleksi Postman untuk integrator
storage/           stock.db + files/ — di .gitignore
```

Larangannya sama dengan YourChoice, dan tidak dinegosiasi ulang:

- **Tidak ada SQL di luar `internal/repository/`.**
- **`models` tidak mengimpor siapa pun.**
- **Tidak ada keputusan di `handlers/`**, dan tidak ada `http.ResponseWriter` di
  `services/`.
- **Handler tidak tahu alamatnya sendiri.** Yang mendaftarkan `routes/`.
- **`pkg` tidak pernah mengimpor `internal`.** Angka yang bersifat kebijakan —
  berapa permintaan per menit, berapa besar berkas boleh diunggah — tinggal di
  `internal/config`, dan `pkg` menerimanya sebagai parameter.

### Dua folder yang berbeda isi dari YourChoice

**`storage/` menggantikan `data/photos/`, dan sifatnya terbalik.** Di YourChoice
isinya selfie yang justru **harus** dibuang setelah 90 hari. Di sini isinya
konten yang harus awet, dan itu membalik seluruh rencana ops: foldernya tumbuh
terus, ukurannya yang menentukan ukuran VPS, dan **pencadangan tidak cukup
mencadangkan database saja**. Database yang utuh tanpa berkasnya adalah daftar
konten yang semuanya rusak.

**`test/` tidak ada, dan `dto/` belum ada.** Uji ikut di sebelah kode yang
diujinya — `internal/api/api_test.go` merakit servis lengkap di atas SQLite
sekali pakai, dan SQLite tidak butuh skrip penyiapan database seperti Postgres.
`dto/` sempat direncanakan karena pengunggah dan pengambil konten dua pihak
berbeda; ternyata keduanya membaca bentuk `Content` yang sama, dan **menyalin
tipe ke sana hanya supaya foldernya terisi** justru yang dilarang. Foldernya
dibuat saat benar-benar ada dua bentuk yang berbeda.

---

## 3. Yang sengaja tidak ikut dibawa dari YourChoice

| Tidak ada di sini | Alasan |
|---|---|
| `mobile/`, `frontend/` | Tidak ada aplikasi HP. Panel web repo terpisah |
| `supabase/` | Databasenya SQLite, jadi tidak ada CLI vendor yang mengatur bentuk foldernya. Migrasinya tetap berkas SQL biasa di `migrations/`, dan ikut masuk binary — lihat §6 |
| Row Level Security | Gudangnya satu: semua aplikasi integrator melihat konten yang sama, jadi tidak ada yang perlu disaring per pemanggil. Ditambah SQLite memang tidak punya RLS. **Kalau suatu hari tiap integrator cuma boleh melihat kontennya sendiri, itu kolom `app_id` di semua tabel — dan keputusan yang jauh lebih murah diambil sekarang daripada saat datanya sudah banyak** |
| `database.Run` yang menurunkan peran | Ikut hilang bersama RLS. Folder `database/` tetap ada — koneksi dan transaksi tetap perlu — tapi isinya lebih tipis, dan `Run` tidak lagi menerima id pemanggil |
| Fungsi PL/pgSQL | Tidak ada `clock()` atau yang setara — SQLite tidak punya padanannya. Aturan main tinggal di `services/`, tempat yang wajar untuk aturan yang tidak menopang anti-kecurangan |
| Pekerjaan yang jalan sendiri | Belum ada satu pun. Yang pertama kemungkinan besar sapuan berkas arsip saat disk menipis; tempatnya `internal/api/`, dan aturannya tiga: lewat service, berhenti kalau context dibatalkan, aman diulang berapa kali pun |

Yang **tetap** dibawa walau kelihatan bisa dibuang: `problem/`. Isinya lebih
tipis karena tidak ada galat PL/pgSQL buatan sendiri, tapi penerjemah penolakan
database tetap perlu — tanpa itu bunyi asli SQLite, lengkap dengan nama tabel
dan kolomnya, sampai ke tangan orang luar.

---

## 4. Bentuk alamat

Aturannya diambil utuh dari YourChoice §6, dan ditulis di sini supaya alamat ke
tiga belas nanti menyesuaikan aturan ini, bukan sebaliknya.

- **Jamak, selalu.** `/v1/contents`, `/v1/apps`.
- **`{id}` selalu milik sumber daya yang disebut alamatnya.**
- **Perubahan keadaan pakai kata benda, bukan kata kerja.** `/publication`,
  bukan `/publish`; `/revocation`, bukan `/revoke`.
- **Semua perubahan `POST`.** Tidak ada `PATCH`, `PUT`, atau `DELETE` — sistem
  ini tidak pernah menghapus apa pun, dan integrator yang mengirim ulang
  kirimannya yang gagal punya satu kata kerja lebih sedikit untuk salah.
- **Satu cara saja untuk "milik saya".** `/v1/me` menjawab "siapa saya";
  penyaringan kumpulan pakai query `?scope=`.
- **Kebab-case untuk nama majemuk.**

### Daftar alamat

Ditulis dengan satu asumsi yang harus dipegang: **engine ini gudang dan jadwal,
bukan yang memposting.** Aplikasi integrator yang memposting ke Instagram,
TikTok, dan seterusnya. Kalau suatu hari servis ini ikut memposting, yang
bertambah bukan satu alamat melainkan satu domain penuh beserta penyimpanan
token OAuth tiap platform.

**`contents` — konten dan jadwalnya**

| Alamat | Guna |
|---|---|
| `POST /v1/contents` | unggah berkas beserta jadwalnya |
| `GET /v1/contents` | daftar; `?scope=`, `?platform=`, `?limit=`, `?offset=` |
| `GET /v1/contents/{id}` | satu konten |
| `GET /v1/contents/{id}/file` | berkasnya |
| `POST /v1/contents/{id}/schedule` | pindahkan jadwal |
| `POST /v1/contents/{id}/claim` | kunci sebelum diposting |
| `POST /v1/contents/{id}/publication` | lapor sudah atau gagal diposting |
| `POST /v1/contents/{id}/archival` | keluarkan dari antrean |

**`apps` — aplikasi yang boleh memanggil**

| Alamat | Guna |
|---|---|
| `GET /v1/apps` | daftar integrator |
| `POST /v1/apps` | daftarkan; kuncinya keluar sekali saja, sesudah itu tinggal hashnya |
| `POST /v1/apps/{id}/revocation` | cabut kuncinya |

**Sisanya:** `GET /v1/me` dan `GET /healthz`.

### Kenapa `?scope=due` ada

Supaya integrator tidak menghitung "sudah waktunya" sendiri. Kalau tiap
integrator menghitungnya versi masing-masing, beda zona waktu dan jeda cron
membuat konten terlewat atau terkirim dua kali — dan **tidak ada yang mengeluh
sampai ada yang membandingkan angkanya.**

`scope` bukan hiasan: nilai yang tidak dikenal ditolak dengan menyebut apa yang
ada. Diam-diam mengembalikan daftar lain untuk permintaan yang meminta sesuatu
yang tidak dikenal adalah jenis salah yang tidak ketahuan sampai terlambat.

### Bentuk balasan daftar

`GET /v1/contents` membalas `{items, total, limit, offset}`, bukan larik
telanjang. `total` dihitung dengan penyaring yang **sama persis** dengan
isinya — satu penyusun `WHERE` dipakai berdua di
`repository.contentWhere`. Dua penyusun yang terpisah akan berbeda tipis cepat
atau lambat, dan yang terjadi bukan galat melainkan total yang bohong.

Keduanya juga diambil dalam **satu transaksi**. Kalau dipisah, unggahan yang
masuk di antara keduanya membuat jumlah halaman salah tanpa ada yang menyadari.

### Kenapa `due` dan `overdue` dua-duanya ada

Keduanya memang bertumpuk, tapi menjawab pertanyaan yang berbeda, dan
menggabungkannya jadi satu akan merusak salah satunya.

`due` adalah antrean kerja: **sengaja tidak memuat** konten yang sedang dipegang
aplikasi lain. `overdue` pertanyaan manusia — "kenapa konten ini tidak muncul?" —
jadi yang macet di tangan integrator dan yang gagal justru harus ikut kelihatan.

Kalau `overdue` dibuat sekadar alias `due`, konten yang tersangkut di satu
integrator yang mati tidak akan muncul di daftar mana pun, dan tidak ada yang
tahu sampai ada yang menghitung sendiri.

### Kenapa `claim` ada

Tanpa itu dua integrator bisa memposting konten yang sama. Ia cuma satu perintah
yang mengubah baris **kalau statusnya masih menunggu** — kecil, tapi gagalnya
senyap: tidak ada galat, cuma satu konten yang tayang dua kali dan baru
ketahuan dari pembaca.

### Yang belum ada, dan itu disengaja

| Belum ada | Kapan layak ditambah |
|---|---|
| `client-errors` | Saat ada developer kedua yang mengeluh dan tidak ada cara melihat apa yang dia lihat |
| `GET /v1/contents/{id}/thumbnail` | Saat ada panel yang menampilkan grid |
| OpenAPI sebagai kontrak | Saat integrator di luar kendalimu mulai menulis kliennya |


---

## 5. Bentuk jawaban

Setiap balasan, berhasil maupun gagal, punya empat field yang sama:

```json
{ "code": 200, "status": "success", "message": "Contents loaded.", "data": [] }
{ "code": 409, "status": "already_claimed", "message": "This content is already claimed by another app.", "data": null }
```

| Field | Isinya |
|---|---|
| `code` | Angka status HTTP, sama dengan yang ada di header |
| `status` | Golongannya. **Ini yang dipakai klien memilih tindakan**, dan yang menjawab "mengulang menolong atau tidak" |
| `message` | Kalimat Inggris yang menyebut sebab persisnya |
| `data` | Isinya. Selalu `null` kalau gagal |

Golongan umum: `success`, `invalid_input`, `unauthorized`, `forbidden`,
`not_found`, `database_error`, `server_error`, `too_many_requests`.

### Satu golongan yang ada karena kalimat umumnya menyesatkan

`already_claimed` (409). Ambangnya bukan "sebabnya berbeda" — hampir semua sebab
berbeda — melainkan: **apakah kalimat umumnya membuat orang melakukan hal yang
salah, atau berhenti padahal ada yang bisa dilakukan?**

Dengan `forbidden`, integrator membaca "tidak berhak", mengira kuncinya
bermasalah, lalu berhenti. Yang benar adalah melewati konten itu dan mengambil
berikutnya. Begitu ada dua worker, ini golongan yang paling sering dia temui.

**Ambangnya di sini lebih ketat daripada di YourChoice, bukan lebih longgar.**
Di sana golongan baru berarti menyunting tiga berkas di repo yang sama. Di sini
kliennya di luar kendali kita: golongan baru berarti semua integrator yang sudah
jalan harus diberi tahu, dan yang tidak diberi tahu akan memperlakukannya sebagai
galat yang tidak dikenal.

Kalau bertambah, tambah di `pkg/httpx/response.go` — dan umumkan.

### Galat aslinya tidak pernah keluar

Nama tabel, path berkas, dan bunyi penolakan SQLite berhenti di log, lewat
satu-satunya penulis galat: `httpx.Fail`. Levelnya dipilih dari golongannya —
4xx di Info, 5xx di Error — supaya penolakan yang terjadi setiap hari tidak
menenggelamkan kerusakan yang sesungguhnya.

---

## 6. Database: SQLite, dan itu keputusan

Alasannya bukan bebannya. Beberapa integrator yang menanyakan antrean tiap
beberapa menit tidak akan membuat database mana pun kewalahan.

Alasannya **pencadangan**. Berkas kontennya sudah pasti harus dicadangkan
sebagai folder, dan itu tidak bisa dihindari. Dengan SQLite seluruh keadaan
servis jadi satu folder yang disalin sekaligus. Dengan database server ada dua
mekanisme cadangan yang harus konsisten satu sama lain — dan yang paling mungkin
terjadi bukan keduanya gagal, melainkan `pg_dump` jalan jam 2 pagi sementara
rsync jalan jam 3, lalu ada konten yang tercatat tapi berkasnya belum ikut
tersalin. **Ketidakcocokan itu baru ketahuan saat restore**, yaitu saat paling
tidak enak untuk tahu.

Yang ikut dibeli: tidak ada database yang harus dirawat di VPS, dan `make test`
jalan tanpa Docker.

### Batasnya, dan kapan jatuh tempo

**Satu proses yang menulis.** Selama servisnya satu salinan itu bukan batasan
sama sekali — dengan WAL, pembaca tidak memblokir penulis. Ditegakkan di
`database.Open` lewat `SetMaxOpenConns(1)`: membiarkan pool membuka banyak
koneksi menukar antrean yang tertib dengan galat "database is locked" yang muncul
acak.

Saat servisnya berjalan lebih dari satu salinan, SQLite berhenti jadi pilihan
yang benar. **Pembatas laju punya batas yang sama persis** — ia menghitung di
memori satu proses — jadi keduanya jatuh tempo di hari yang sama, dan itu
memudahkan: satu keputusan, bukan dua.

Pindahnya tidak gratis, dialek SQL-nya berbeda. Tapi yang ditulis ulang cuma
`internal/repository/`, dan itu memang yang dijanjikan susunan foldernya.

### Waktu

Satu bentuk, `2026-09-13 09:00:00`, dan itu waktu server. Diputuskan di
`internal/models/time.go` lewat tipe `models.Time`, yang membawa bentuk
kirimannya sendiri — jadi tidak ada handler yang memformat waktunya sendiri lalu
berbeda tipis dari yang lain.

Disimpan apa adanya ke SQLite. Urutannya tetap benar saat dibandingkan sebagai
teks, yang berarti `scheduled_at <= ?` bekerja tanpa fungsi konversi.

**Harganya: zona waktu server jadi bagian dari data.** Mengubah zona VPS sesudah
ada isinya menggeser arti seluruh jadwal yang sudah tersimpan. Setel sekali saat
menyiapkan server, lalu jangan diubah — dan kalau suatu hari terpaksa diubah,
isinya harus ikut dikonversi lewat migrasi.

### Migrasi

Berkas SQL biasa di `migrations/`, ikut masuk binary lewat `go:embed`, dan
dijalankan saat servis start. Langkah migrasi terpisah adalah langkah yang bisa
terlupa, dan skema yang tertinggal satu kolom gagalnya tidak selalu langsung
terlihat.

Belum ada tabel versi: seluruh isinya `IF NOT EXISTS`, jadi aman diputar ulang
berapa kali pun. **`ALTER TABLE` yang pertama adalah tanda bahwa tabel versi
sudah harus ada.**

---

## 7. Penamaan

Sudah ada di `CLAUDE.md`, diulang di sini karena ini tempat orang mencarinya:

- Nama di dalam kode **bahasa Inggris** — berkas, tipe, fungsi, variabel,
  parameter, konstanta, kolom database, variabel skrip shell. **Termasuk nama
  uji**: `TestClaimWinsOnlyOnce`, bukan `TestClaimCumaMenangSekali`.
- Komentar dan dokumen **bahasa Indonesia**. Penjelasan perilaku yang tidak muat
  di nama uji ditulis sebagai komentar di atasnya.
- Field `message` di amplop jawaban **selalu Inggris**: yang membacanya developer
  integrator, yang belum tentu orang Indonesia.
- Cari fungsi yang sudah ada sebelum menulis yang baru.

---

## 8. Yang belum ada, dan itu disengaja

| Belum ada | Kapan layak ditambah |
|---|---|
| `dto/` | Saat pengunggah dan pengambil konten benar-benar membaca bentuk yang berbeda |
| OpenAPI sebagai kontrak | Saat integrator di luar kendalimu mulai menulis kliennya. Sementara ini koleksi Postman di `api/` yang menjelaskan |
| Sapuan berkas arsip | Saat `storage/` menipis. Yang diarsipkan sekarang tetap memakan disk, dan itu pilihan — arsip sering diubah pikiran, berkas yang terhapus tidak bisa dikembalikan |
| Pagination keyset | Saat daftarnya tumbuh sampai halaman belakang terasa lambat. Sekarang offset |
| Pembatas laju terdistribusi | Bersamaan dengan pindah dari SQLite — lihat §6 |

Jangan tambah komponen infrastruktur tanpa angka yang membuktikannya perlu.
