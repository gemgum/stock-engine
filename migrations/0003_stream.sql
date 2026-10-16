-- Penanda bahwa konten sudah dikabarkan ke Redis Stream.
--
-- Tanpa penanda ini, konten yang sudah waktunya dikirim lagi tiap menit sampai
-- ada yang mengclaim. Dikosongkan lagi saat konten kembali ke antrean — lewat
-- /schedule atau saat claimnya dilepas — supaya ia dikabarkan ulang.
ALTER TABLE contents ADD COLUMN streamed_at TEXT;
