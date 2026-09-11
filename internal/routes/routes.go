// Package routes memasang seluruh alamat ke satu mux, dan membungkusnya dengan
// middleware.
//
// Handler tidak tahu alamatnya sendiri, dan berkas ini tidak tahu isi handler
// mana pun. Yang menyambungkan keduanya cuma daftar di bawah — jadi seluruh
// permukaan HTTP servis ini bisa dibaca dalam satu folder.
package routes

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/gemgum/stock-engine/internal/handlers"
	"github.com/gemgum/stock-engine/pkg/httpx"
	"github.com/gemgum/stock-engine/pkg/middleware"
	"github.com/gemgum/stock-engine/pkg/ratelimit"
)

type Options struct {
	Logger *slog.Logger

	// Auth menerima kunci induk maupun kunci aplikasi. AdminAuth cuma menerima
	// kunci induk — dua antarmuka, bukan satu dengan penanda peran, supaya yang
	// membaca daftar alamat langsung melihat mana yang terbuka untuk integrator.
	Auth      httpx.Authenticator
	AdminAuth httpx.Authenticator

	AllowedOrigins []string

	// Alive dipanggil /healthz. Nil berarti kesehatannya tidak diperiksa —
	// dipakai uji yang merakit router tanpa database.
	Alive func(ctx context.Context) error

	Content *handlers.ContentHandler
	App     *handlers.AppHandler
}

// Batas permintaan.
//
// Angkanya untuk beberapa aplikasi integrator yang menanyakan antrean tiap
// beberapa menit, bukan untuk API publik. Enam puluh per menit jauh di atas
// pemakaian wajar dan tetap menghentikan skrip yang menembak beruntun.
const (
	generalBurst     = 60
	generalPerMinute = 60
)

func NewRouter(opts Options) http.Handler {
	mux := http.NewServeMux()

	general := ratelimit.New(generalBurst, generalPerMinute)
	sweep(general)

	open := func(fn httpx.Route) http.HandlerFunc {
		return httpx.Wrap(opts.Logger, fn)
	}
	guard := func(fn httpx.Route) http.HandlerFunc {
		return httpx.Guard(opts.Logger, opts.Auth, fn)
	}
	admin := func(fn httpx.Route) http.HandlerFunc {
		return httpx.Guard(opts.Logger, opts.AdminAuth, fn)
	}

	healthz(mux, open, opts.Alive)

	Content(mux, guard, opts.Content)
	App(mux, guard, admin, opts.App)

	// Urutannya dari luar ke dalam: yang pertama disebut yang pertama menyentuh
	// permintaan. Recover paling luar supaya panic di middleware mana pun ikut
	// tertangkap; pembatas laju sebelum apa pun yang menyentuh database.
	return middleware.Chain(mux,
		middleware.Recover(opts.Logger),
		middleware.CORS(opts.AllowedOrigins),
		middleware.Log(opts.Logger),
		middleware.RateLimit(general, opts.Logger),
	)
}

// healthz ikut menyentuh database.
//
// Alamat yang cuma membalas "hidup" tanpa memeriksa apa pun akan tetap bilang
// sehat saat databasenya rusak — padahal saat itu tidak ada satu pun permintaan
// yang bisa dilayani.
func healthz(mux *http.ServeMux, open func(httpx.Route) http.HandlerFunc,
	alive func(context.Context) error) {

	mux.HandleFunc("GET /healthz", open(func(w http.ResponseWriter, r *http.Request) error {
		if alive != nil {
			ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
			defer cancel()

			if err := alive(ctx); err != nil {
				return httpx.DatabaseError(err)
			}
		}
		httpx.OK(w, "Service is running.", nil)
		return nil
	}))
}

// sweep membuang hitungan alamat yang sudah lama tidak datang.
//
// Tanpa ini map-nya tumbuh selamanya, dan pemindai porta meninggalkan ribuan
// baris yang tidak akan pernah dipakai lagi.
func sweep(limiters ...*ratelimit.Limiter) {
	go func() {
		for range time.Tick(10 * time.Minute) {
			for _, l := range limiters {
				l.Sweep(30 * time.Minute)
			}
		}
	}()
}
