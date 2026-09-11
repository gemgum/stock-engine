package httpx

import "net/http"

// Error adalah satu-satunya jenis galat yang boleh sampai ke lapisan HTTP.
//
// Ia membawa dua hal sekaligus: apa yang akan dilihat pemanggil (Code, Status,
// Message) dan apa yang cuma boleh masuk log (cause). Pemisahan itu yang
// membuat "galat aslinya tidak pernah keluar" bisa ditegakkan di satu tempat,
// bukan diingat di tiap handler.
type Error struct {
	Code    int
	Status  Status
	Message string
	cause   error
}

func (e *Error) Error() string { return e.Message }

func (e *Error) Unwrap() error { return e.cause }

// HTTPStatus dipakai saat menulis jawabannya. Sama dengan Code — ada sebagai
// method supaya pemanggil tidak perlu tahu keduanya memang selalu sama.
func (e *Error) HTTPStatus() int { return e.Code }

// WithCause menempelkan sebab aslinya. Isinya cuma masuk log.
func (e *Error) WithCause(err error) *Error {
	e.cause = err
	return e
}

func newError(code int, status Status, message string) *Error {
	return &Error{Code: code, Status: status, Message: message}
}

// From memastikan apa pun yang sampai ke httpx.Fail punya bentuk yang sama.
//
// Galat yang bukan *Error berarti tidak ada yang pernah memutuskan boleh
// dilihat atau tidak — jadi diperlakukan sebagai bug kami, dan isinya berhenti
// di log.
func From(err error) *Error {
	if appErr, ok := err.(*Error); ok {
		return appErr
	}
	return ServerError(err)
}

func BadRequest(message string) *Error {
	return newError(http.StatusBadRequest, StatusInvalidInput, message)
}

// InvalidKey dipakai untuk kunci yang tidak ada, tidak dikenal, atau sudah
// dicabut — ketiganya dijawab sama persis.
//
// Membedakan "kunci tidak dikenal" dari "kunci dicabut" cuma berguna bagi yang
// menebak-nebak kunci.
func InvalidKey() *Error {
	return newError(http.StatusUnauthorized, StatusUnauthorized, "The API key is missing, unknown, or revoked.")
}

func Forbidden(message string) *Error {
	return newError(http.StatusForbidden, StatusForbidden, message)
}

func NotFound(message string) *Error {
	return newError(http.StatusNotFound, StatusNotFound, message)
}

// AlreadyClaimed adalah jawaban untuk lomba yang memang dirancang terjadi: dua
// integrator meminta konten yang sama, satu menang.
//
// Bukan galat dari sisi pemanggil — dia cuma perlu mengambil yang berikutnya.
func AlreadyClaimed() *Error {
	return newError(http.StatusConflict, StatusAlreadyClaimed, "This content is already claimed by another app.")
}

func TooManyRequests() *Error {
	return newError(http.StatusTooManyRequests, StatusTooManyRequests, "Too many requests. Please slow down.")
}

// DatabaseError dipakai saat database menolak atau tidak bisa dihubungi.
//
// Dipisahkan dari ServerError karena jawabannya berbeda bagi pemanggil: yang
// ini layak dicoba lagi nanti.
func DatabaseError(err error) *Error {
	return newError(http.StatusInternalServerError, StatusDatabaseError,
		"The database could not be reached. Please try again later.").WithCause(err)
}

func ServerError(err error) *Error {
	return newError(http.StatusInternalServerError, StatusServerError,
		"Something went wrong on our side.").WithCause(err)
}
