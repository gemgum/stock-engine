// Package services memegang aturan main: siapa boleh apa, dan langkah mana yang
// harus jadi satu.
//
// Yang tidak ada di sini: SQL (milik repository) dan bentuk jawaban HTTP (milik
// handlers).
package services

import (
	"context"
	"log/slog"

	"github.com/gemgum/stock-engine/internal/database"
	"github.com/gemgum/stock-engine/pkg/httpx"
)

// base adalah yang dipunyai semua service: sambungan database dan logger.
type base struct {
	db     database.Runner
	logger *slog.Logger
}

// run menjalankan satu transaksi, lalu mencatat galatnya sekali.
//
// Bedanya dengan memanggil database.Query langsung cuma satu — nama operasinya
// ikut tercatat — tapi itu yang membedakan log yang berguna dari log yang cuma
// bilang "gagal". Tanpa `op`, satu baris galat di server tidak memberi tahu
// fungsi mana yang menghasilkannya, dan itu justru yang pertama ingin diketahui
// saat ada integrator menelepon.
//
// Fungsi, bukan method, karena Go tidak mengizinkan method bertipe generik.
func run[T any](ctx context.Context, b base, op, actor string,
	fn func(context.Context) (T, error)) (T, error) {

	result, err := database.Query(ctx, b.db, fn)
	return result, logError(ctx, b.logger, op, actor, err)
}

// logError mencatat satu galat, lalu mengembalikannya apa adanya.
//
// **Levelnya dipilih dari golongan galatnya, bukan selalu Error.** Penolakan
// yang wajar — konten sudah diambil integrator lain, kunci kedaluwarsa — terjadi
// setiap hari dan bukan tanda kerusakan. Kalau semuanya Error, log banjir sampai
// galat yang sesungguhnya tenggelam.
//
// Isi request tidak pernah ikut. Yang lewat servis ini adalah berkas konten dan
// kunci API.
func logError(ctx context.Context, logger *slog.Logger, op, actor string, err error) error {
	if err == nil || logger == nil {
		return err
	}

	appErr := httpx.From(err)

	level := slog.LevelInfo
	if appErr.HTTPStatus() >= 500 {
		level = slog.LevelError
	}

	logger.Log(ctx, level, "operasi gagal",
		"op", op,
		"actor", actor,
		"status", string(appErr.Status),
		"code", appErr.Code,
		"error", err.Error(),
	)
	return err
}
