package services

import (
	"context"
	"io"
	"log/slog"
	"strings"

	"github.com/gemgum/stock-engine/internal/database"
	"github.com/gemgum/stock-engine/internal/models"
	"github.com/gemgum/stock-engine/pkg/httpx"
)

// ContentService memegang aturan main gudang konten.
//
// Yang dijaga di sini ada tiga: jadwal yang masuk akal, satu konten cuma boleh
// diambil satu aplikasi, dan berkas tidak pernah tertinggal tanpa barisnya.
type ContentService struct {
	base
	repo  models.ContentRepository
	files models.Files

	// now bisa diganti saat pengujian. Seluruh servis ini soal waktu, dan
	// menguji "sudah waktunya" dengan menunggu jam berganti bukan pengujian.
	now func() models.Time
}

func NewContentService(db database.Runner, repo models.ContentRepository,
	files models.Files, logger *slog.Logger) *ContentService {

	return &ContentService{
		base:  base{db: db, logger: logger},
		repo:  repo,
		files: files,
		now:   models.Now,
	}
}

// Upload menyimpan berkasnya lebih dulu, barisnya belakangan.
//
// Urutannya penting dan tidak boleh dibalik. Baris yang ditulis lebih dulu akan
// muncul di antrean integrator dengan berkas yang belum tentu jadi — dan
// integrator yang mengunduhnya menerima 404 atas konten yang katanya siap.
// Kalau barisnya yang gagal, berkasnya dibuang lagi di sini.
func (s *ContentService) Upload(ctx context.Context, actor string, in models.NewContent,
	src io.Reader) (models.Content, error) {

	if err := validateUpload(in); err != nil {
		return models.Content{}, logError(ctx, s.logger, "ContentService.Upload", actor, err)
	}

	in.ID = models.NewID()
	in.UploadedBy = actor

	size, checksum, err := s.files.Save(in.ID, src)
	if err != nil {
		return models.Content{}, logError(ctx, s.logger, "ContentService.Upload", actor,
			httpx.ServerError(err))
	}
	in.Size, in.Checksum = size, checksum

	// Berkas kosong berarti unggahannya putus sebelum satu byte pun sampai.
	// Diperiksa sesudah menulis, bukan sebelum, karena ukurannya baru diketahui
	// setelah seluruh isinya mengalir.
	if size == 0 {
		_ = s.files.Remove(in.ID)
		return models.Content{}, logError(ctx, s.logger, "ContentService.Upload", actor,
			httpx.BadRequest("The uploaded file is empty."))
	}

	created, err := run(ctx, s.base, "ContentService.Upload", actor,
		func(ctx context.Context) (models.Content, error) {
			return s.repo.Insert(ctx, in)
		})
	if err != nil {
		_ = s.files.Remove(in.ID)
		return models.Content{}, err
	}
	return created, nil
}

func validateUpload(in models.NewContent) error {
	if strings.TrimSpace(in.Filename) == "" {
		return httpx.BadRequest("The file has no name.")
	}
	if in.ScheduledAt.IsZero() {
		return httpx.BadRequest("Field 'scheduled_at' is required, in 'YYYY-MM-DD HH:MM:SS' server time.")
	}
	return nil
}

// List mengembalikan satu halaman beserta jumlah seluruhnya.
//
// Keduanya diambil di dalam **satu transaksi**. Kalau dipisah, unggahan yang
// masuk di antara keduanya membuat total tidak cocok dengan isinya — dan panel
// menampilkan jumlah halaman yang salah tanpa ada galat yang muncul.
func (s *ContentService) List(ctx context.Context, actor string,
	filter models.ContentFilter) (models.ContentPage, error) {

	filter.Now = s.now()
	filter.Limit, filter.Offset = pageLimits(filter.Limit, filter.Offset)

	return run(ctx, s.base, "ContentService.List", actor,
		func(ctx context.Context) (models.ContentPage, error) {
			items, err := s.repo.List(ctx, filter)
			if err != nil {
				return models.ContentPage{}, err
			}

			total, err := s.repo.Count(ctx, filter)
			if err != nil {
				return models.ContentPage{}, err
			}

			return models.ContentPage{
				Items:  items,
				Total:  total,
				Limit:  filter.Limit,
				Offset: filter.Offset,
			}, nil
		})
}

// pageLimits menjaga permintaan yang tidak masuk akal tidak menarik seluruh
// tabel ke memori.
//
// Batas atasnya 200 karena yang memanggil `?scope=due` tiap beberapa menit
// tidak pernah butuh lebih; yang butuh lebih sedang mengekspor, dan ekspor
// boleh memanggil beberapa kali.
func pageLimits(limit, offset int) (int, int) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}

func (s *ContentService) Get(ctx context.Context, actor, id string) (models.Content, error) {
	return run(ctx, s.base, "ContentService.Get", actor,
		func(ctx context.Context) (models.Content, error) {
			return s.repo.Get(ctx, id)
		})
}

// File menyebut letak berkasnya beserta keterangan yang dipakai handler saat
// menyajikannya.
//
// Service tidak menyajikan berkasnya sendiri — di sini tidak ada
// http.ResponseWriter, dan itu batas yang tidak dilanggar demi kemudahan.
func (s *ContentService) File(ctx context.Context, actor, id string) (models.Content, string, error) {
	content, err := s.Get(ctx, actor, id)
	if err != nil {
		return models.Content{}, "", err
	}
	return content, s.files.Path(content.ID), nil
}

// Claim mengunci satu konten untuk satu aplikasi.
//
// Yang kalah lomba menerima `already_claimed`, bukan galat. Itu keadaan yang
// memang dirancang terjadi, dan yang perlu dilakukan pemanggil cuma mengambil
// konten berikutnya.
func (s *ContentService) Claim(ctx context.Context, actor, id string) (models.Content, error) {
	return run(ctx, s.base, "ContentService.Claim", actor,
		func(ctx context.Context) (models.Content, error) {
			won, err := s.repo.Claim(ctx, id, actor, s.now())
			if err != nil {
				return models.Content{}, err
			}
			if !won {
				// Bedakan "tidak ada" dari "sudah diambil": yang pertama berarti
				// integrator salah id, yang kedua berarti dia cuma kalah cepat.
				if _, err := s.repo.Get(ctx, id); err != nil {
					return models.Content{}, err
				}
				return models.Content{}, httpx.AlreadyClaimed()
			}
			return s.repo.Get(ctx, id)
		})
}

// Publish mencatat hasil akhir yang dilaporkan integrator.
//
// Alasan gagal wajib diisi. Antrean berisi konten gagal tanpa keterangan tidak
// bisa ditindaklanjuti siapa pun — dan yang menulis integrasinya nanti akan
// menebak-nebak platform mana yang menolak.
func (s *ContentService) Publish(ctx context.Context, actor, id string, published bool,
	reason string) (models.Content, error) {

	status := models.StatusPublished
	var storedReason *string

	if !published {
		status = models.StatusFailed
		if strings.TrimSpace(reason) == "" {
			return models.Content{}, logError(ctx, s.logger, "ContentService.Publish", actor,
				httpx.BadRequest("Field 'reason' is required when reporting a failure."))
		}
		storedReason = &reason
	}

	return run(ctx, s.base, "ContentService.Publish", actor,
		func(ctx context.Context) (models.Content, error) {
			if err := s.repo.SetPublication(ctx, id, status, storedReason, s.now()); err != nil {
				return models.Content{}, err
			}
			return s.repo.Get(ctx, id)
		})
}

// Reschedule memindahkan jadwal, sekaligus mengembalikan kontennya ke antrean.
func (s *ContentService) Reschedule(ctx context.Context, actor, id string,
	at models.Time) (models.Content, error) {

	if at.IsZero() {
		return models.Content{}, logError(ctx, s.logger, "ContentService.Reschedule", actor,
			httpx.BadRequest("Field 'scheduled_at' is required, in 'YYYY-MM-DD HH:MM:SS' server time."))
	}

	return run(ctx, s.base, "ContentService.Reschedule", actor,
		func(ctx context.Context) (models.Content, error) {
			if err := s.repo.SetSchedule(ctx, id, at); err != nil {
				return models.Content{}, err
			}
			return s.repo.Get(ctx, id)
		})
}

// Archive mengeluarkan konten dari antrean.
//
// Berkasnya sengaja tidak ikut dibuang: mengarsipkan adalah keputusan yang
// sering diubah pikiran, dan berkas yang sudah terhapus tidak bisa
// dikembalikan. Yang membuang berkas nanti pekerjaan sapuan tersendiri, kalau
// memang disknya menipis.
func (s *ContentService) Archive(ctx context.Context, actor, id string) (models.Content, error) {
	return run(ctx, s.base, "ContentService.Archive", actor,
		func(ctx context.Context) (models.Content, error) {
			if err := s.repo.Archive(ctx, id, s.now()); err != nil {
				return models.Content{}, err
			}
			return s.repo.Get(ctx, id)
		})
}
