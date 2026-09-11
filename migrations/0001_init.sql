-- Skema awal: aplikasi integrator dan konten.
--
-- Waktu disimpan sebagai teks `2026-09-13 09:00:00`, waktu server. SQLite tidak
-- punya tipe waktu, dan bentuk ini yang urutannya benar saat dibandingkan
-- sebagai teks — yang berarti `scheduled_at <= ?` bekerja apa adanya, tanpa
-- fungsi konversi di tiap kueri.
--
-- Konsekuensinya zona waktu server jadi bagian dari data: setel sekali saat
-- menyiapkan server, lalu jangan diubah. Alasan lengkapnya di
-- internal/models/time.go.

CREATE TABLE IF NOT EXISTS apps (
  id         TEXT PRIMARY KEY,
  name       TEXT NOT NULL,
  key_hash   TEXT NOT NULL UNIQUE,
  created_at TEXT NOT NULL,
  revoked_at TEXT
);

-- Pencarian kunci terjadi di **setiap** permintaan, jadi indeksnya bukan
-- kemewahan. UNIQUE di atas sudah menyediakannya.

CREATE TABLE IF NOT EXISTS contents (
  id       TEXT PRIMARY KEY,
  filename TEXT NOT NULL,
  mime     TEXT NOT NULL,
  size     INTEGER NOT NULL,
  checksum TEXT NOT NULL,

  caption  TEXT NOT NULL DEFAULT '',
  platform TEXT NOT NULL DEFAULT '',

  scheduled_at TEXT NOT NULL,
  status       TEXT NOT NULL DEFAULT 'scheduled'
               CHECK (status IN ('scheduled','claimed','published','failed','archived')),

  -- Tanpa foreign key ke apps, dan itu disengaja: pemakai kunci induk tercatat
  -- sebagai 'admin', yang memang tidak punya baris di sana.
  claimed_by     TEXT,
  claimed_at     TEXT,
  published_at   TEXT,
  failure_reason TEXT,
  archived_at    TEXT,
  uploaded_by    TEXT NOT NULL,

  created_at TEXT NOT NULL
);

-- Satu indeks untuk satu kueri: "mana yang sudah waktunya dan belum diambil".
-- Itu kueri yang dipanggil tiap beberapa menit oleh tiap integrator; sisanya
-- dipanggil manusia dan boleh lambat.
CREATE INDEX IF NOT EXISTS contents_queue ON contents(status, scheduled_at);
