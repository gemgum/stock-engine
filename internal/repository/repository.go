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

	"github.com/gemgum/stock-engine/internal/database"
	"github.com/gemgum/stock-engine/internal/models"
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

	result, err := scan(tx.QueryRowContext(ctx, query, args...))
	if err != nil {
		return zero, problem.FromDatabase(err)
	}
	return result, nil
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

	result := []T{}
	for rows.Next() {
		item, err := scan(rows)
		if err != nil {
			return nil, problem.FromDatabase(err)
		}
		result = append(result, item)
	}
	return result, problem.FromDatabase(rows.Err())
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

// Bentuknya `2026-09-13 09:00:00`, waktu server — diputuskan di
// models.TimeLayout, dan dipakai apa adanya di sini supaya yang tersimpan di
// database persis sama dengan yang dikirim dan dibalas.

// Stamp mengubah waktu jadi teks yang disimpan.
func Stamp(t models.Time) string { return t.String() }

// StampPtr sama, untuk kolom yang boleh kosong.
func StampPtr(t *models.Time) any {
	if t == nil {
		return nil
	}
	return Stamp(*t)
}

// ReadTime membaca kolom waktu yang wajib terisi.
func ReadTime(raw string) (models.Time, error) { return models.ParseTime(raw) }

// ReadTimePtr membaca kolom waktu yang boleh kosong.
func ReadTimePtr(raw sql.NullString) (*models.Time, error) {
	if !raw.Valid || raw.String == "" {
		return nil, nil
	}
	t, err := models.ParseTime(raw.String)
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
