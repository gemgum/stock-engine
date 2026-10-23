package handlers

import (
	"log/slog"
	"net/http"

	"github.com/gemgum/stock-engine/internal/services"
	"github.com/gemgum/stock-engine/pkg/httpx"
)

type PlatformHandler struct {
	service *services.PlatformService
	logger  *slog.Logger
}

func NewPlatformHandler(service *services.PlatformService, logger *slog.Logger) *PlatformHandler {
	return &PlatformHandler{service: service, logger: logger}
}

func (h *PlatformHandler) List(w http.ResponseWriter, r *http.Request) error {
	rows, err := h.service.List(r.Context(), httpx.Actor(r))
	if err != nil {
		return err
	}

	httpx.OK(w, "Platforms loaded.", rows)
	return nil
}

func (h *PlatformHandler) Create(w http.ResponseWriter, r *http.Request) error {
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

	httpx.Created(w, "Platform registered.", created)
	return nil
}

func (h *PlatformHandler) Archive(w http.ResponseWriter, r *http.Request) error {
	archived, err := h.service.Archive(r.Context(), httpx.Actor(r), r.PathValue("name"))
	if err != nil {
		return err
	}

	httpx.OK(w, "Platform archived.", archived)
	return nil
}
