package repository

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/gemgum/stock-engine/internal/models"
	"github.com/gemgum/stock-engine/pkg/httpx"
)

type Content struct{}

const contentColumns = `id, filename, mime, size, checksum, caption, platform,
	scheduled_at, status, claimed_by, claimed_at, published_at, failure_reason,
	archived_at, uploaded_by, created_at`

func scanContent(row Scanner) (models.Content, error) {
	var (
		c                                  models.Content
		scheduledAt, createdAt             string
		claimedBy, failureReason           sql.NullString
		claimedAt, publishedAt, archivedAt sql.NullString
		err                                error
	)

	if err = row.Scan(&c.ID, &c.Filename, &c.Mime, &c.Size, &c.Checksum, &c.Caption,
		&c.Platform, &scheduledAt, &c.Status, &claimedBy, &claimedAt, &publishedAt,
		&failureReason, &archivedAt, &c.UploadedBy, &createdAt); err != nil {
		return c, err
	}

	if c.ScheduledAt, err = ReadTime(scheduledAt); err != nil {
		return c, err
	}
	if c.CreatedAt, err = ReadTime(createdAt); err != nil {
		return c, err
	}
	if c.ClaimedAt, err = ReadTimePtr(claimedAt); err != nil {
		return c, err
	}
	if c.PublishedAt, err = ReadTimePtr(publishedAt); err != nil {
		return c, err
	}
	if c.ArchivedAt, err = ReadTimePtr(archivedAt); err != nil {
		return c, err
	}

	c.ClaimedBy = ReadStringPtr(claimedBy)
	c.FailureReason = ReadStringPtr(failureReason)
	return c, nil
}

func (Content) Insert(ctx context.Context, in models.NewContent) (models.Content, error) {
	const q = `INSERT INTO contents
		(id, filename, mime, size, checksum, caption, platform, scheduled_at,
		 status, uploaded_by, created_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?)
		RETURNING ` + contentColumns

	return One(ctx, scanContent, q,
		in.ID, in.Filename, in.Mime, in.Size, in.Checksum, in.Caption, in.Platform,
		Stamp(in.ScheduledAt), models.StatusScheduled, in.UploadedBy, Stamp(models.Now()))
}

func (Content) Get(ctx context.Context, id string) (models.Content, error) {
	return One(ctx, scanContent, `SELECT `+contentColumns+` FROM contents WHERE id = ?`, id)
}

// contentWhere menyusun penyaringnya dari scope.
//
// Satu tempat, dipakai List dan Count. Kalau keduanya menyusun penyaringnya
// sendiri-sendiri, cepat atau lambat keduanya berbeda tipis — dan yang terjadi
// bukan galat melainkan total yang tidak cocok dengan isinya, yang cuma
// ketahuan kalau ada yang menghitung barisnya sendiri.
func contentWhere(f models.ContentFilter) (string, []any) {
	var where string
	var args []any

	switch f.Scope {
	case models.ScopeDue:
		// Dua syarat, dan keduanya wajib: sudah waktunya, **dan** belum diambil
		// siapa pun. Yang cuma memeriksa jadwal akan terus mengembalikan konten
		// yang sedang diproses integrator lain.
		where = `WHERE status = ? AND scheduled_at <= ?`
		args = append(args, models.StatusScheduled, Stamp(f.Now))
	case models.ScopeUnused:
		// Belum tayang dan belum dibatalkan. Disebut lewat status yang **tidak**
		// termasuk, bukan yang termasuk: status baru yang ditambah nanti otomatis
		// terhitung stok, dan lupa menambahkannya di sini akan menyembunyikan
		// konten yang sebenarnya masih punya giliran.
		where = `WHERE status NOT IN (?, ?)`
		args = append(args, models.StatusPublished, models.StatusArchived)
	case models.ScopeOverdue:
		where = `WHERE status NOT IN (?, ?) AND scheduled_at <= ?`
		args = append(args, models.StatusPublished, models.StatusArchived, Stamp(f.Now))
	case models.ScopeAll:
		where = `WHERE 1 = 1`
	default:
		where = `WHERE status = ?`
		args = append(args, string(f.Scope))
	}

	if f.Platform != "" {
		where += ` AND platform = ?`
		args = append(args, f.Platform)
	}
	return where, args
}

// List mengembalikan satu halaman.
//
// Urutannya selalu menurut jadwal, bukan menurut waktu unggah: yang ditanyakan
// integrator adalah "mana yang berikutnya tayang", dan itu urutan jadwal.
func (Content) List(ctx context.Context, f models.ContentFilter) ([]models.Content, error) {
	where, args := contentWhere(f)

	// ponytail: pagination lewat offset. Cukup untuk antrean berukuran ribuan;
	// ganti ke keysetnya (scheduled_at, id) kalau daftarnya tumbuh sampai
	// halaman belakang terasa lambat.
	q := fmt.Sprintf(`SELECT %s FROM contents %s ORDER BY scheduled_at ASC LIMIT ? OFFSET ?`,
		contentColumns, where)
	args = append(args, f.Limit, f.Offset)

	return Many(ctx, scanContent, q, args...)
}

// Count menghitung seluruh yang cocok, tanpa batas halamannya.
func (Content) Count(ctx context.Context, f models.ContentFilter) (int, error) {
	where, args := contentWhere(f)

	return One(ctx, func(row Scanner) (int, error) {
		var total int
		err := row.Scan(&total)
		return total, err
	}, `SELECT count(*) FROM contents `+where, args...)
}

// Claim menang atau kalah dalam satu perintah.
//
// Syarat `status = 'scheduled'` ada di dalam UPDATE-nya, bukan diperiksa lebih
// dulu lalu ditulis. Kalau dipisah jadi dua langkah, dua integrator bisa
// sama-sama lolos pemeriksaan sebelum salah satunya sempat menulis — dan
// keduanya memposting konten yang sama.
func (Content) Claim(ctx context.Context, id, appID string, at models.Time) (bool, error) {
	const q = `UPDATE contents SET status = ?, claimed_by = ?, claimed_at = ?
		WHERE id = ? AND status = ?`

	changed, err := Exec(ctx, q, models.StatusClaimed, appID, Stamp(at), id, models.StatusScheduled)
	return changed > 0, err
}

// SetPublication mencatat hasil akhir yang dilaporkan integrator.
//
// Dua syarat di dalam UPDATE-nya, dan keduanya wajib: kontennya sudah diclaim,
// **dan** yang melapor adalah yang mengclaim.
//
// Syarat kedua yang menegakkan seluruh mekanisme claim. Tanpa itu, aplikasi
// yang kalah lomba tinggal mengabaikan jawaban 409, tetap memposting, lalu
// melapor — dan engine mencatatnya sebagai tayang. Claim jadi sekadar saran,
// dan konten tetap bisa tayang dua kali.
//
// Mengembalikan false kalau tidak ada yang berubah; yang memilih kalimatnya
// service, karena ia yang bisa melihat kenapa.
func (Content) SetPublication(ctx context.Context, id, appID string, status models.Status,
	reason *string, at models.Time) (bool, error) {

	const q = `UPDATE contents SET status = ?, failure_reason = ?, published_at = ?
		WHERE id = ? AND status = ? AND claimed_by = ?`

	var publishedAt any
	if status == models.StatusPublished {
		publishedAt = Stamp(at)
	}

	changed, err := Exec(ctx, q, status, reason, publishedAt, id, models.StatusClaimed, appID)
	return changed > 0, err
}

// SetSchedule memindahkan jadwal dan mengembalikan kontennya ke antrean.
//
// Satu alamat mengerjakan dua hal karena keduanya memang selalu diminta
// bersamaan: yang memindahkan jadwal konten gagal selalu bermaksud
// mencobanya lagi. Alamat terpisah untuk "kembalikan ke antrean" berarti dua
// panggilan yang tidak pernah dipakai sendiri-sendiri.
func (Content) SetSchedule(ctx context.Context, id string, at models.Time) error {
	const q = `UPDATE contents
		SET scheduled_at = ?, status = ?, claimed_by = NULL, claimed_at = NULL,
		    failure_reason = NULL
		WHERE id = ? AND status != ?`

	changed, err := Exec(ctx, q, Stamp(at), models.StatusScheduled, id, models.StatusPublished)
	if err != nil {
		return err
	}
	if changed == 0 {
		return httpx.BadRequest("Published content cannot be rescheduled.")
	}
	return nil
}

// Archive mengeluarkan konten dari antrean tanpa menghapusnya.
//
// Servis ini tidak pernah menghapus apa pun: yang batal tetap harus terbaca
// riwayatnya, dan berkasnya tetap ada sampai ada yang sengaja membuangnya.
func (Content) Archive(ctx context.Context, id string, at models.Time) error {
	const q = `UPDATE contents SET status = ?, archived_at = ? WHERE id = ?`

	changed, err := Exec(ctx, q, models.StatusArchived, Stamp(at), id)
	if err != nil {
		return err
	}
	if changed == 0 {
		return httpx.NotFound("The requested item does not exist.")
	}
	return nil
}
