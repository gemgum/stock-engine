-- Daftar platform yang boleh ditulis di kolom `contents.platform`.
--
-- Sebelum ini `platform` teks bebas, dan `?platform=instagram` diam-diam
-- melewatkan konten yang diunggah sebagai `Instagram` atau `instgram`. Sekarang
-- unggahan dengan nama yang tidak ada di sini ditolak.
--
-- Namanya sekaligus kuncinya: itulah yang ditulis integrator di unggahan dan di
-- saringan, jadi id terpisah cuma menambah satu hal untuk ditukar-tukar.
--
-- Tanpa foreign key dari `contents`: konten lama boleh menyebut nama yang tidak
-- pernah terdaftar, dan barisnya tetap harus terbaca.
CREATE TABLE IF NOT EXISTS platforms (
  name        TEXT PRIMARY KEY,
  created_at  TEXT NOT NULL,
  archived_at TEXT
);

-- Dua yang sudah dipakai integrator saat tabel ini dibuat, supaya unggahan
-- tidak langsung tertolak sesudah deploy.
INSERT OR IGNORE INTO platforms (name, created_at) VALUES
  ('instagram', strftime('%Y-%m-%d %H:%M:%S', 'now', 'localtime')),
  ('tiktok',    strftime('%Y-%m-%d %H:%M:%S', 'now', 'localtime'));
