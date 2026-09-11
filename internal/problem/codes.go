// Package problem menerjemahkan penolakan database jadi galat yang boleh
// dilihat pemanggil.
//
// Ada supaya bunyi asli SQLite — nama tabel, nama kolom, nomor constraint —
// tidak pernah sampai ke luar. Buat integrator itu tidak berarti apa-apa, dan
// buat penyerang itu peta skema.
package problem

import (
	"database/sql"
	"errors"
	"strings"

	"github.com/gemgum/stock-engine/pkg/httpx"
)

// FromDatabase menggolongkan galat database.
//
// Tiga kemungkinan yang punya arti berbeda bagi pemanggil; sisanya satu
// golongan, karena bagi dia semuanya berarti hal yang sama — coba lagi nanti.
//
// ponytail: penggolongan lewat teks galat, bukan kode. modernc/sqlite membawa
// kode aslinya, tapi mengambilnya berarti internal/problem mengimpor driver —
// dan folder ini yang seharusnya tidak tahu drivernya apa. Ganti ke kode kalau
// suatu hari ada constraint yang salah digolongkan.
func FromDatabase(err error) error {
	if err == nil {
		return nil
	}
	if appErr, ok := err.(*httpx.Error); ok {
		return appErr
	}

	if errors.Is(err, sql.ErrNoRows) {
		return httpx.NotFound("The requested item does not exist.")
	}

	message := err.Error()
	switch {
	case strings.Contains(message, "UNIQUE constraint"):
		return httpx.BadRequest("That value already exists.").WithCause(err)
	case strings.Contains(message, "FOREIGN KEY constraint"):
		return httpx.BadRequest("That reference points to something that does not exist.").WithCause(err)
	}

	return httpx.DatabaseError(err)
}
