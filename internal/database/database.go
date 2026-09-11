// Package database membuka SQLite dan menjalankan setiap kueri di dalam satu
// transaksi.
//
// SQLite dipilih karena seluruh keadaan servis jadi satu folder yang bisa
// dicadangkan sekaligus — berkas database di sebelah berkas kontennya. Dengan
// database server terpisah ada dua mekanisme cadangan yang harus konsisten satu
// sama lain, dan yang paling mungkin terjadi bukan keduanya gagal melainkan
// keduanya jalan di jam berbeda.
//
// Batasnya jelas dan harus dipegang: **satu proses yang menulis.** Selama
// servisnya satu salinan itu bukan batasan sama sekali. Saat ia berjalan lebih
// dari satu salinan, SQLite berhenti jadi pilihan yang benar dan yang ditulis
// ulang cuma internal/repository.
//
// Transaksinya dititipkan di context, bukan dioper sebagai parameter. Itu yang
// membuat antarmuka tiap domain cukup menerima context.Context dan tidak perlu
// menyebut SQL sama sekali.
package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	_ "modernc.org/sqlite"
)

// ErrNoTransaction berarti ada kode yang mencoba menyentuh database di luar
// database.Run. Itu bug, bukan keadaan yang bisa terjadi karena pemakaian.
var ErrNoTransaction = errors.New("kueri dijalankan di luar transaksi")

type DB struct {
	sql *sql.DB
}

// Open menyiapkan koneksi beserta tiga pragma yang menentukan.
//
//   - journal_mode=WAL: pembaca tidak memblokir penulis. Tanpa ini, satu
//     unggahan 200MB membuat semua integrator yang sedang mengambil daftar ikut
//     menunggu.
//   - busy_timeout: yang menunggu giliran menunggu, bukan langsung gagal.
//   - foreign_keys=on: SQLite mematikannya secara bawaan, dan relasi yang tidak
//     ditegakkan sama saja dengan tidak ada.
func Open(path string) (*DB, error) {
	dsn := path + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(on)"

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("database tidak bisa dibuka: %w", err)
	}

	// Satu penulis, dan itu ditegakkan di sini.
	//
	// SQLite cuma mengizinkan satu transaksi tulis pada satu waktu. Membiarkan
	// pool membuka banyak koneksi berarti menukar antrean yang tertib dengan
	// galat "database is locked" yang muncul acak — dan yang kalah adalah
	// unggahan yang sudah terlanjur menulis berkasnya ke disk.
	db.SetMaxOpenConns(1)

	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("database tidak bisa dihubungi: %w", err)
	}
	return &DB{sql: db}, nil
}

func (d *DB) Close() error { return d.sql.Close() }

// Ping memastikan databasenya masih menjawab. Dipakai /healthz.
//
// Servis yang hidup tapi tidak bisa menyentuh database tidak bisa melayani satu
// permintaan pun, jadi menyebutnya sehat cuma menunda ketahuan.
func (d *DB) Ping(ctx context.Context) error { return d.sql.PingContext(ctx) }

// Migrate menjalankan skema. Aman diulang: seluruh isinya IF NOT EXISTS.
func (d *DB) Migrate(ctx context.Context, schema string) error {
	_, err := d.sql.ExecContext(ctx, schema)
	return err
}

type contextKey struct{}

// Run menjalankan fn di dalam satu transaksi.
//
// Kalau fn mengembalikan galat, transaksinya dibatalkan seluruhnya. Konten yang
// separuh tercatat lebih buruk daripada unggahan yang gagal: yang separuh
// muncul di antrean integrator dengan berkas yang tidak ada.
func (d *DB) Run(ctx context.Context, fn func(context.Context) error) error {
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if err := fn(context.WithValue(ctx, contextKey{}, tx)); err != nil {
		return err
	}
	return tx.Commit()
}

// Tx mengambil transaksi yang sedang berjalan.
//
// Dipakai **hanya** oleh internal/repository. Di luar itu tidak ada yang perlu
// tahu bahwa databasenya SQL.
func Tx(ctx context.Context) (*sql.Tx, error) {
	tx, ok := ctx.Value(contextKey{}).(*sql.Tx)
	if !ok {
		return nil, ErrNoTransaction
	}
	return tx, nil
}

// Runner adalah satu-satunya hal yang perlu diketahui lapisan service tentang
// database: cara menjalankan sesuatu di dalam transaksi.
//
// Antarmuka, bukan *DB, supaya service bisa diuji tanpa database — dan supaya
// terlihat jelas bahwa service tidak punya urusan lain dengan koneksi.
type Runner interface {
	Run(ctx context.Context, fn func(context.Context) error) error
}

// Query menjalankan satu transaksi dan mengembalikan hasilnya.
//
// Ada supaya tiap method service tidak mengulang enam baris pembukaan
// transaksi yang sama.
func Query[T any](ctx context.Context, run Runner, fn func(context.Context) (T, error)) (T, error) {
	var result T

	err := run.Run(ctx, func(ctx context.Context) error {
		var err error
		result, err = fn(ctx)
		return err
	})
	if err != nil {
		var zero T
		return zero, err
	}
	return result, nil
}
