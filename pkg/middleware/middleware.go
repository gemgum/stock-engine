// Package middleware membungkus setiap permintaan dengan hal-hal yang tidak
// ada hubungannya dengan konten: pemulihan dari panic, CORS, log, dan pembatas
// laju.
package middleware

import (
	"log/slog"
	"net"
	"net/http"
	"slices"
	"time"

	"github.com/gemgum/stock-engine/pkg/httpx"
	"github.com/gemgum/stock-engine/pkg/ratelimit"
)

type Middleware func(http.Handler) http.Handler

// Chain membungkus handler dari luar ke dalam: yang pertama disebut yang
// pertama menyentuh permintaan.
func Chain(h http.Handler, list ...Middleware) http.Handler {
	for i := len(list) - 1; i >= 0; i-- {
		h = list[i](h)
	}
	return h
}

// Recover menangkap panic supaya satu bug tidak mematikan seluruh servis.
//
// Yang sampai ke pemanggil tetap amplop yang sama seperti galat lain; jejak
// panic-nya berhenti di log.
func Recover(logger *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if panik := recover(); panik != nil {
					logger.Error("panic", "path", r.URL.Path, "panic", panik)
					httpx.Fail(w, logger, httpx.ServerError(nil))
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// CORS mengizinkan alamat yang disebut saja.
//
// Daftarnya keputusan aplikasi, jadi datang sebagai parameter dari
// internal/config — bukan konstanta di sini.
func CORS(allowed []string) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin != "" && slices.Contains(allowed, origin) {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
				w.Header().Set("Vary", "Origin")
			}
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// pencatat menahan angka status supaya bisa ikut dicatat setelah handler
// selesai.
type pencatat struct {
	http.ResponseWriter
	status int
}

func (p *pencatat) WriteHeader(status int) {
	p.status = status
	p.ResponseWriter.WriteHeader(status)
}

// Log mencatat satu baris per permintaan: alamat apa, berapa lama, status
// berapa.
//
// Isi request tidak pernah ikut. Di servis ini yang lewat adalah berkas konten
// dan kunci API, dan log adalah tempat paling gampang untuk membocorkan
// keduanya tanpa sadar.
func Log(logger *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mulai := time.Now()
			p := &pencatat{ResponseWriter: w, status: http.StatusOK}

			next.ServeHTTP(p, r)

			logger.Info("request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", p.status,
				"ms", time.Since(mulai).Milliseconds(),
			)
		})
	}
}

// RateLimit menolak pemanggil yang datang terlalu sering.
//
// Dihitung per alamat IP, bukan per kunci API: yang menebak-nebak kunci justru
// belum punya kunci yang sah.
func RateLimit(limiter *ratelimit.Limiter, logger *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !limiter.Allow(pemanggil(r)) {
				httpx.Fail(w, logger, httpx.TooManyRequests())
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func pemanggil(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
