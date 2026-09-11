package repository

import (
	"context"
	"database/sql"
	"time"

	"github.com/gemgum/stock-engine/internal/models"
)

type App struct{}

const kolomApp = `id, name, key_hash, created_at, revoked_at`

func scanApp(row Scanner) (models.App, error) {
	var (
		a         models.App
		createdAt string
		revokedAt sql.NullString
		err       error
	)

	if err = row.Scan(&a.ID, &a.Name, &a.KeyHash, &createdAt, &revokedAt); err != nil {
		return a, err
	}
	if a.CreatedAt, err = ReadTime(createdAt); err != nil {
		return a, err
	}
	a.RevokedAt, err = ReadTimePtr(revokedAt)
	return a, err
}

func (App) Insert(ctx context.Context, name, keyHash string, at time.Time) (models.App, error) {
	const q = `INSERT INTO apps (id, name, key_hash, created_at)
		VALUES (?,?,?,?) RETURNING ` + kolomApp

	return One(ctx, scanApp, q, models.NewID(), name, keyHash, Stamp(at))
}

func (App) List(ctx context.Context) ([]models.App, error) {
	return Many(ctx, scanApp, `SELECT `+kolomApp+` FROM apps ORDER BY created_at DESC`)
}

func (App) Get(ctx context.Context, id string) (models.App, error) {
	return One(ctx, scanApp, `SELECT `+kolomApp+` FROM apps WHERE id = ?`, id)
}

// FindActiveByHash mencari yang belum dicabut.
//
// Pencabutan disaring di dalam kueri, bukan sesudahnya. Yang dicabut lalu
// diperiksa di Go berarti ada satu jalur — yang lupa memeriksanya — di mana
// kunci yang sudah dicabut tetap bekerja.
func (App) FindActiveByHash(ctx context.Context, keyHash string) (models.App, error) {
	const q = `SELECT ` + kolomApp + ` FROM apps WHERE key_hash = ? AND revoked_at IS NULL`
	return One(ctx, scanApp, q, keyHash)
}

// SetRevoked mencabut kunci tanpa menghapus barisnya.
//
// Barisnya tetap karena id-nya masih tercatat di kolom `uploaded_by` dan
// `claimed_by` milik konten lama, dan riwayat yang menunjuk ke aplikasi yang
// sudah hilang tidak bisa dibaca lagi.
//
// Aman diulang: mencabut yang sudah dicabut tidak mengubah waktu cabutnya.
func (App) SetRevoked(ctx context.Context, id string, at time.Time) error {
	const q = `UPDATE apps SET revoked_at = ? WHERE id = ? AND revoked_at IS NULL`

	_, err := Exec(ctx, q, Stamp(at), id)
	return err
}
