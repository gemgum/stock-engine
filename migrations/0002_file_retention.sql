-- Penanda bahwa berkas konten sudah dibuang sapuan retensi.
--
-- Kolom sendiri, bukan disimpulkan dari "berkasnya tidak ada di disk". Tanpa
-- penanda, berkas yang hilang karena disapu tidak bisa dibedakan dari berkas
-- yang hilang karena kerusakan — dan keduanya butuh jawaban yang berbeda:
-- yang pertama normal dan sudah diberitahukan, yang kedua harus diperiksa.
ALTER TABLE contents ADD COLUMN file_removed_at TEXT;

-- Sapuan mencari konten yang sudah selesai dan sudah lewat umurnya. Tanpa
-- indeks ini ia memindai seluruh tabel tiap 24 jam — murah sekarang, tidak lagi
-- saat isinya puluhan ribu baris.
CREATE INDEX IF NOT EXISTS contents_retention
  ON contents(status, file_removed_at);
