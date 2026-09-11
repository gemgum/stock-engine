package services

import (
	"context"
	"log/slog"
	"strings"

	"github.com/gemgum/stock-engine/internal/database"
	"github.com/gemgum/stock-engine/internal/models"
	"github.com/gemgum/stock-engine/pkg/httpx"
	"github.com/gemgum/stock-engine/pkg/security"
)

// AppService mengurus siapa yang boleh memanggil servis ini.
//
// Dua tingkat, dan cuma dua: kunci induk yang boleh mendaftarkan aplikasi, dan
// kunci aplikasi yang boleh menyentuh konten. Tingkat ketiga baru layak ada
// kalau nanti benar-benar ada peran yang berbeda hak lihatnya.
type AppService struct {
	base
	repo models.AppRepository

	// adminKey datang dari environment, bukan dari database, supaya selalu ada
	// jalan masuk saat belum ada satu pun aplikasi terdaftar.
	adminKey string

	now func() models.Time
}

func NewAppService(db database.Runner, repo models.AppRepository, adminKey string,
	logger *slog.Logger) *AppService {

	return &AppService{
		base:     base{db: db, logger: logger},
		repo:     repo,
		adminKey: adminKey,
		now:      models.Now,
	}
}

// Authenticate menerima kunci induk maupun kunci aplikasi.
//
// Dipakai seluruh alamat konten. Kunci induk ikut diterima supaya gudang bisa
// diisi dan diperiksa sebelum ada satu pun integrator — dan supaya Postman tidak
// perlu kunci kedua.
func (s *AppService) Authenticate(ctx context.Context, key string) (string, error) {
	if security.Equal(key, s.adminKey) {
		return models.AdminID, nil
	}

	app, err := database.Query(ctx, s.db, func(ctx context.Context) (models.App, error) {
		return s.repo.FindActiveByHash(ctx, security.Hash(key))
	})
	if err != nil {
		// Kunci yang tidak ketemu dijawab sama dengan kunci yang salah bentuk.
		// Membedakan keduanya cuma berguna bagi yang sedang menebak-nebak.
		return "", httpx.InvalidKey()
	}
	return app.ID, nil
}

// AuthenticateAdmin cuma menerima kunci induk.
//
// Dipasang di alamat `/v1/apps`. Aplikasi integrator yang bisa mendaftarkan
// aplikasi baru berarti kunci yang bocor tidak bisa benar-benar dicabut — yang
// memegangnya tinggal menerbitkan kunci lain untuk dirinya sendiri.
func (s *AppService) AuthenticateAdmin(_ context.Context, key string) (string, error) {
	if !security.Equal(key, s.adminKey) {
		return "", httpx.InvalidKey()
	}
	return models.AdminID, nil
}

func (s *AppService) List(ctx context.Context, actor string) ([]models.App, error) {
	return run(ctx, s.base, "AppService.List", actor,
		func(ctx context.Context) ([]models.App, error) {
			return s.repo.List(ctx)
		})
}

// Create menerbitkan kunci baru, dan **ini satu-satunya saat kuncinya
// terlihat.**
//
// Yang disimpan cuma hashnya. Kalau kuncinya hilang, tidak ada cara
// memulihkannya — aplikasinya dicabut lalu didaftarkan lagi.
func (s *AppService) Create(ctx context.Context, actor, name string) (models.CreatedApp, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return models.CreatedApp{}, logError(ctx, s.logger, "AppService.Create", actor,
			httpx.BadRequest("Field 'name' is required."))
	}

	key, hash, err := security.NewKey()
	if err != nil {
		return models.CreatedApp{}, logError(ctx, s.logger, "AppService.Create", actor,
			httpx.ServerError(err))
	}

	app, err := run(ctx, s.base, "AppService.Create", actor,
		func(ctx context.Context) (models.App, error) {
			return s.repo.Insert(ctx, name, hash, s.now())
		})
	if err != nil {
		return models.CreatedApp{}, err
	}
	return models.CreatedApp{App: app, Key: key}, nil
}

// Revoke mencabut kunci sebuah aplikasi.
//
// Kontennya tidak ikut hilang: gudangnya satu, dan konten yang sudah diunggah
// tetap milik semua orang. Yang berhenti cuma kemampuannya memanggil.
func (s *AppService) Revoke(ctx context.Context, actor, id string) (models.App, error) {
	return run(ctx, s.base, "AppService.Revoke", actor,
		func(ctx context.Context) (models.App, error) {
			if _, err := s.repo.Get(ctx, id); err != nil {
				return models.App{}, err
			}
			if err := s.repo.SetRevoked(ctx, id, s.now()); err != nil {
				return models.App{}, err
			}
			return s.repo.Get(ctx, id)
		})
}

// Me menjawab "siapa saya" untuk pemanggil mana pun.
//
// Pemegang kunci induk tidak punya baris di tabel apps, jadi jawabannya disusun
// di sini. Tanpa itu `/v1/me` akan menjawab 404 untuk kunci yang justru paling
// berhak.
func (s *AppService) Me(ctx context.Context, actor string) (models.App, error) {
	if actor == models.AdminID {
		return models.App{ID: models.AdminID, Name: "Admin key"}, nil
	}

	return run(ctx, s.base, "AppService.Me", actor,
		func(ctx context.Context) (models.App, error) {
			return s.repo.Get(ctx, actor)
		})
}
