// Command api menyalakan servis.
//
// Isinya cuma urutan menyalakan dan mematikan. Aturan main ada di
// internal/services, perakitan di internal/api.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/gemgum/stock-engine/internal/api"
	"github.com/gemgum/stock-engine/internal/config"
	"github.com/gemgum/stock-engine/internal/database"
	"github.com/gemgum/stock-engine/internal/models"
	"github.com/gemgum/stock-engine/internal/services"
	"github.com/gemgum/stock-engine/migrations"
	"github.com/gemgum/stock-engine/pkg/redis"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	if err := run(logger); err != nil {
		logger.Error("servis berhenti", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	// Foldernya dibuat di sini, sebelum apa pun menyentuhnya. SQLite tidak
	// membuat folder induk berkasnya sendiri — ia cuma menjawab "unable to open
	// database file", dan pesan itu tidak menyebut foldernya yang belum ada.
	if err := os.MkdirAll(cfg.StorageDir, 0o755); err != nil {
		return err
	}

	db, err := database.Open(cfg.DatabasePath())
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	// Migrasi jalan saat start, bukan lewat perintah terpisah. Langkah terpisah
	// adalah langkah yang bisa terlupa, dan skema yang tertinggal satu kolom
	// gagalnya tidak selalu langsung terlihat.
	list, err := migrations.All()
	if err != nil {
		return err
	}
	if err := db.Migrate(context.Background(), list); err != nil {
		return err
	}

	files, err := services.NewFiles(cfg.FileDir())
	if err != nil {
		return err
	}

	// Ditulis sebagai antarmuka yang dibiarkan nil kalau Redis tidak diatur —
	// bukan *redis.Stream yang nil, yang tetap terhitung "ada" bagi service.
	var events models.Events
	if cfg.RedisAddr != "" {
		events = redis.NewStream(cfg.RedisAddr, cfg.RedisPassword, cfg.RedisStream)
	}

	handler, janitor := api.Build(db, files, events, cfg, logger)

	// Sapuan berkas berjalan selama servis hidup, dan berhenti begitu servis
	// diminta mati. Tidak ditunggu: yang terpotong di tengah aman diulang.
	kerja, hentikanKerja := context.WithCancel(context.Background())
	defer hentikanKerja()
	go janitor.Run(kerja)

	server := &http.Server{
		Addr:    cfg.Address,
		Handler: handler,

		// Tanpa batas baca dan tulis, dan itu disengaja.
		//
		// Yang lewat servis ini adalah video ratusan megabyte. Batas tulis 30
		// detik memutus unduhan yang sah di jaringan yang pelan, dan yang
		// kelihatan di sisi integrator cuma berkas rusak tanpa keterangan.
		// Yang tetap dibatasi: waktu mengirim header, karena koneksi yang
		// membuka lalu diam adalah cara paling murah menghabiskan koneksi.
		ReadHeaderTimeout: config.ShutdownGrace,
	}

	// Berhenti dengan rapi: permintaan yang sedang jalan diberi waktu selesai.
	// Unggahan 200MB yang diputus di tengah harus dikirim ulang dari nol.
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-stop
		logger.Info("servis diminta berhenti")
		hentikanKerja()

		ctx, cancel := context.WithTimeout(context.Background(), config.ShutdownGrace)
		defer cancel()

		if err := server.Shutdown(ctx); err != nil {
			logger.Error("gagal berhenti dengan rapi", "error", err)
		}
	}()

	logger.Info("servis menyala",
		"address", cfg.Address,
		"storage", cfg.StorageDir,
		"retensi_hari", cfg.RetentionDays,
		"redis", cfg.RedisAddr,
		"stream", cfg.RedisStream)

	if err := server.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
