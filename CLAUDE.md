# Stock Engine — gudang konten social media

Dikerjakan **solo**. Satu servis Go, satu folder penyimpanan, tanpa database
server. Kliennya aplikasi milik developer lain.

## Dokumen

| Berkas | Isi |
|---|---|
| `README.md` | cara menjalankan, dan panduan untuk developer integrator |
| `ARCHITECTURE.md` | **Aturan struktur kode** — lapisan, susunan folder, bentuk jawaban, bentuk alamat. Baca sebelum menambah berkas |

## Status keputusan

| | |
|---|---|
| Backend | **Go, pustaka bawaan + `net/http`. Tanpa framework** |
| Database | **SQLite** (`modernc.org/sqlite`, Go murni). Alasannya pencadangan, bukan beban — `ARCHITECTURE.md` §6 |
| Migrasi | Berkas SQL di `migrations/`, ikut masuk binary, dicatat di `schema_migrations`. Nomor berkas menentukan urutan |
| Penyimpanan berkas | Disk servis, satu folder bersama databasenya |
| Auth | Kunci API per aplikasi, disimpan ter-hash. Kunci induk dari environment |
| Kepemilikan konten | **Satu gudang bersama.** Semua aplikasi melihat isi yang sama |
| Lingkup | **Gudang dan jadwal saja.** Servis ini tidak memposting apa pun |
| Panel web | Repo terpisah, memakai alamat yang sama dengan integrator lain |

**Servis ini tidak memposting ke Instagram, TikTok, atau platform mana pun.**
Jangan tawarkan integrasi platform, token OAuth, atau antrean kirim. Kalau suatu
hari berubah, itu domain baru dan rilis tersendiri — bukan tambahan di
`services/content.go`.

**SQLite bukan kelalaian.** Batasnya sudah diketahui dan ditulis: satu proses
yang menulis. Jangan tawarkan Postgres sebelum servisnya benar-benar berjalan
lebih dari satu salinan.

**Gudangnya satu.** Tidak ada kolom pemilik, tidak ada penyaringan per pemanggil,
tidak ada RLS. Kalau nanti tiap integrator cuma boleh melihat kontennya sendiri,
itu keputusan besar yang diambil sadar — bukan disisipkan sambil mengerjakan hal
lain.

## Aturan penulisan kode

**Semua nama di dalam kode ditulis bahasa Inggris** — nama berkas, tipe, fungsi,
variabel, parameter, konstanta, kolom database, variabel di skrip shell.

Alasannya: nama yang campur dua bahasa (`namaAman`, `batasHalaman`) bikin ragu
tiap kali mengetik. Kode ikut kebiasaan bahasanya, penjelasan ikut bahasa yang
menulisnya.

**Komentar dan seluruh dokumen di repo ini ditulis bahasa Indonesia.** Yang di
dalam kode tidak ada pengecualian — **termasuk nama uji**
(`TestClaimWinsOnlyOnce`, bukan `TestClaimCumaMenangSekali`). Nama uji memang
berbunyi seperti kalimat, tapi ia tetap nama fungsi, dan satu pengecualian
berarti satu hal lagi yang harus diingat tiap kali mengetik.

Kalau perilakunya butuh dijelaskan lebih panjang daripada yang muat di nama
fungsi, tempatnya komentar di atas ujinya — dan itu bahasa Indonesia.

**Teks yang sampai ke pemanggil selalu bahasa Inggris.** Field `message` di
amplop jawaban dibaca developer integrator yang belum tentu orang Indonesia.
Galat internal yang cuma masuk log boleh Indonesia.

**Cari dulu sebelum menulis fungsi baru.** Kalau sudah ada fungsi yang
mengerjakan hal serupa, pakai itu — atau ubah supaya mencakup kebutuhan baru.
Dua fungsi yang mirip tapi beda tipis adalah tempat bug bersembunyi: saat
aturannya berubah, yang satu diperbaiki dan yang satu lagi terlupa.

**Logika yang tidak sepele meninggalkan satu uji yang bisa dijalankan.** Bukan
uji per fungsi — yang paling berguna di servis ini uji yang merakit servis
lengkap di atas SQLite sungguhan, seperti `internal/api/api_test.go`.

## Prinsip yang sudah disepakati

- **Berkas ditulis lebih dulu, barisnya belakangan.** Baris yang ada lebih dulu
  akan muncul di antrean integrator dengan berkas yang belum tentu jadi. Kalau
  barisnya gagal, berkasnya dibuang lagi.
- **Cuma pemegang claim yang boleh melapor hasilnya.** Ini yang membuat claim
  bukan sekadar saran: tanpa itu, aplikasi yang kalah lomba tinggal mengabaikan
  jawaban 409, tetap memposting, lalu melapor — dan engine mencatatnya sebagai
  tayang. Syaratnya ada di dalam `UPDATE`-nya, bukan diperiksa terpisah.
  **Mengunduh berkas sengaja tidak butuh claim**: ia tidak mengubah apa pun,
  harus boleh diulang saat koneksi putus, dan panel perlu pratinjau.
- **Satu konten cuma boleh diambil satu aplikasi.** Syaratnya ada di dalam
  perintah `UPDATE`-nya, bukan diperiksa lebih dulu lalu ditulis. Kalau dipisah
  jadi dua langkah, dua integrator bisa sama-sama lolos pemeriksaan sebelum salah
  satunya sempat menulis — dan keduanya memposting konten yang sama. Gagalnya
  senyap: yang melapor pembacanya.
- **"Sudah waktunya" dihitung di server.** Integrator menanyakan `?scope=due`,
  tidak menghitung jadwalnya sendiri. Kalau tiap integrator menghitungnya versi
  masing-masing, beda zona waktu dan jeda cron membuat konten terlewat atau
  terkirim dua kali.
- **Satu bentuk waktu, dan itu waktu server.** `2026-09-13 09:00:00`, tanpa zona
  waktu — yang dikirim, yang tersimpan, dan yang dibalas semuanya sama persis.
  Bentuk lain ditolak, tidak diterjemahkan diam-diam: jadwal yang salah tafsir
  baru ketahuan saat kontennya tayang di jam yang keliru. Harganya, **zona waktu
  VPS jadi bagian dari data** — setel sekali, lalu jangan diubah. Bentuknya
  diputuskan di `internal/models/time.go`, dan tidak ada tempat lain yang
  memformat waktu sendiri.
- **Yang gagal tidak kembali sendiri ke antrean.** Konten yang ditolak platform
  biasanya ditolak lagi kalau dikirim ulang apa adanya, dan antrean yang mengisi
  ulang dirinya sendiri akan mencoba selamanya tanpa ada yang tahu. Yang
  mengembalikannya manusia, lewat `/schedule`.
- **Pembatas laju harus melihat pemanggil yang sebenarnya.** Di balik nginx,
  `RemoteAddr` selalu `127.0.0.1` — tanpa penanganan khusus, seluruh integrator
  berbagi satu jatah dan satu aplikasi yang beruntun mematikan yang lain. Header
  alamat asli **cuma dipercaya dari jaringan di `TRUSTED_PROXIES`**; dari luar
  itu siapa pun bisa mengarangnya.
- **Tiga pekerjaan jalan sendiri, dan semuanya harus aman diulang.** Claim yang
  ditinggalkan dilepas setelah 2 jam (tiap 5 menit); berkas konten yang sudah
  selesai dibuang setelah 90 hari (tiap 24 jam); potongan unggahan yang
  ditinggalkan servis yang mati mendadak dibuang setelah 6 jam — yang terakhir
  **tidak** ikut dimatikan `RETENTION_DAYS=0`, karena itu sampah, bukan
  kebijakan penyimpanan. Aturannya di `ARCHITECTURE.md` §7. Yang paling mudah terbalik: **berkas dibuang dulu,
  penandanya belakangan** — dibalik, berkas yang gagal dihapus akan bertanda
  "sudah dibuang" dan tidak akan pernah dilirik lagi.
- **Ambang lepas claim jangan dipendekkan tanpa alasan.** Integrator yang masih
  mengunggah video besar di jaringan pelan akan direbut aplikasi lain, dan
  kontennya tayang dua kali — persis yang seluruh mekanisme claim ini cegah.
- **Stok yang belum pernah tayang tidak pernah disapu**, berapa lama pun sudah
  diunggah. Yang menunggu gilirannya bukan sampah.
- **Servis ini tidak pernah menghapus apa pun.** Konten dinonaktifkan lewat
  `/archival`, aplikasi dicabut lewat `/revocation`, dan riwayatnya tetap
  terbaca. Tidak ada `DELETE`, dan seluruh perubahan pakai `POST`. Yang benar-
  benar dibuang cuma berkas yang lewat umurnya — barisnya tetap.
- **Kunci cuma terlihat sekali.** Yang disimpan hashnya. Database yang bocor
  tidak berisi satu pun kunci yang bisa dipakai.
- **Galat aslinya tidak pernah keluar.** Nama tabel, path berkas, bunyi
  penolakan SQLite — semuanya berhenti di log, lewat satu-satunya penulis galat,
  `httpx.Fail`. Klien memilih tindakan dari `status`, bukan dari `code`.
- **Setiap galat dicatat sekali, dengan nama operasinya.** Lapisan service
  memanggil `run()` yang mencatat `op` beserta golongan galatnya; 4xx di Info,
  5xx di Error. Isi request tidak pernah masuk log — yang lewat servis ini
  berkas konten dan kunci API.
- **Pencadangan mencakup berkasnya, bukan cuma databasenya.** Database yang utuh
  tanpa berkasnya adalah daftar konten yang semuanya rusak. Itu seluruh alasan
  keduanya tinggal di satu folder.
- **Golongan `status` baru ditambah hanya kalau kalimat umumnya menyesatkan**,
  dan ambangnya di sini lebih ketat daripada biasanya: kliennya di luar kendali
  kita, jadi golongan baru berarti semua integrator yang sudah jalan harus
  diberi tahu.
- **Tulis standarnya dulu, kodenya menyesuaikan.** Aturan yang berlaku ada di
  `ARCHITECTURE.md`; kalau kode dan berkas itu berbeda, salah satunya harus
  diperbaiki.
- Jangan tambah komponen infrastruktur tanpa angka beban yang membuktikannya
  perlu.

## Gaya jawaban

- **Ringkas.** Jawab yang ditanya, berhenti di situ.
- **Rekomendasi, bukan perbandingan.** Kalau ada beberapa pilihan, sebutkan satu
  yang disarankan dan alasannya dalam satu-dua kalimat.
- **Bahasa sederhana.** Hindari istilah teknis tanpa penjelasan.
- Tabel hanya untuk data yang memang berkolom. Bukan untuk memecah prosa.
- Kalau butuh menjelaskan panjang, tanya dulu: *"mau versi singkat atau
  lengkap?"*
