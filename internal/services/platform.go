package services

import (
	"context"
	"log/slog"
	"net/http"
	"regexp"

	"github.com/gemgum/stock-engine/internal/database"
	"github.com/gemgum/stock-engine/internal/models"
	"github.com/gemgum/stock-engine/pkg/httpx"
)

// PlatformService mengurus daftar nilai yang boleh ditulis di `platform`.
//
// Ada supaya salah ketik ditolak saat unggah, bukan ketahuan saat kontennya
// tidak pernah muncul di `?platform=`.
type PlatformService struct {
	base
	repo models.PlatformRepository
	now  func() models.Time
}

func NewPlatformService(db database.Runner, repo models.PlatformRepository,
	logger *slog.Logger) *PlatformService {

	return &PlatformService{base: base{db: db, logger: logger}, repo: repo, now: models.Now}
}

// platformName sengaja sempit: huruf kecil, angka, dan tanda hubung.
//
// Kalau `Instagram` dan `instagram` boleh sama-sama didaftarkan, salah ketik
// yang ingin dicegah tabel ini cuma pindah tempat.
var platformName = regexp.MustCompile(`^[a-z0-9-]{1,32}$`)

func (s *PlatformService) List(ctx context.Context, actor string) ([]models.Platform, error) {
	return run(ctx, s.base, "PlatformService.List", actor,
		func(ctx context.Context) ([]models.Platform, error) {
			return s.repo.List(ctx)
		})
}

// Create mendaftarkan platform, atau mengaktifkan lagi yang diarsipkan.
func (s *PlatformService) Create(ctx context.Context, actor, name string) (models.Platform, error) {
	if !platformName.MatchString(name) {
		return models.Platform{}, logError(ctx, s.logger, "PlatformService.Create", actor,
			httpx.BadRequest("Field 'name' must be 1-32 lowercase letters, digits, or '-'."))
	}

	return run(ctx, s.base, "PlatformService.Create", actor,
		func(ctx context.Context) (models.Platform, error) {
			return s.repo.Upsert(ctx, name, s.now())
		})
}

// Archive menutup platform untuk unggahan baru.
//
// Konten yang sudah memakainya tidak disentuh: yang terjadwal tetap tayang,
// dan `?platform=` tetap bisa menemukannya.
func (s *PlatformService) Archive(ctx context.Context, actor, name string) (models.Platform, error) {
	return run(ctx, s.base, "PlatformService.Archive", actor,
		func(ctx context.Context) (models.Platform, error) {
			if _, err := s.repo.Get(ctx, name); err != nil {
				return models.Platform{}, err
			}
			if err := s.repo.SetArchived(ctx, name, s.now()); err != nil {
				return models.Platform{}, err
			}
			return s.repo.Get(ctx, name)
		})
}

// checkPlatform menolak nama yang tidak terdaftar, dan — kalau forUpload —
// yang sudah diarsipkan.
//
// Dipanggil di dalam transaksi pemanggilnya. Satu fungsi untuk unggah dan
// saringan, supaya keduanya tidak pernah berbeda pendapat soal nama mana yang
// sah.
func checkPlatform(ctx context.Context, repo models.PlatformRepository, name string,
	forUpload bool) error {

	p, err := repo.Get(ctx, name)
	if err != nil {
		if httpx.From(err).HTTPStatus() == http.StatusNotFound {
			return httpx.BadRequest("Unknown platform '" + name + "'. See GET /v1/platforms.")
		}
		return err
	}
	if forUpload && p.ArchivedAt != nil {
		return httpx.BadRequest("Platform '" + name + "' is archived. See GET /v1/platforms.")
	}
	return nil
}
