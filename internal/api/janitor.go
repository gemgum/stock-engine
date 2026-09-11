package api

import (
	"context"
	"log/slog"
	"time"
)

// Dua pekerjaan, dua irama, dan bedanya bukan selera.
//
// Sapuan berkas mengurus ambang yang dihitung dalam hari: berkas yang lewat
// umurnya beberapa jam lebih lama tidak merugikan siapa pun, sedangkan sapuan
// yang sering cuma menambah kueri yang hampir selalu kosong.
//
// Pelepasan claim mengurus ambang yang dihitung dalam jam. Menjalankannya
// sekali sehari berarti konten yang tersangkut jam sembilan pagi baru kembali
// ke antrean besok — jauh sesudah jadwalnya tidak ada artinya lagi.
const (
	purgeInterval   = 24 * time.Hour
	releaseInterval = 5 * time.Minute
)

// Janitor menjalankan pekerjaan yang tidak dipicu permintaan siapa pun.
//
// Ada tiga sekarang: membuang berkas yang lewat umurnya, membuang potongan
// unggahan yang ditinggalkan servis yang mati mendadak, dan mengembalikan claim
// yang ditinggalkan integrator yang mati. Aturannya tiga, dan berlaku untuk yang berikutnya kalau
// memang ada:
//
//   - **Lewat service, bukan langsung ke database.** Galatnya tercatat dengan
//     nama operasinya seperti permintaan HTTP mana pun.
//   - **Berhenti kalau contextnya dibatalkan**, dan tidak ditunggu saat servis
//     menutup. Yang terpotong di tengah aman diulang, bukan harus diselesaikan.
//   - **Aman diulang berapa kali pun.** Keduanya jalan sekali saat servis
//     menyala, dan servis bisa dinyalakan ulang lima kali dalam sejam.
type Janitor struct {
	housekeeper Housekeeper
	logger      *slog.Logger
}

// Housekeeper adalah satu-satunya hal yang perlu diketahui janitor tentang
// konten.
type Housekeeper interface {
	PurgeFiles(ctx context.Context) (int, error)
	PurgeStaleUploads(ctx context.Context) (int, error)
	ReleaseStuckClaims(ctx context.Context) (int, error)
}

func NewJanitor(housekeeper Housekeeper, logger *slog.Logger) *Janitor {
	return &Janitor{housekeeper: housekeeper, logger: logger}
}

// Run menjalankan kedua pekerjaan sampai contextnya dibatalkan.
//
// Keduanya dijalankan sekali di awal supaya servis yang baru dideploy tidak
// menunggu sebelum membereskan apa pun — dan supaya hasilnya kelihatan saat
// orang yang memasangnya masih menonton lognya.
func (j *Janitor) Run(ctx context.Context) {
	// Galatnya sudah tercatat di dalam; di sini memang tidak ada yang bisa
	// dilakukan selain mencoba lagi nanti.
	_ = j.Once(ctx)

	purge := time.NewTicker(purgeInterval)
	defer purge.Stop()

	release := time.NewTicker(releaseInterval)
	defer release.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-purge.C:
			// Galatnya sudah tercatat di dalam; di sini memang tidak ada yang
			// bisa dilakukan selain menunggu giliran berikutnya.
			_ = j.jalankan(ctx, "sapuan berkas", j.housekeeper.PurgeFiles)
			_ = j.jalankan(ctx, "sapuan berkas sementara", j.housekeeper.PurgeStaleUploads)
		case <-release.C:
			_ = j.jalankan(ctx, "pelepasan claim", j.housekeeper.ReleaseStuckClaims)
		}
	}
}

// Once menjalankan kedua pekerjaan sekali.
//
// Terbuka supaya uji bisa memanggilnya langsung — perilaku sapuan tidak boleh
// diuji dengan menunggu jam berganti.
//
// Urutannya pelepasan dulu, sapuan berkas belakangan: claim yang baru dilepas
// kembali jadi stok, dan stok tidak pernah disapu. Dibalik pun hasilnya sama,
// tapi urutan ini yang tidak perlu dijelaskan dua kali.
func (j *Janitor) Once(ctx context.Context) error {
	if err := j.jalankan(ctx, "pelepasan claim", j.housekeeper.ReleaseStuckClaims); err != nil {
		return err
	}
	if err := j.jalankan(ctx, "sapuan berkas", j.housekeeper.PurgeFiles); err != nil {
		return err
	}
	return j.jalankan(ctx, "sapuan berkas sementara", j.housekeeper.PurgeStaleUploads)
}

// jalankan mencatat galatnya, lalu meneruskannya.
//
// Dicatat di sini karena pemanggil di Run memang tidak melakukan apa pun
// dengannya: pekerjaan yang gagal akan dicoba lagi nanti, dan servis tidak boleh
// berhenti melayani konten gara-gara pembersihan yang gagal. Diteruskan supaya
// uji tidak perlu membaca log untuk tahu ada yang salah.
func (j *Janitor) jalankan(ctx context.Context, nama string,
	kerja func(context.Context) (int, error)) error {

	_, err := kerja(ctx)
	if err != nil && ctx.Err() == nil {
		j.logger.Error(nama+" gagal", "error", err.Error())
	}
	return err
}
