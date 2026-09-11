package services

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"time"

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

	// retention adalah umur berkas yang sudah selesai sebelum boleh dibuang.
	// Nol berarti tidak pernah dibuang.
	retention time.Duration

	// claimTimeout adalah berapa lama konten boleh dipegang tanpa kabar sebelum
	// claimnya dilepas. Nol berarti tidak pernah dilepas.
	claimTimeout time.Duration

	// now bisa diganti saat pengujian. Seluruh servis ini soal waktu, dan
	// menguji "sudah waktunya" dengan menunggu jam berganti bukan pengujian.
	now func() models.Time
}

func NewContentService(db database.Runner, repo models.ContentRepository,
	files models.Files, retention, claimTimeout time.Duration,
	logger *slog.Logger) *ContentService {

	return &ContentService{
		base:         base{db: db, logger: logger},
		repo:         repo,
		files:        files,
		retention:    retention,
		claimTimeout: claimTimeout,
		now:          models.Now,
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

	// Berkas yang dibuang karena umur dijawab dengan sebabnya, bukan 404 polos.
	// Tanpa itu integrator mengira ada yang rusak dan mencoba lagi berkali-kali
	// atas berkas yang memang sudah tidak ada.
	if content.FileRemovedAt != nil {
		return models.Content{}, "", logError(ctx, s.logger, "ContentService.File", actor,
			httpx.NotFound("The file was removed after its retention period. Its record is still here."))
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
				// Bedakan "tidak ada" dari "tidak bisa diambil": yang pertama
				// berarti integrator salah id, yang kedua berarti kontennya sudah
				// bukan miliknya lagi.
				content, err := s.repo.Get(ctx, id)
				if err != nil {
					return models.Content{}, err
				}
				return models.Content{}, notClaimable(content.Status)
			}
			return s.repo.Get(ctx, id)
		})
}

// notClaimable menyebut keadaan yang sebenarnya.
//
// Golongannya tetap satu — tindakan pemanggil sama untuk semuanya: lewati,
// ambil konten berikutnya. Yang dibedakan kalimatnya, karena "sudah diambil
// aplikasi lain" untuk konten yang sebenarnya sudah tayang membuat yang membaca
// log mencari aplikasi yang tidak pernah ada.
func notClaimable(status models.Status) error {
	switch status {
	case models.StatusClaimed:
		return httpx.AlreadyClaimed("This content is already claimed by another app.")
	case models.StatusPublished:
		return httpx.AlreadyClaimed("This content has already been published.")
	case models.StatusArchived:
		return httpx.AlreadyClaimed("This content has been archived.")
	default:
		return httpx.AlreadyClaimed("This content is not waiting to be claimed (status: " +
			string(status) + ").")
	}
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
			recorded, err := s.repo.SetPublication(ctx, id, actor, status, storedReason, s.now())
			if err != nil {
				return models.Content{}, err
			}
			if !recorded {
				return models.Content{}, s.whyNotReportable(ctx, id)
			}
			return s.repo.Get(ctx, id)
		})
}

// whyNotReportable menjelaskan laporan yang ditolak.
//
// Dipanggil **hanya** saat laporannya gagal, jadi kueri tambahan ini tidak
// membebani jalur yang normal. Sebabnya dipisah karena yang perlu dilakukan
// pemanggil berbeda: yang belum mengclaim harus mengclaim dulu, sedangkan yang
// kalah lomba harus berhenti — kontennya sedang dikerjakan orang lain, dan
// memposting terus berarti tayang dua kali.
func (s *ContentService) whyNotReportable(ctx context.Context, id string) error {
	content, err := s.repo.Get(ctx, id)
	if err != nil {
		return err
	}

	if content.Status == models.StatusClaimed {
		return httpx.AlreadyClaimed("This content is claimed by another app.")
	}
	return httpx.BadRequest("Only claimed content can be reported as published or failed.")
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
			moved, err := s.repo.SetSchedule(ctx, id, at)
			if err != nil {
				return models.Content{}, err
			}
			if !moved {
				// Get menjawab 404 kalau kontennya memang tidak ada; kalau ada,
				// tersisa dua sebab, dan keduanya perlu kalimatnya sendiri.
				content, err := s.repo.Get(ctx, id)
				if err != nil {
					return models.Content{}, err
				}
				if content.FileRemovedAt != nil {
					return models.Content{}, httpx.BadRequest(
						"The file was removed after its retention period, so this content cannot be scheduled again.")
				}
				return models.Content{}, httpx.BadRequest("Published content cannot be rescheduled.")
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

// purgeBatch adalah berapa konten yang diambil sekali kueri.
//
// Dibatasi supaya satu transaksi tidak menahan kunci tulis berlama-lama —
// SQLite cuma punya satu penulis, dan unggahan yang datang saat sapuan berjalan
// harus tetap terlayani. Satu sapuan mengulang batch sampai habis, jadi ini
// **bukan** batas berapa yang bisa dibersihkan per hari: gudang yang sudah lama
// menumpuk tetap bersih dalam satu jalan.
const purgeBatch = 200

// staleUploadAge adalah umur berkas sementara sebelum dianggap ditinggalkan.
//
// Enam jam, dan angkanya longgar dengan sengaja: unggahan 200MB di jaringan
// pelan bisa memakan puluhan menit, dan membuang berkas milik unggahan yang
// masih berjalan jauh lebih buruk daripada membiarkan sampah menginap setengah
// hari.
const staleUploadAge = 6 * time.Hour

// PurgeFiles membuang berkas konten yang sudah selesai dan lewat umurnya.
//
// Yang dibuang cuma berkasnya; barisnya tetap, lengkap dengan riwayatnya.
//
// Urutannya berkas dulu, penandanya belakangan. Kalau servis mati di antara
// keduanya, berkasnya sudah hilang tapi belum bertanda — sapuan berikutnya
// mencoba menghapus berkas yang memang sudah tidak ada (tidak dianggap galat)
// lalu menandainya. Dibalik, berkas yang gagal dihapus akan bertanda "sudah
// dibuang" dan tidak akan pernah dilirik lagi.
func (s *ContentService) PurgeFiles(ctx context.Context) (int, error) {
	if s.retention <= 0 {
		return 0, nil
	}

	total := 0

	// Berhenti kalau servis diminta mati. Yang belum sempat disapu ikut sapuan
	// berikutnya — pekerjaan ini memang aman diulang.
	for ctx.Err() == nil {
		batas := models.Time{Time: s.now().Add(-s.retention)}

		expired, err := run(ctx, s.base, "ContentService.PurgeFiles", models.AdminID,
			func(ctx context.Context) ([]models.Content, error) {
				return s.repo.ExpiredFiles(ctx, batas, purgeBatch)
			})
		if err != nil {
			return total, err
		}
		if len(expired) == 0 {
			break
		}

		dibuang, err := s.purgeOne(ctx, expired)
		total += dibuang
		if err != nil {
			return total, err
		}

		// Berhenti kalau ada yang tidak jadi dibuang. Baris yang gagal dihapus
		// tidak bertanda, jadi kueri berikutnya mengembalikannya lagi — dan
		// tanpa penjagaan ini sapuannya berputar di baris yang sama selamanya.
		if dibuang < len(expired) {
			break
		}
		if len(expired) < purgeBatch {
			break
		}
	}

	if total > 0 {
		s.logger.Info("sapuan berkas selesai", "dibuang", total,
			"umur_hari", int(s.retention.Hours()/24))
	}
	return total, nil
}

func (s *ContentService) purgeOne(ctx context.Context, expired []models.Content) (int, error) {
	dibuang := 0

	for _, c := range expired {
		if ctx.Err() != nil {
			return dibuang, nil
		}

		if err := s.files.Remove(c.ID); err != nil {
			// Satu berkas yang gagal dihapus tidak boleh menghentikan sisanya.
			s.logger.Error("berkas gagal dibuang", "op", "ContentService.PurgeFiles",
				"content", c.ID, "error", err.Error())
			continue
		}

		_, err := run(ctx, s.base, "ContentService.PurgeFiles", models.AdminID,
			func(ctx context.Context) (struct{}, error) {
				return struct{}{}, s.repo.MarkFileRemoved(ctx, c.ID, s.now())
			})
		if err != nil {
			return dibuang, err
		}
		dibuang++
	}
	return dibuang, nil
}

// PurgeStaleUploads membuang berkas separuh yang ditinggalkan servis yang mati
// mendadak.
//
// Terpisah dari PurgeFiles, dan **tidak ikut dimatikan oleh RETENTION_DAYS=0**:
// yang ini bukan kebijakan penyimpanan melainkan sampah. Yang memilih menyimpan
// kontennya selamanya tetap tidak ingin menyimpan potongan unggahan yang gagal.
func (s *ContentService) PurgeStaleUploads(ctx context.Context) (int, error) {
	dibuang, err := s.files.RemoveStale(staleUploadAge)
	if err != nil {
		return 0, httpx.ServerError(err)
	}

	if dibuang > 0 {
		s.logger.Info("berkas sementara yang ditinggalkan dibuang", "jumlah", dibuang)
	}
	return dibuang, nil
}

// ReleaseStuckClaims mengembalikan konten yang diclaim lalu ditinggalkan.
//
// Integrator yang mati sesudah mengclaim membuat kontennya diam di `claimed`
// selamanya: tidak pernah tayang, dan tidak pernah muncul lagi di antrean kerja
// karena antrean itu memang sengaja menyembunyikan yang sedang dipegang orang.
//
// Yang dilepas dicatat di log. Konten yang berulang kali dilepas berarti ada
// integrator yang selalu mati di tempat yang sama, dan itu tidak akan ketahuan
// dari mana pun kecuali dari baris ini.
func (s *ContentService) ReleaseStuckClaims(ctx context.Context) (int, error) {
	if s.claimTimeout <= 0 {
		return 0, nil
	}

	batas := models.Time{Time: s.now().Add(-s.claimTimeout)}

	dilepas, err := run(ctx, s.base, "ContentService.ReleaseStuckClaims", models.AdminID,
		func(ctx context.Context) (int, error) {
			return s.repo.ReleaseStuckClaims(ctx, batas)
		})
	if err != nil {
		return 0, err
	}

	if dilepas > 0 {
		s.logger.Info("claim tersangkut dilepas", "jumlah", dilepas,
			"ambang_jam", int(s.claimTimeout.Hours()))
	}
	return dilepas, nil
}
