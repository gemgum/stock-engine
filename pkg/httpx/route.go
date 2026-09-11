package httpx

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
)

// Route adalah bentuk semua handler di servis ini: mengembalikan galat, bukan
// menuliskannya sendiri.
//
// Ini yang menghapus pengulangan penanganan galat di tiap handler. Satu tempat
// yang menuliskan galat berarti satu tempat yang bisa keliru membocorkan
// isinya — dan tempat itu sudah diperiksa.
type Route func(http.ResponseWriter, *http.Request) error

// Wrap dipakai untuk alamat yang memang terbuka tanpa kunci.
func Wrap(logger *slog.Logger, fn Route) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := fn(w, r); err != nil {
			Fail(w, logger, err)
		}
	}
}

// Authenticator memeriksa kunci dan mengembalikan id pemiliknya.
//
// Antarmuka sekecil ini supaya lapisan HTTP tidak perlu tahu apa pun tentang
// cara kunci disimpan — dan supaya domain yang menerbitkan kunci tidak perlu
// tahu apa pun tentang HTTP.
type Authenticator interface {
	Authenticate(ctx context.Context, key string) (string, error)
}

type contextKey struct{}

// Guard memeriksa kunci sebelum handler-nya jalan, lalu menitipkan id
// pemanggilnya di context.
//
// Semua rute kecuali healthz lewat sini. Handler yang lupa memeriksa kunci
// tidak mungkin ada, karena tidak ada handler yang memeriksanya sendiri.
func Guard(logger *slog.Logger, auth Authenticator, fn Route) http.HandlerFunc {
	return Wrap(logger, func(w http.ResponseWriter, r *http.Request) error {
		raw, ok := bearerKey(r)
		if !ok {
			return InvalidKey()
		}

		callerID, err := auth.Authenticate(r.Context(), raw)
		if err != nil {
			return err
		}

		return fn(w, r.WithContext(context.WithValue(r.Context(), contextKey{}, callerID)))
	})
}

// Actor mengembalikan id pemanggil. Selalu terisi di dalam handler yang
// dibungkus Guard.
func Actor(r *http.Request) string {
	callerID, _ := r.Context().Value(contextKey{}).(string)
	return callerID
}

func bearerKey(r *http.Request) (string, bool) {
	const prefix = "Bearer "
	header := r.Header.Get("Authorization")
	if !strings.HasPrefix(header, prefix) {
		return "", false
	}
	value := strings.TrimSpace(header[len(prefix):])
	return value, value != ""
}

// Decode membaca badan JSON dengan batas ukuran.
//
// Batasnya bukan kehati-hatian berlebihan: tanpa itu satu request bisa membuat
// servis menghabiskan seluruh memori. Yang tidak lewat sini cuma unggahan
// berkas — batasnya sendiri, dan jauh lebih besar.
func Decode[T any](w http.ResponseWriter, r *http.Request) (T, error) {
	var body T

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		return body, BadRequest("The request body could not be read.").WithCause(err)
	}
	return body, nil
}
