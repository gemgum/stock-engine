// Package handlers membaca HTTP, memanggil satu method service, lalu membalas
// amplopnya.
//
// Tidak ada keputusan di sini. Handler boleh menolak kiriman yang bentuknya
// rusak — jadwal yang bukan tanggal, angka yang bukan angka — tapi soal siapa
// boleh apa milik service.
package handlers

import (
	"log/slog"
	"net/http"
	"strconv"

	"github.com/gemgum/stock-engine/internal/models"
	"github.com/gemgum/stock-engine/internal/services"
	"github.com/gemgum/stock-engine/pkg/httpx"
)

type ContentHandler struct {
	service *services.ContentService
	logger  *slog.Logger

	// maxUpload datang dari config, bukan konstanta di sini: batas ukuran
	// berkas adalah keputusan yang berubah saat disk menipis.
	maxUpload int64
}

func NewContentHandler(service *services.ContentService, maxUpload int64,
	logger *slog.Logger) *ContentHandler {

	return &ContentHandler{service: service, maxUpload: maxUpload, logger: logger}
}

// Upload menerima multipart: satu berkas plus keterangannya.
//
// Multipart, bukan JSON berisi base64: base64 membengkakkan video 200MB jadi
// 267MB yang harus muat di memori dua kali.
func (h *ContentHandler) Upload(w http.ResponseWriter, r *http.Request) error {
	r.Body = http.MaxBytesReader(w, r.Body, h.maxUpload)

	// 32MB ditahan di memori, sisanya ditulis ke berkas sementara oleh
	// pustaka bawaan.
	//
	// ponytail: berkas besar jadi ditulis dua kali — sekali ke temp, sekali ke
	// tujuannya. Ganti ke r.MultipartReader() yang mengalirkannya langsung kalau
	// I/O disk jadi soal; harganya, urutan field dalam kiriman jadi penting.
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		return httpx.BadRequest("The upload could not be read, or the file is too large.").WithCause(err)
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		return httpx.BadRequest("Field 'file' is required.").WithCause(err)
	}
	defer func() { _ = file.Close() }()

	scheduledAt, err := parseTime(r.FormValue("scheduled_at"))
	if err != nil {
		return err
	}

	in := models.NewContent{
		Filename:    header.Filename,
		Mime:        header.Header.Get("Content-Type"),
		Caption:     r.FormValue("caption"),
		Platform:    r.FormValue("platform"),
		ScheduledAt: scheduledAt,
	}

	created, err := h.service.Upload(r.Context(), httpx.Actor(r), in, file)
	if err != nil {
		return err
	}

	httpx.Created(w, "Content uploaded.", created)
	return nil
}

func (h *ContentHandler) List(w http.ResponseWriter, r *http.Request) error {
	q := r.URL.Query()

	scope, err := models.ParseScope(q.Get("scope"))
	if err != nil {
		return httpx.BadRequest(err.Error())
	}

	limit, _ := strconv.Atoi(q.Get("limit"))
	offset, _ := strconv.Atoi(q.Get("offset"))

	page, err := h.service.List(r.Context(), httpx.Actor(r), models.ContentFilter{
		Scope:    scope,
		Platform: q.Get("platform"),
		Limit:    limit,
		Offset:   offset,
	})
	if err != nil {
		return err
	}

	httpx.OK(w, "Contents loaded.", page)
	return nil
}

func (h *ContentHandler) Get(w http.ResponseWriter, r *http.Request) error {
	content, err := h.service.Get(r.Context(), httpx.Actor(r), r.PathValue("id"))
	if err != nil {
		return err
	}

	httpx.OK(w, "Content loaded.", content)
	return nil
}

// File menyajikan berkasnya.
//
// Lewat http.ServeFile, bukan io.Copy, karena ia yang mengerti header Range —
// dan tanpa Range, pemutar video tidak bisa meloncat ke menit mana pun tanpa
// mengunduh seluruh berkas lebih dulu.
func (h *ContentHandler) File(w http.ResponseWriter, r *http.Request) error {
	content, path, err := h.service.File(r.Context(), httpx.Actor(r), r.PathValue("id"))
	if err != nil {
		return err
	}

	if content.Mime != "" {
		w.Header().Set("Content-Type", content.Mime)
	}
	// Nama aslinya dikembalikan lewat header, karena di disk namanya cuma id.
	w.Header().Set("Content-Disposition", `attachment; filename="`+safeFilename(content.Filename)+`"`)

	http.ServeFile(w, r, path)
	return nil
}

func (h *ContentHandler) Claim(w http.ResponseWriter, r *http.Request) error {
	claimed, err := h.service.Claim(r.Context(), httpx.Actor(r), r.PathValue("id"))
	if err != nil {
		return err
	}

	httpx.OK(w, "Content claimed.", claimed)
	return nil
}

func (h *ContentHandler) Publish(w http.ResponseWriter, r *http.Request) error {
	body, err := httpx.Decode[struct {
		Published bool   `json:"published"`
		Reason    string `json:"reason"`
	}](w, r)
	if err != nil {
		return err
	}

	reported, err := h.service.Publish(r.Context(), httpx.Actor(r), r.PathValue("id"),
		body.Published, body.Reason)
	if err != nil {
		return err
	}

	httpx.OK(w, "Publication recorded.", reported)
	return nil
}

func (h *ContentHandler) Reschedule(w http.ResponseWriter, r *http.Request) error {
	body, err := httpx.Decode[struct {
		ScheduledAt string `json:"scheduled_at"`
	}](w, r)
	if err != nil {
		return err
	}

	at, err := parseTime(body.ScheduledAt)
	if err != nil {
		return err
	}

	moved, err := h.service.Reschedule(r.Context(), httpx.Actor(r), r.PathValue("id"), at)
	if err != nil {
		return err
	}

	httpx.OK(w, "Content rescheduled.", moved)
	return nil
}

func (h *ContentHandler) Archive(w http.ResponseWriter, r *http.Request) error {
	archived, err := h.service.Archive(r.Context(), httpx.Actor(r), r.PathValue("id"))
	if err != nil {
		return err
	}

	httpx.OK(w, "Content archived.", archived)
	return nil
}

// parseTime membaca jadwal dari kiriman.
//
// Satu bentuk saja, `2026-09-12 09:00:00`, dan dibaca sebagai waktu server.
// Menerima beberapa bentuk sekaligus terdengar ramah, tapi yang mengirim bentuk
// lain tidak pernah tahu bentuknya diterjemahkan — dan salah tafsir jadwal baru
// ketahuan saat kontennya tayang di jam yang keliru.
func parseTime(raw string) (models.Time, error) {
	if raw == "" {
		return models.Time{}, httpx.BadRequest("Field 'scheduled_at' is required, in 'YYYY-MM-DD HH:MM:SS' server time (e.g. 2026-09-12 09:00:00).")
	}

	t, err := models.ParseTime(raw)
	if err != nil {
		return models.Time{}, httpx.BadRequest("Field 'scheduled_at' must look like '2026-09-12 09:00:00' (server time).").WithCause(err)
	}
	return t, nil
}

// safeFilename membuang kutip dan baris baru dari nama berkas.
//
// Nama itu datang dari pengunggah dan dipasang ke dalam header. Kutip yang lolos
// bisa menyelipkan header lain, dan itu bukan sesuatu yang boleh bergantung pada
// niat baik pengunggah.
func safeFilename(name string) string {
	safe := make([]rune, 0, len(name))
	for _, r := range name {
		switch r {
		case '"', '\\', '\r', '\n':
			continue
		}
		safe = append(safe, r)
	}
	if len(safe) == 0 {
		return "content"
	}
	return string(safe)
}
