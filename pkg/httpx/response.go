// Package httpx berisi bentuk baku jawaban HTTP: satu amplop untuk semua
// balasan, dan satu jenis galat untuk seluruh servis.
//
// Alasannya satu: kalau tiap handler merangkai jawabannya sendiri, integrator
// harus menebak bentuknya per endpoint — kadang `{"error":...}`, kadang larik
// telanjang. Satu bentuk berarti mereka menulis satu pembaca saja.
package httpx

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
)

// Status adalah golongan jawaban, dan **satu-satunya hal yang dipakai klien
// memilih tindakan**.
//
// Golongan ditambah kalau kalimat umumnya akan **menyesatkan** — membuat orang
// melakukan hal yang salah, atau berhenti padahal ada yang bisa dilakukan.
// Bukan kalau sekadar kurang tepat.
//
// Di servis ini ambangnya lebih ketat daripada biasanya, karena kliennya di
// luar kendali kita: golongan baru berarti semua integrator yang sudah jalan
// harus diberi tahu.
type Status string

const (
	// Berhasil.
	StatusSuccess Status = "success"

	// Kiriman yang tidak bisa diproses: field kurang, format salah, nilai di
	// luar rentang. Mengulang request yang sama akan gagal lagi.
	StatusInvalidInput Status = "invalid_input"

	// Kuncinya tidak dikenal, atau sudah dicabut.
	StatusUnauthorized Status = "unauthorized"

	// Kuncinya sah, tapi tidak berhak memanggil alamat ini.
	StatusForbidden Status = "forbidden"

	// Yang diminta tidak ada.
	StatusNotFound Status = "not_found"

	// Database menolak atau tidak bisa dihubungi. Layak dicoba lagi nanti.
	StatusDatabaseError Status = "database_error"

	// Sisanya — bug kami. Layak dicoba lagi nanti.
	StatusServerError Status = "server_error"

	// ── Golongan yang ada karena kalimat umumnya menyesatkan ──────────────

	// Konten ini sudah dikunci aplikasi lain. Dengan `forbidden`, integrator
	// mengira kuncinya bermasalah lalu berhenti — padahal yang benar adalah
	// melewati konten ini dan mengambil berikutnya.
	//
	// Ini golongan yang paling sering ditemui begitu ada dua worker.
	StatusAlreadyClaimed Status = "already_claimed"

	// Terlalu sering memanggil. Yang perlu dilakukan menunggu, bukan mengubah
	// apa pun.
	StatusTooManyRequests Status = "too_many_requests"
)

// Response adalah bentuk **setiap** balasan servis, berhasil maupun gagal.
//
//	{"code":200,"status":"success","message":"...","data":{...}}
//	{"code":409,"status":"already_claimed","message":"...","data":null}
//
// `code` angka status HTTP, sama dengan yang ada di header. **Yang dipakai
// klien memilih tindakan adalah `status`.** `message` menyebut sebab
// persisnya, selalu dalam bahasa Inggris — untuk log dan untuk layar cadangan
// saat golongannya belum dikenal klien.
type Response struct {
	Code    int    `json:"code"`
	Status  Status `json:"status"`
	Message string `json:"message"`
	Data    any    `json:"data"`
}

func write(w http.ResponseWriter, httpStatus int, body Response) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(httpStatus)
	_ = json.NewEncoder(w).Encode(body)
}

// OK membalas 200 dengan data.
func OK(w http.ResponseWriter, message string, data any) {
	success(w, http.StatusOK, message, data)
}

// Created membalas 201. Dipakai untuk yang benar-benar membuat baris baru.
func Created(w http.ResponseWriter, message string, data any) {
	success(w, http.StatusCreated, message, data)
}

// success menyalin angka status HTTP ke dalam amplopnya.
//
// Satu angka, dua tempat, dan keduanya wajib sama. Yang membaca amplopnya di
// log atau di Postman tidak boleh melihat angka yang berbeda dari yang sungguh
// dikirim.
func success(w http.ResponseWriter, httpStatus int, message string, data any) {
	write(w, httpStatus, Response{
		Code: httpStatus, Status: StatusSuccess, Message: message, Data: data,
	})
}

// Fail membalas galat, dan **di sinilah satu-satunya tempat** isi galat
// diputuskan boleh keluar atau tidak.
//
// Sebab aslinya selalu berhenti di log. Nama tabel, path berkas, dan bunyi
// penolakan SQLite tidak pernah ikut keluar: buat pemakai tidak berarti
// apa-apa, dan buat penyerang itu peta.
func Fail(w http.ResponseWriter, logger *slog.Logger, err error) {
	appErr := From(err)

	if appErr.cause != nil {
		// Level dipilih dari golongannya, bukan selalu Error.
		//
		// Penolakan 4xx sudah dicatat lapisan service lengkap dengan nama
		// operasinya, jadi baris di sini cuma pengulangan — diturunkan ke Debug.
		// Yang 5xx tetap Error: galat yang lahir di luar service, misalnya di
		// middleware, tidak punya tempat lain untuk tercatat.
		level := slog.LevelDebug
		if appErr.HTTPStatus() >= 500 {
			level = slog.LevelError
		}

		logger.Log(context.Background(), level, "request gagal",
			"code", appErr.Code,
			"status", string(appErr.Status),
			"cause", appErr.cause.Error(),
		)
	}

	write(w, appErr.HTTPStatus(), Response{
		Code:    appErr.Code,
		Status:  appErr.Status,
		Message: appErr.Message,
		Data:    nil,
	})
}
