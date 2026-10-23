package repository

import (
	"context"
	"database/sql"

	"github.com/gemgum/stock-engine/internal/models"
)

type Platform struct{}

const platformColumns = `name, created_at, archived_at`

func scanPlatform(row Scanner) (models.Platform, error) {
	var (
		p          models.Platform
		createdAt  string
		archivedAt sql.NullString
		err        error
	)

	if err = row.Scan(&p.Name, &createdAt, &archivedAt); err != nil {
		return p, err
	}
	if p.CreatedAt, err = ReadTime(createdAt); err != nil {
		return p, err
	}
	p.ArchivedAt, err = ReadTimePtr(archivedAt)
	return p, err
}

func (Platform) List(ctx context.Context) ([]models.Platform, error) {
	return Many(ctx, scanPlatform, `SELECT `+platformColumns+` FROM platforms ORDER BY name`)
}

func (Platform) Get(ctx context.Context, name string) (models.Platform, error) {
	return One(ctx, scanPlatform, `SELECT `+platformColumns+` FROM platforms WHERE name = ?`, name)
}

// Upsert mendaftarkan nama baru, atau mengaktifkan lagi yang diarsipkan.
//
// Satu perintah, supaya mendaftarkan ulang nama yang sudah ada aman diulang dan
// tidak berakhir di "That value already exists".
func (Platform) Upsert(ctx context.Context, name string, at models.Time) (models.Platform, error) {
	const q = `INSERT INTO platforms (name, created_at) VALUES (?, ?)
		ON CONFLICT (name) DO UPDATE SET archived_at = NULL
		RETURNING ` + platformColumns

	return One(ctx, scanPlatform, q, name, Stamp(at))
}

// SetArchived aman diulang: yang sudah diarsipkan tidak berubah waktu arsipnya.
func (Platform) SetArchived(ctx context.Context, name string, at models.Time) error {
	const q = `UPDATE platforms SET archived_at = ? WHERE name = ? AND archived_at IS NULL`

	_, err := Exec(ctx, q, Stamp(at), name)
	return err
}
