// Package middleware membungkus setiap permintaan dengan hal-hal yang tidak
// ada hubungannya dengan konten: pemulihan dari panic, CORS, log, dan pembatas
// laju.
package middleware

import (
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"slices"
	"strings"
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
				if recovered := recover(); recovered != nil {
					logger.Error("panic", "path", r.URL.Path, "panic", recovered)
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
type recorder struct {
	http.ResponseWriter
	status int
}

func (p *recorder) WriteHeader(status int) {
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
			p := &recorder{ResponseWriter: w, status: http.StatusOK}

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
//
// trusted adalah jaringan tempat proxy berada — biasanya cuma loopback, karena
// nginx berjalan di mesin yang sama. Daftarnya keputusan aplikasi, jadi datang
// sebagai parameter dari internal/config.
func RateLimit(limiter *ratelimit.Limiter, trusted []netip.Prefix,
	logger *slog.Logger) Middleware {

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !limiter.Allow(ClientIP(r, trusted)) {
				httpx.Fail(w, logger, httpx.TooManyRequests())
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// ClientIP menyebut siapa yang sebenarnya memanggil.
//
// Tanpa ini, servis di balik nginx melihat `127.0.0.1` untuk **semua**
// permintaan — dan seluruh integrator berbagi satu jatah. Satu aplikasi yang
// menembak beruntun mematikan yang lain, dan pembatasnya berhenti membatasi apa
// pun kecuali proxy-nya sendiri.
//
// Header alamat asli **cuma dipercaya kalau permintaannya datang dari proxy yang
// dipercaya.** Kalau tidak, siapa pun tinggal memasang `X-Real-IP` karangan
// sendiri dan mendapat jatah tak terbatas — pembatas yang bisa dipalsukan lebih
// buruk daripada tidak ada, karena ia menimbulkan rasa aman yang keliru.
func ClientIP(r *http.Request, trusted []netip.Prefix) string {
	remote := hostOf(r.RemoteAddr)

	alamat, err := netip.ParseAddr(remote)
	if err != nil || !dipercaya(alamat, trusted) {
		return remote
	}

	// nginx memasangnya dari $remote_addr, jadi isinya pemanggil yang sebenarnya.
	if ip := bersih(r.Header.Get("X-Real-IP")); ip != "" {
		return ip
	}

	// Sebagian proxy cuma memasang X-Forwarded-For. Isinya berurutan dari
	// pemanggil asli ke proxy terakhir, jadi yang dicari entri paling kanan yang
	// **bukan** proxy kita — itu yang menyerahkan permintaannya kepada kita.
	maju := strings.Split(r.Header.Get("X-Forwarded-For"), ",")
	for i := len(maju) - 1; i >= 0; i-- {
		ip := bersih(maju[i])
		if ip == "" {
			continue
		}
		if alamat, err := netip.ParseAddr(ip); err == nil && dipercaya(alamat, trusted) {
			continue
		}
		return ip
	}

	return remote
}

func bersih(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if _, err := netip.ParseAddr(raw); err != nil {
		return ""
	}
	return raw
}

func dipercaya(alamat netip.Addr, trusted []netip.Prefix) bool {
	for _, jaringan := range trusted {
		if jaringan.Contains(alamat) {
			return true
		}
	}
	return false
}

func hostOf(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return remoteAddr
	}
	return host
}
