// Package models memegang bentuk data dan antarmuka Repository yang dijanjikan
// tiap domain.
//
// Tidak mengimpor lapisan mana pun. Ia yang diimpor semuanya — kalau arahnya
// dibalik, tidak ada lagi yang bisa diuji sendirian.
package models

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"strings"
)

// NewID menerbitkan penanda baru.
//
// Acak, bukan berurutan: id konten muncul di alamat yang dipegang integrator,
// dan angka berurutan memberitahu siapa pun berapa banyak konten yang ada serta
// mengundang menebak id tetangganya.
func NewID() string {
	raw := make([]byte, 12)
	if _, err := rand.Read(raw); err != nil {
		panic("tidak bisa membuat id acak: " + err.Error())
	}
	return hex.EncodeToString(raw)
}

// Status adalah perjalanan satu konten, dari diunggah sampai selesai.
//
//	scheduled ──claim──→ claimed ──publication──→ published
//	    ↑                                     └─→ failed
//	    └────────────── schedule ─────────────────┘
//
// Yang gagal tidak kembali sendiri ke antrean. Itu disengaja: konten yang
// ditolak platform biasanya ditolak lagi kalau dikirim ulang apa adanya, dan
// antrean yang mengisi ulang dirinya sendiri akan mencoba selamanya tanpa ada
// yang tahu. Yang mengembalikannya manusia, lewat `POST /contents/{id}/schedule`.
type Status string

const (
	StatusScheduled Status = "scheduled"
	StatusClaimed   Status = "claimed"
	StatusPublished Status = "published"
	StatusFailed    Status = "failed"
	StatusArchived  Status = "archived"
)

// Content adalah satu berkas beserta jadwal dan riwayatnya.
//
// Berkasnya sendiri tidak pernah ikut di sini — yang ada cuma penunjuknya.
// Alamatnya `/v1/contents/{id}/file`.
type Content struct {
	ID       string `json:"id"`
	Filename string `json:"filename"`
	Mime     string `json:"mime"`
	Size     int64  `json:"size"`

	// Checksum SHA-256 berkasnya, supaya integrator bisa memastikan unduhannya
	// utuh tanpa harus mempercayai jaringan.
	Checksum string `json:"checksum"`

	Caption  string `json:"caption"`
	Platform string `json:"platform"`

	ScheduledAt Time   `json:"scheduled_at"`
	Status      Status `json:"status"`

	ClaimedBy   *string `json:"claimed_by"`
	ClaimedAt   *Time   `json:"claimed_at"`
	PublishedAt *Time   `json:"published_at"`

	// FailureReason diisi integrator saat melapor gagal. Satu-satunya tempat
	// teks dari luar disimpan apa adanya, dan karena itu tidak pernah
	// ditafsirkan servis — cuma disimpan dan ditampilkan kembali.
	FailureReason *string `json:"failure_reason"`

	ArchivedAt *Time  `json:"archived_at"`
	UploadedBy string `json:"uploaded_by"`
	CreatedAt  Time   `json:"created_at"`
}

// NewContent adalah kiriman unggahan, setelah berkasnya selesai ditulis.
//
// ID datang dari pemanggil, bukan dari database. Berkasnya disimpan dengan nama
// itu, jadi id harus sudah ada sebelum barisnya ditulis — dan urutan itulah yang
// membuat baris tanpa berkas tidak mungkin terjadi.
type NewContent struct {
	ID          string
	Filename    string
	Mime        string
	Size        int64
	Checksum    string
	Caption     string
	Platform    string
	ScheduledAt Time
	UploadedBy  string
}

// ContentFilter adalah penyaring daftar konten.
//
// Scope menjawab "yang mana", bukan "milik siapa" — di servis ini gudangnya
// satu dan semua aplikasi melihat isi yang sama.
type ContentFilter struct {
	Scope    Scope
	Platform string
	Limit    int
	Offset   int

	// Now dioper dari service, bukan dibaca repository, supaya "sudah waktunya"
	// dihitung di satu tempat dan bisa diuji tanpa menunggu jam berganti.
	Now Time
}

// ContentPage adalah satu halaman daftar konten.
//
// Total ikut dikirim karena panel butuh tahu ada berapa halaman, dan ia tidak
// bisa menyimpulkannya dari jumlah baris yang diterima: halaman terakhir yang
// kebetulan pas berisi 50 baris tidak bisa dibedakan dari halaman yang masih
// ada sambungannya.
//
// Limit dan Offset ikut dipantulkan kembali — bukan hiasan. Servis membetulkan
// nilai yang tidak masuk akal (limit 5000 jadi 50), dan panel yang tidak tahu
// nilainya dibetulkan akan salah menghitung halaman.
type ContentPage struct {
	Items  []Content `json:"items"`
	Total  int       `json:"total"`
	Limit  int       `json:"limit"`
	Offset int       `json:"offset"`
}

// Scope adalah nilai `?scope=` yang dikenal.
type Scope string

const (
	// ScopeDue: sudah waktunya tayang dan belum diambil siapa pun. Ini yang
	// dipanggil integrator setiap beberapa menit.
	ScopeDue Scope = "due"

	// ScopeUnused: belum pernah tayang dan belum dibatalkan — apa pun sudah
	// sampai mana percobaannya. Ini "stok yang masih punya giliran", pertanyaan
	// yang berbeda dari ScopeScheduled: konten yang sedang diproses integrator
	// dan konten yang gagal semalam juga belum terpakai.
	ScopeUnused Scope = "unused"

	// ScopeOverdue: jadwalnya sudah lewat tapi tidak pernah tayang.
	//
	// Menjawab "kenapa konten ini tidak muncul?", dan itu pertanyaan manusia —
	// berbeda dari ScopeDue yang antrean kerja integrator. Bedanya: yang sedang
	// dipegang integrator dan yang gagal ikut kelihatan di sini, karena
	// keduanya sama-sama terlewat jadwalnya.
	ScopeOverdue Scope = "overdue"

	ScopeScheduled Scope = "scheduled"
	ScopeClaimed   Scope = "claimed"
	ScopePublished Scope = "published"
	ScopeFailed    Scope = "failed"
	ScopeArchived  Scope = "archived"
	ScopeAll       Scope = "all"
)

// ParseScope menolak nilai yang tidak dikenal dengan menyebut apa yang ada.
//
// Diam-diam mengembalikan daftar lain untuk permintaan yang meminta sesuatu
// yang tidak dikenal adalah jenis salah yang tidak ketahuan sampai ada yang
// membandingkan angkanya.
func ParseScope(raw string) (Scope, error) {
	known := []Scope{ScopeDue, ScopeOverdue, ScopeUnused, ScopeScheduled,
		ScopeClaimed, ScopePublished, ScopeFailed, ScopeArchived, ScopeAll}

	if raw == "" {
		return ScopeAll, nil
	}
	for _, s := range known {
		if Scope(raw) == s {
			return s, nil
		}
	}

	labels := make([]string, len(known))
	for i, s := range known {
		labels[i] = string(s)
	}
	return "", fmt.Errorf("unknown scope %q. Known values: %s", raw, strings.Join(labels, ", "))
}

type ContentRepository interface {
	Insert(ctx context.Context, in NewContent) (Content, error)
	List(ctx context.Context, filter ContentFilter) ([]Content, error)

	// Count memakai penyaring yang sama persis dengan List, tanpa batas
	// halamannya. Dua penyusun penyaring yang terpisah akan berbeda tipis cepat
	// atau lambat, dan totalnya jadi bohong tanpa ada yang menyadarinya.
	Count(ctx context.Context, filter ContentFilter) (int, error)
	Get(ctx context.Context, id string) (Content, error)

	// Claim mengubah baris **hanya kalau statusnya masih scheduled**, dan
	// mengembalikan false kalau tidak ada yang berubah.
	//
	// Satu perintah, bukan baca-lalu-tulis: dua integrator yang datang
	// bersamaan harus punya satu pemenang, dan yang memutuskan database.
	Claim(ctx context.Context, id, appID string, at Time) (bool, error)

	// SetPublication cuma menerima laporan dari aplikasi yang mengclaim
	// kontennya. False berarti syaratnya tidak terpenuhi — belum diclaim, atau
	// diclaim aplikasi lain.
	SetPublication(ctx context.Context, id, appID string, status Status, reason *string, at Time) (bool, error)
	SetSchedule(ctx context.Context, id string, at Time) error
	Archive(ctx context.Context, id string, at Time) error
}

// Files adalah yang perlu diketahui service tentang penyimpanan berkas.
//
// Antarmuka, bukan tipe konkret, supaya suatu hari berkasnya bisa pindah ke S3
// tanpa menyentuh satu baris pun di services/content.go — dan supaya service
// bisa diuji tanpa menyentuh disk.
type Files interface {
	Save(id string, src io.Reader) (size int64, checksum string, err error)
	Path(id string) string
	Remove(id string) error
}
