// Package api merakit seluruh servis dari bagian-bagiannya.
//
// **Satu-satunya tempat yang tahu implementasi mana yang dipakai.** Lapisan di
// atasnya cuma mengenal antarmuka, jadi mengganti penyimpanan atau memakai
// tiruan saat pengujian cukup dilakukan di sini.
//
// Dipisah dari `routes` dengan sengaja: `routes` menjawab "alamat apa saja yang
// ada", berkas ini menjawab "siapa memakai siapa". Keduanya berubah karena
// alasan yang berbeda.
package api

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/gemgum/stock-engine/internal/config"
	"github.com/gemgum/stock-engine/internal/database"
	"github.com/gemgum/stock-engine/internal/handlers"
	"github.com/gemgum/stock-engine/internal/models"
	"github.com/gemgum/stock-engine/internal/repository"
	"github.com/gemgum/stock-engine/internal/routes"
	"github.com/gemgum/stock-engine/internal/services"
)

func Build(db *database.DB, files models.Files, cfg config.Config, logger *slog.Logger) http.Handler {
	appService := services.NewAppService(db, repository.App{}, cfg.AdminKey, logger)
	contentService := services.NewContentService(db, repository.Content{}, files, logger)

	return routes.NewRouter(routes.Options{
		Logger:         logger,
		Auth:           authenticator(appService.Authenticate),
		AdminAuth:      authenticator(appService.AuthenticateAdmin),
		AllowedOrigins: cfg.AllowedOrigins,
		Alive:          db.Ping,

		Content: handlers.NewContentHandler(contentService, cfg.MaxUploadBytes(), logger),
		App:     handlers.NewAppHandler(appService, logger),
	})
}

// authenticator membungkus satu method jadi httpx.Authenticator.
//
// Ada karena satu tipe cuma bisa punya satu method bernama Authenticate,
// sementara servis ini butuh dua pintu berbeda dari service yang sama.
type authenticator func(ctx context.Context, key string) (string, error)

func (f authenticator) Authenticate(ctx context.Context, key string) (string, error) {
	return f(ctx, key)
}
