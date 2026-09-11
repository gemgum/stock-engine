// Package repository adalah satu-satunya tempat di servis ini yang menulis SQL.
//
// Semua kueri dari seluruh domain dikumpulkan di sini, satu berkas per hal yang
// dilayaninya. Kalau databasenya suatu hari diganti — dan itu memang mungkin,
// SQLite punya batas yang sudah diketahui — yang ditulis ulang folder ini saja.
//
// Tiap tipe di sini memenuhi antarmuka Repository milik domainnya. Domainnya
// sendiri tidak pernah mengimpor folder ini: yang menyambungkan keduanya cuma
// internal/api/build.go.
package repository

import (
	"context"
	"database/sql"
	"time"

	"github.com/gemgum/stock-engine/internal/database"
	"github.com/gemgum/stock-engine/internal/problem"
)

// Scanner adalah yang dipunyai *sql.Row dan *sql.Rows sama-sama, jadi satu
// fungsi scan bisa dipakai untuk kueri satu baris maupun banyak baris.
type Scanner interface {
	Scan(dest ...any) error
}

// One menjalankan kueri yang **harus** mengembalikan satu baris.
//
// Tidak ada baris berarti galat, dan diterjemahkan jadi NotFound. Yang
// dikembalikan tidak pernah membawa bunyi asli SQLite.
func One[T any](ctx context.Context, scan func(Scanner) (T, error), query string, args ...any) (T, error) {
	var zero T

	tx, err := database.Tx(ctx)
	if err != nil {
		return zero, err
	}

	hasil, err := scan(tx.QueryRowContext(ctx, query, args...))
	if err != nil {
		return zero, problem.FromDatabase(err)
	}
	return hasil, nil
}

// Many menjalankan kueri yang mengembalikan banyak baris.
//
// Hasilnya tidak pernah nil — klien menerima `[]`, bukan `null`, saat memang
// tidak ada barisnya. "Belum ada konten" dan "gagal memuat" tidak boleh
// terlihat sama.
func Many[T any](ctx context.Context, scan func(Scanner) (T, error), query string, args ...any) ([]T, error) {
	tx, err := database.Tx(ctx)
	if err != nil {
		return nil, err
	}

	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, problem.FromDatabase(err)
	}
	defer func() { _ = rows.Close() }()

	hasil := []T{}
	for rows.Next() {
		item, err := scan(rows)
		if err != nil {
			return nil, problem.FromDatabase(err)
		}
		hasil = append(hasil, item)
	}
	return hasil, problem.FromDatabase(rows.Err())
}

// Exec menjalankan perintah yang tidak mengembalikan baris, dan melaporkan
// berapa baris yang berubah.
//
// Jumlahnya ikut dikembalikan karena beberapa perintah di servis ini memang
// bertanya lewat jumlah itu — `claim` yang kalah lomba mengubah nol baris, dan
// itu bukan galat.
func Exec(ctx context.Context, query string, args ...any) (int64, error) {
	tx, err := database.Tx(ctx)
	if err != nil {
		return 0, err
	}

	res, err := tx.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, problem.FromDatabase(err)
	}
	return res.RowsAffected()
}

// ── Waktu ────────────────────────────────────────────────────────────────
//
// SQLite tidak punya tipe waktu. Empat fungsi di bawah adalah satu-satunya
// tempat bentuk simpannya diputuskan, supaya tidak ada kueri yang menuliskan
// formatnya sendiri lalu berbeda tipis dari yang lain.

const layout = time.RFC3339

// Stamp mengubah waktu jadi teks yang disimpan. Selalu UTC — waktu bersona
// lokal yang diurutkan sebagai teks akan salah urut begitu ada dua zona.
func Stamp(t time.Time) string { return t.UTC().Format(layout) }

// StampPtr sama, untuk kolom yang boleh kosong.
func StampPtr(t *time.Time) any {
	if t == nil {
		return nil
	}
	return Stamp(*t)
}

// ReadTime membaca kolom waktu yang wajib terisi.
func ReadTime(raw string) (time.Time, error) { return time.Parse(layout, raw) }

// ReadTimePtr membaca kolom waktu yang boleh kosong.
func ReadTimePtr(raw sql.NullString) (*time.Time, error) {
	if !raw.Valid || raw.String == "" {
		return nil, nil
	}
	t, err := time.Parse(layout, raw.String)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// ReadStringPtr membaca kolom teks yang boleh kosong.
func ReadStringPtr(raw sql.NullString) *string {
	if !raw.Valid {
		return nil
	}
	return &raw.String
}
