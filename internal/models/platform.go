package models

import "context"

// Platform adalah satu nilai yang boleh ditulis di kolom `platform` konten.
//
// Yang diarsipkan tidak bisa dipakai unggahan baru, tapi tetap boleh dipakai
// menyaring — konten lama yang menyebutnya masih ada di gudang.
type Platform struct {
	Name       string `json:"name"`
	CreatedAt  Time   `json:"created_at"`
	ArchivedAt *Time  `json:"archived_at"`
}

type PlatformRepository interface {
	List(ctx context.Context) ([]Platform, error)
	Get(ctx context.Context, name string) (Platform, error)

	// Upsert mendaftarkan nama baru, atau mengaktifkan lagi yang diarsipkan.
	Upsert(ctx context.Context, name string, at Time) (Platform, error)

	SetArchived(ctx context.Context, name string, at Time) error
}
