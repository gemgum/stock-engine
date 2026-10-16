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
	archived_at, file_removed_at, streamed_at, uploaded_by, created_at`

func scanContent(row Scanner) (models.Content, error) {
	var (
		c                                  models.Content
		scheduledAt, createdAt             string
		claimedBy, failureReason           sql.NullString
		claimedAt, publishedAt, archivedAt sql.NullString
		fileRemovedAt, streamedAt          sql.NullString
		err                                error
	)

	if err = row.Scan(&c.ID, &c.Filename, &c.Mime, &c.Size, &c.Checksum, &c.Caption,
		&c.Platform, &scheduledAt, &c.Status, &claimedBy, &claimedAt, &publishedAt,
		&failureReason, &archivedAt, &fileRemovedAt, &streamedAt, &c.UploadedBy, &createdAt); err != nil {
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
	if c.FileRemovedAt, err = ReadTimePtr(fileRemovedAt); err != nil {
		return c, err
	}
	if c.StreamedAt, err = ReadTimePtr(streamedAt); err != nil {
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
	if f.Unstreamed {
		where += ` AND streamed_at IS NULL`
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
//
// Mengembalikan false kalau tidak ada yang berubah; yang memilih kalimatnya
// service, karena ia yang bisa melihat kenapa. Nol baris di sini punya dua sebab
// yang berbeda jauh — kontennya tidak ada, atau sudah tayang — dan menjawab
// keduanya dengan kalimat yang sama membuat yang salah ketik id mencari-cari
// konten yang tidak pernah ada.
func (Content) SetSchedule(ctx context.Context, id string, at models.Time) (bool, error) {
	// Syarat ketiga: berkasnya masih ada. Konten yang berkasnya sudah dibuang
	// sapuan retensi tidak boleh kembali ke antrean — integrator akan
	// mengambilnya, gagal mengunduh, dan yang terlihat cuma kegagalan berulang
	// tanpa sebab yang jelas.
	//
	// streamed_at dikosongkan supaya konten ini dikabarkan lagi di jadwal
	// barunya.
	const q = `UPDATE contents
		SET scheduled_at = ?, status = ?, claimed_by = NULL, claimed_at = NULL,
		    failure_reason = NULL, streamed_at = NULL
		WHERE id = ? AND status != ? AND file_removed_at IS NULL`

	changed, err := Exec(ctx, q, Stamp(at), models.StatusScheduled, id, models.StatusPublished)
	return changed > 0, err
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

// ExpiredFiles menyebut konten yang berkasnya sudah boleh dibuang.
//
// "Selesainya" diambil dari waktu tayang atau waktu diarsipkan, bukan waktu
// unggah: konten yang diunggah setahun lalu tapi baru tayang kemarin masih
// muda umurnya. COALESCE memilih yang terisi — sebuah baris tidak pernah punya
// keduanya sekaligus.
func (Content) ExpiredFiles(ctx context.Context, before models.Time, limit int) ([]models.Content, error) {
	const q = `SELECT ` + contentColumns + ` FROM contents
		WHERE status IN (?, ?)
		  AND file_removed_at IS NULL
		  AND COALESCE(published_at, archived_at) <= ?
		ORDER BY COALESCE(published_at, archived_at) ASC
		LIMIT ?`

	return Many(ctx, scanContent, q,
		models.StatusPublished, models.StatusArchived, Stamp(before), limit)
}

func (Content) MarkStreamed(ctx context.Context, id string, at models.Time) error {
	_, err := Exec(ctx, `UPDATE contents SET streamed_at = ? WHERE id = ?`, Stamp(at), id)
	return err
}

func (Content) MarkFileRemoved(ctx context.Context, id string, at models.Time) error {
	_, err := Exec(ctx, `UPDATE contents SET file_removed_at = ? WHERE id = ?`, Stamp(at), id)
	return err
}

// ReleaseStuckClaims mengembalikan claim yang tidak pernah dilaporkan.
//
// Satu perintah, tanpa membaca dulu: yang dicari justru baris yang tidak ada
// yang mengurus, dan memeriksanya satu per satu cuma menambah kesempatan
// integrator yang sebenarnya masih hidup melapor di sela-selanya.
//
// `failure_reason` sengaja tidak diisi. Yang terjadi bukan kegagalan yang
// dilaporkan siapa pun — tidak ada yang tahu apa yang terjadi di sana, dan
// mengarangnya akan tercampur dengan alasan gagal yang sungguhan.
//
// streamed_at ikut dikosongkan. Kalau tidak, konten yang dilepas kembali ke
// `?scope=due` tapi tidak pernah dikabarkan lagi ke Redis Stream — integrator
// yang cuma membaca stream tidak akan pernah melihatnya.
func (Content) ReleaseStuckClaims(ctx context.Context, before models.Time) (int, error) {
	const q = `UPDATE contents
		SET status = ?, claimed_by = NULL, claimed_at = NULL, streamed_at = NULL
		WHERE status = ? AND claimed_at <= ?`

	changed, err := Exec(ctx, q, models.StatusScheduled, models.StatusClaimed, Stamp(before))
	return int(changed), err
}
