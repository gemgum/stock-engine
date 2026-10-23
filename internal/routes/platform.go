package routes

import (
	"net/http"

	"github.com/gemgum/stock-engine/internal/handlers"
	"github.com/gemgum/stock-engine/pkg/httpx"
)

// Platform mendaftarkan alamat milik domain ini.
//
// Membaca daftarnya terbuka untuk integrator mana pun — mereka butuh tahu nama
// yang sah sebelum mengunggah. Mengubahnya cuma kunci induk: satu integrator
// yang menambah nama salah ketik merusak saringan semua yang lain.
//
// Tidak ada DELETE (ARCHITECTURE.md §4). "Menghapus" adalah `/archival`, dan
// mendaftarkan nama yang sama lagi mengaktifkannya kembali.
func Platform(mux *http.ServeMux, guard, admin func(httpx.Route) http.HandlerFunc, h *handlers.PlatformHandler) {
	mux.HandleFunc("GET /v1/platforms", guard(h.List))
	mux.HandleFunc("POST /v1/platforms", admin(h.Create))
	mux.HandleFunc("POST /v1/platforms/{name}/archival", admin(h.Archive))
}
