package models

import (
	"context"
	"time"
)

// AdminID adalah id pemanggil yang memakai kunci induk.
//
// Bukan baris di tabel `apps`: kunci induk hidup di environment supaya selalu
// ada jalan masuk saat belum ada satu pun aplikasi terdaftar. Karena itu kolom
// `uploaded_by` dan `claimed_by` tidak punya foreign key — nilai ini tidak akan
// pernah menemukan pasangannya di sana.
const AdminID = "admin"

// App adalah satu aplikasi integrator yang boleh memanggil servis ini.
//
// Kuncinya sendiri tidak pernah ada di sini. Yang disimpan cuma hashnya, dan
// hash itu tidak pernah ikut keluar — lihat tag `json:"-"`.
type App struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	KeyHash   string     `json:"-"`
	CreatedAt time.Time  `json:"created_at"`
	RevokedAt *time.Time `json:"revoked_at"`
}

// CreatedApp adalah **satu-satunya saat kuncinya terlihat.**
//
// Sesudah balasan ini terkirim, tidak ada cara memulihkannya. Kalau hilang,
// aplikasinya dicabut dan didaftarkan lagi.
type CreatedApp struct {
	App
	Key string `json:"key"`
}

type AppRepository interface {
	List(ctx context.Context) ([]App, error)
	Insert(ctx context.Context, name, keyHash string, at time.Time) (App, error)
	Get(ctx context.Context, id string) (App, error)

	// FindActiveByHash mencari yang belum dicabut. Yang sudah dicabut
	// diperlakukan sama dengan yang tidak ada.
	FindActiveByHash(ctx context.Context, keyHash string) (App, error)

	SetRevoked(ctx context.Context, id string, at time.Time) error
}
