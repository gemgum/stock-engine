package handlers

import (
	"log/slog"
	"net/http"

	"github.com/gemgum/stock-engine/internal/services"
	"github.com/gemgum/stock-engine/pkg/httpx"
)

type AppHandler struct {
	service *services.AppService
	logger  *slog.Logger
}

func NewAppHandler(service *services.AppService, logger *slog.Logger) *AppHandler {
	return &AppHandler{service: service, logger: logger}
}

func (h *AppHandler) List(w http.ResponseWriter, r *http.Request) error {
	rows, err := h.service.List(r.Context(), httpx.Actor(r))
	if err != nil {
		return err
	}

	httpx.OK(w, "Apps loaded.", rows)
	return nil
}

// Create menerbitkan kunci baru.
//
// Pesannya sengaja menyebut bahwa kuncinya cuma muncul sekali. Yang membaca
// balasan ini di Postman biasanya menutupnya tanpa menyalin, dan baru sadar saat
// integrasinya mulai ditulis.
func (h *AppHandler) Create(w http.ResponseWriter, r *http.Request) error {
	body, err := httpx.Decode[struct {
		Name string `json:"name"`
	}](w, r)
	if err != nil {
		return err
	}

	created, err := h.service.Create(r.Context(), httpx.Actor(r), body.Name)
	if err != nil {
		return err
	}

	httpx.Created(w, "App registered. Copy the key now — it is shown only once.", created)
	return nil
}

func (h *AppHandler) Revoke(w http.ResponseWriter, r *http.Request) error {
	revoked, err := h.service.Revoke(r.Context(), httpx.Actor(r), r.PathValue("id"))
	if err != nil {
		return err
	}

	httpx.OK(w, "App revoked.", revoked)
	return nil
}

func (h *AppHandler) Me(w http.ResponseWriter, r *http.Request) error {
	me, err := h.service.Me(r.Context(), httpx.Actor(r))
	if err != nil {
		return err
	}

	httpx.OK(w, "Caller identified.", me)
	return nil
}
