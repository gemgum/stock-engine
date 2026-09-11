package routes

import (
	"net/http"

	"github.com/gemgum/stock-engine/internal/handlers"
	"github.com/gemgum/stock-engine/pkg/httpx"
)

// App mendaftarkan alamat milik domain ini.
//
// Pendaftaran dan pencabutan aplikasi cuma untuk pemegang kunci induk. Aplikasi
// yang bisa mendaftarkan aplikasi baru berarti kunci yang bocor tidak bisa
// benar-benar dicabut — yang memegangnya tinggal menerbitkan kunci lain.
func App(mux *http.ServeMux, guard, admin func(httpx.Route) http.HandlerFunc, h *handlers.AppHandler) {
	mux.HandleFunc("GET /v1/apps", admin(h.List))
	mux.HandleFunc("POST /v1/apps", admin(h.Create))
	mux.HandleFunc("POST /v1/apps/{id}/revocation", admin(h.Revoke))

	// Satu-satunya alamat domain ini yang boleh dipanggil integrator biasa.
	mux.HandleFunc("GET /v1/me", guard(h.Me))
}
