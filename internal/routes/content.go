package routes

import (
	"net/http"

	"github.com/gemgum/stock-engine/internal/handlers"
	"github.com/gemgum/stock-engine/pkg/httpx"
)

// Content mendaftarkan alamat milik domain ini.
//
// Gudang konten: unggah, lihat antrean, ambil berkasnya, dan lapor hasilnya.
// Semuanya terbuka untuk aplikasi integrator mana pun — gudangnya satu.
func Content(mux *http.ServeMux, guard func(httpx.Route) http.HandlerFunc, h *handlers.ContentHandler) {
	mux.HandleFunc("POST /v1/contents", guard(h.Upload))
	mux.HandleFunc("GET /v1/contents", guard(h.List))
	mux.HandleFunc("GET /v1/contents/{id}", guard(h.Get))
	mux.HandleFunc("GET /v1/contents/{id}/file", guard(h.File))

	// Kata benda, bukan kata kerja — lihat ARCHITECTURE.md §4.
	mux.HandleFunc("POST /v1/contents/{id}/schedule", guard(h.Reschedule))
	mux.HandleFunc("POST /v1/contents/{id}/claim", guard(h.Claim))
	mux.HandleFunc("POST /v1/contents/{id}/publication", guard(h.Publish))
	mux.HandleFunc("POST /v1/contents/{id}/archival", guard(h.Archive))
}
