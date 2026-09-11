package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/gemgum/stock-engine/internal/api"
	"github.com/gemgum/stock-engine/internal/config"
	"github.com/gemgum/stock-engine/internal/database"
	"github.com/gemgum/stock-engine/internal/models"
	"github.com/gemgum/stock-engine/internal/services"
	"github.com/gemgum/stock-engine/migrations"
)

const adminKey = "kunci-induk-untuk-pengujian-yang-panjangnya-cukup"

// setup merakit servis lengkap di atas SQLite sungguhan.
//
// Bukan tiruan: yang paling ingin dibuktikan justru perilaku database — claim
// yang cuma boleh menang sekali, dan urutan waktu yang dibandingkan sebagai
// teks. Tiruan akan menjawab apa pun yang kita suruh.
func setup(t *testing.T) *httptest.Server {
	srv, _ := setupWith(t)
	return srv
}

// setupWith ikut mengembalikan pengaturannya, untuk uji yang perlu
// mengintip isi folder penyimpanan.
func setupWith(t *testing.T) (*httptest.Server, config.Config) {
	t.Helper()

	dir := t.TempDir()
	cfg := config.Config{
		StorageDir:  dir,
		AdminKey:    adminKey,
		MaxUploadMB: 200,
		Address:     ":0",
	}

	db, err := database.Open(cfg.DatabasePath())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	schema, err := migrations.All()
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(context.Background(), schema); err != nil {
		t.Fatal(err)
	}

	files, err := services.NewFiles(cfg.FileDir())
	if err != nil {
		t.Fatal(err)
	}

	// Log dibuang: yang diuji perilakunya, dan baris log yang membanjiri output
	// uji membuat kegagalan sungguhan susah ditemukan.
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	srv := httptest.NewServer(api.Build(db, files, cfg, logger))
	t.Cleanup(srv.Close)
	return srv, cfg
}

// at menyebut waktu server, sekian lama dari sekarang.
//
// Bentuknya `2026-09-13 09:00:00` — satu-satunya yang diterima servis ini.
func at(d time.Duration) string {
	return time.Now().Add(d).Format(models.TimeLayout)
}

type envelope struct {
	Code    int             `json:"code"`
	Status  string          `json:"status"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

func call(t *testing.T, srv *httptest.Server, method, path, key string,
	body io.Reader, contentType string) envelope {
	t.Helper()

	req, err := http.NewRequest(method, srv.URL+path, body)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+key)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()

	var out envelope
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		t.Fatalf("%s %s: jawaban bukan JSON: %v", method, path, err)
	}
	if out.Code != res.StatusCode {
		t.Fatalf("%s %s: code di amplop %d, di header %d", method, path, out.Code, res.StatusCode)
	}
	return out
}

func postJSON(t *testing.T, srv *httptest.Server, path, key, body string) envelope {
	t.Helper()
	return call(t, srv, "POST", path, key, bytes.NewBufferString(body), "application/json")
}

// upload mengirim satu berkas beserta jadwalnya.
func upload(t *testing.T, srv *httptest.Server, key, scheduledAt, body string) envelope {
	t.Helper()

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)

	part, err := mw.CreateFormFile("file", "poster.jpg")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte(body)); err != nil {
		t.Fatal(err)
	}
	_ = mw.WriteField("scheduled_at", scheduledAt)
	_ = mw.WriteField("caption", "halo dunia")
	_ = mw.WriteField("platform", "instagram")
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}

	return call(t, srv, "POST", "/v1/contents", key, &buf, mw.FormDataContentType())
}

func decode[T any](t *testing.T, res envelope) T {
	t.Helper()

	var out T
	if err := json.Unmarshal(res.Data, &out); err != nil {
		t.Fatalf("data tidak terbaca: %v (%s)", err, res.Data)
	}
	return out
}

// page adalah bentuk balasan daftar: satu halaman beserta jumlah seluruhnya.
type page struct {
	Items  []content `json:"items"`
	Total  int       `json:"total"`
	Limit  int       `json:"limit"`
	Offset int       `json:"offset"`
}

type content struct {
	ID            string  `json:"id"`
	Status        string  `json:"status"`
	Size          int64   `json:"size"`
	Checksum      string  `json:"checksum"`
	ClaimedBy     *string `json:"claimed_by"`
	FailureReason *string `json:"failure_reason"`
}

// registerApp menerbitkan satu kunci aplikasi lewat kunci induk.
func registerApp(t *testing.T, srv *httptest.Server, name string) string {
	t.Helper()

	res := postJSON(t, srv, "/v1/apps", adminKey, `{"name":"`+name+`"}`)
	if res.Code != http.StatusCreated {
		t.Fatalf("daftar app gagal: %+v", res)
	}

	app := decode[struct {
		Key string `json:"key"`
	}](t, res)

	if app.Key == "" {
		t.Fatal("kunci tidak ikut di balasan, padahal cuma muncul sekali")
	}
	return app.Key
}

func TestUnknownKeyIsRejected(t *testing.T) {
	srv := setup(t)

	res := call(t, srv, "GET", "/v1/contents", "sk_bukan-kunci-siapa-siapa", nil, "")
	if res.Status != "unauthorized" {
		t.Fatalf("kunci asing harus unauthorized, dapat %q", res.Status)
	}
}

func TestAppKeyCannotRegisterApps(t *testing.T) {
	srv := setup(t)
	key := registerApp(t, srv, "poster-1")

	res := postJSON(t, srv, "/v1/apps", key, `{"name":"aplikasi-selundupan"}`)
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("kunci aplikasi harus ditolak di /v1/apps, dapat %+v", res)
	}
}

func TestRevokedKeyStopsWorking(t *testing.T) {
	srv := setup(t)
	key := registerApp(t, srv, "poster-1")

	labels := decode[[]struct {
		ID string `json:"id"`
	}](t, call(t, srv, "GET", "/v1/apps", adminKey, nil, ""))
	if len(labels) != 1 {
		t.Fatalf("harusnya ada satu aplikasi, dapat %d", len(labels))
	}

	if res := postJSON(t, srv, "/v1/apps/"+labels[0].ID+"/revocation", adminKey, `{}`); res.Code != 200 {
		t.Fatalf("pencabutan gagal: %+v", res)
	}

	if res := call(t, srv, "GET", "/v1/contents", key, nil, ""); res.Status != "unauthorized" {
		t.Fatalf("kunci yang dicabut harus berhenti bekerja, dapat %q", res.Status)
	}
}

func TestUnknownScopeIsRejected(t *testing.T) {
	srv := setup(t)
	key := registerApp(t, srv, "poster-1")

	res := call(t, srv, "GET", "/v1/contents?scope=besok", key, nil, "")
	if res.Status != "invalid_input" {
		t.Fatalf("scope asing harus ditolak, dapat %q", res.Status)
	}
}

// TestScopeDue memastikan antrean cuma berisi yang sudah waktunya.
//
// Ini kueri yang dipanggil integrator tiap beberapa menit, dan salahnya senyap:
// yang belum waktunya ikut keluar berarti konten tayang lebih cepat dari
// jadwalnya, dan tidak ada yang melapor.
func TestScopeDue(t *testing.T) {
	srv := setup(t)
	key := registerApp(t, srv, "poster-1")

	past := at(-time.Hour)
	tomorrow := at(24 * time.Hour)

	due := decode[content](t, upload(t, srv, key, past, "isi-berkas"))
	upload(t, srv, key, tomorrow, "belum-waktunya")

	queue := decode[page](t, call(t, srv, "GET", "/v1/contents?scope=due", key, nil, "")).Items
	if len(queue) != 1 || queue[0].ID != due.ID {
		t.Fatalf("antrean harusnya berisi satu yang sudah waktunya, dapat %+v", queue)
	}
}

// TestClaimWinsOnlyOnce adalah alasan seluruh mekanisme claim ada.
func TestClaimWinsOnlyOnce(t *testing.T) {
	srv := setup(t)
	first := registerApp(t, srv, "poster-1")
	second := registerApp(t, srv, "poster-2")

	past := at(-time.Hour)
	c := decode[content](t, upload(t, srv, first, past, "isi-berkas"))

	won := postJSON(t, srv, "/v1/contents/"+c.ID+"/claim", first, `{}`)
	if won.Code != 200 || decode[content](t, won).Status != "claimed" {
		t.Fatalf("claim pertama harus menang: %+v", won)
	}

	lost := postJSON(t, srv, "/v1/contents/"+c.ID+"/claim", second, `{}`)
	if lost.Status != "already_claimed" || lost.Code != http.StatusConflict {
		t.Fatalf("claim kedua harus already_claimed 409, dapat %+v", lost)
	}

	// Yang sudah diambil tidak boleh muncul lagi di antrean, kalau tidak
	// integrator ketiga akan memposting konten yang sama.
	queue := decode[page](t, call(t, srv, "GET", "/v1/contents?scope=due", first, nil, "")).Items
	if len(queue) != 0 {
		t.Fatalf("yang sudah diclaim masih muncul di antrean: %+v", queue)
	}
}

func TestClaimMissingContent(t *testing.T) {
	srv := setup(t)
	key := registerApp(t, srv, "poster-1")

	res := postJSON(t, srv, "/v1/contents/tidak-ada/claim", key, `{}`)
	if res.Status != "not_found" {
		t.Fatalf("id asing harus not_found, bukan %q", res.Status)
	}
}

func TestPublicationRequiresClaim(t *testing.T) {
	srv := setup(t)
	key := registerApp(t, srv, "poster-1")

	past := at(-time.Hour)
	c := decode[content](t, upload(t, srv, key, past, "isi-berkas"))

	res := postJSON(t, srv, "/v1/contents/"+c.ID+"/publication", key, `{"published":true}`)
	if res.Status != "invalid_input" {
		t.Fatalf("lapor tanpa claim harus ditolak, dapat %+v", res)
	}
}

func TestFailureRequiresReason(t *testing.T) {
	srv := setup(t)
	key := registerApp(t, srv, "poster-1")

	past := at(-time.Hour)
	c := decode[content](t, upload(t, srv, key, past, "isi-berkas"))
	postJSON(t, srv, "/v1/contents/"+c.ID+"/claim", key, `{}`)

	res := postJSON(t, srv, "/v1/contents/"+c.ID+"/publication", key, `{"published":false}`)
	if res.Status != "invalid_input" {
		t.Fatalf("gagal tanpa alasan harus ditolak, dapat %+v", res)
	}
}

// TestFullFlow menelusuri satu konten dari unggah sampai tayang, lalu satu lagi
// yang gagal dan dijadwalkan ulang.
func TestFullFlow(t *testing.T) {
	srv := setup(t)
	key := registerApp(t, srv, "poster-1")

	past := at(-time.Hour)
	c := decode[content](t, upload(t, srv, key, past, "isi-berkas"))

	if c.Size != int64(len("isi-berkas")) || c.Checksum == "" {
		t.Fatalf("ukuran atau checksum tidak terisi: %+v", c)
	}

	// Berkasnya bisa diambil, dan isinya persis yang dikirim.
	req, _ := http.NewRequest("GET", srv.URL+"/v1/contents/"+c.ID+"/file", nil)
	req.Header.Set("Authorization", "Bearer "+key)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	_ = res.Body.Close()
	if string(body) != "isi-berkas" {
		t.Fatalf("isi berkas berubah: %q", body)
	}

	postJSON(t, srv, "/v1/contents/"+c.ID+"/claim", key, `{}`)

	published := decode[content](t, postJSON(t, srv, "/v1/contents/"+c.ID+"/publication", key,
		`{"published":true}`))
	if published.Status != "published" {
		t.Fatalf("harusnya published, dapat %q", published.Status)
	}

	// Yang sudah tayang tidak boleh dijadwalkan ulang — itu akan memposting
	// ulang konten yang sama.
	again := postJSON(t, srv, "/v1/contents/"+c.ID+"/schedule", key,
		`{"scheduled_at":"`+at(0)+`"}`)
	if again.Status != "invalid_input" {
		t.Fatalf("menjadwalkan ulang yang sudah tayang harus ditolak, dapat %+v", again)
	}
}

func TestFailedCanBeRescheduled(t *testing.T) {
	srv := setup(t)
	key := registerApp(t, srv, "poster-1")

	past := at(-time.Hour)
	c := decode[content](t, upload(t, srv, key, past, "isi-berkas"))

	postJSON(t, srv, "/v1/contents/"+c.ID+"/claim", key, `{}`)
	failed := decode[content](t, postJSON(t, srv, "/v1/contents/"+c.ID+"/publication", key,
		`{"published":false,"reason":"token instagram kedaluwarsa"}`))
	if failed.Status != "failed" || failed.FailureReason == nil {
		t.Fatalf("harusnya failed beserta alasannya: %+v", failed)
	}

	// Dijadwalkan ulang ke masa lalu supaya langsung masuk antrean lagi.
	back := decode[content](t, postJSON(t, srv, "/v1/contents/"+c.ID+"/schedule", key,
		`{"scheduled_at":"`+past+`"}`))
	if back.Status != "scheduled" || back.ClaimedBy != nil || back.FailureReason != nil {
		t.Fatalf("penjadwalan ulang harus membersihkan jejak percobaan sebelumnya: %+v", back)
	}

	queue := decode[page](t, call(t, srv, "GET", "/v1/contents?scope=due", key, nil, "")).Items
	if len(queue) != 1 {
		t.Fatalf("yang dijadwalkan ulang harus kembali ke antrean: %+v", queue)
	}
}

func TestArchivedLeavesQueue(t *testing.T) {
	srv := setup(t)
	key := registerApp(t, srv, "poster-1")

	past := at(-time.Hour)
	c := decode[content](t, upload(t, srv, key, past, "isi-berkas"))

	if res := postJSON(t, srv, "/v1/contents/"+c.ID+"/archival", key, `{}`); res.Code != 200 {
		t.Fatalf("arsip gagal: %+v", res)
	}

	queue := decode[page](t, call(t, srv, "GET", "/v1/contents?scope=due", key, nil, "")).Items
	if len(queue) != 0 {
		t.Fatalf("yang diarsipkan masih di antrean: %+v", queue)
	}

	// Tapi masih terbaca. Servis ini tidak pernah menghapus apa pun.
	if res := call(t, srv, "GET", "/v1/contents/"+c.ID, key, nil, ""); res.Code != 200 {
		t.Fatalf("yang diarsipkan harus tetap terbaca: %+v", res)
	}
}

// TestScheduleRejectsOtherFormats menjaga satu bentuk tetap satu bentuk.
//
// RFC3339 dulu yang dipakai, jadi integrator lama mungkin masih mengirimnya.
// Ditolak terang-terangan lebih baik daripada diterjemahkan diam-diam: jadwal
// yang salah tafsir baru ketahuan saat kontennya tayang di jam yang keliru.
func TestScheduleRejectsOtherFormats(t *testing.T) {
	srv := setup(t)
	key := registerApp(t, srv, "poster-1")

	for _, raw := range []string{"2026-09-12T09:00:00+07:00", "12/09/2026 09:00", "2026-09-12"} {
		res := upload(t, srv, key, raw, "isi-berkas")
		if res.Status != "invalid_input" {
			t.Fatalf("bentuk %q harus ditolak, dapat %+v", raw, res)
		}
	}
}

// TestNoOrphanFileWhenUploadRejected menjaga janji yang paling mudah
// dilanggar: berkas ditulis lebih dulu, barisnya belakangan — jadi unggahan yang
// ditolak sesudah berkasnya jadi harus ikut membersihkan berkasnya.
//
// Berkas yatim tidak muncul di mana pun. Ia cuma memakan disk sampai ada yang
// menyadarinya bertahun-tahun kemudian.
func TestNoOrphanFileWhenUploadRejected(t *testing.T) {
	srv, cfg := setupWith(t)
	key := registerApp(t, srv, "poster-1")

	past := at(-time.Hour)

	// Berkas kosong baru ketahuan sesudah seluruh isinya mengalir — jadi
	// berkasnya sudah terlanjur ditulis saat unggahannya ditolak.
	res := upload(t, srv, key, past, "")
	if res.Status != "invalid_input" {
		t.Fatalf("berkas kosong harus ditolak, dapat %+v", res)
	}

	leftover, err := filepath.Glob(filepath.Join(cfg.FileDir(), "*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(leftover) != 0 {
		t.Fatalf("berkas tertinggal setelah unggahan ditolak: %v", leftover)
	}

	labels := decode[page](t, call(t, srv, "GET", "/v1/contents?scope=all", key, nil, "")).Items
	if len(labels) != 0 {
		t.Fatalf("unggahan yang ditolak tidak boleh meninggalkan baris: %+v", labels)
	}
}

// TestScopeUnusedAndOverdue memisahkan dua pertanyaan yang gampang tertukar:
// "stok apa yang masih punya giliran" dan "apa yang terlewat jadwalnya".
//
// Keduanya berbeda dari ScopeDue — antrean kerja integrator, yang sengaja tidak
// memuat konten yang sedang dipegang aplikasi lain.
func TestScopeUnusedAndOverdue(t *testing.T) {
	srv := setup(t)
	key := registerApp(t, srv, "poster-1")

	past := at(-time.Hour)

	// Satu sudah tayang, satu sedang dipegang, satu masih menunggu jadwal.
	done := decode[content](t, upload(t, srv, key, past, "sudah-tayang"))
	postJSON(t, srv, "/v1/contents/"+done.ID+"/claim", key, `{}`)
	postJSON(t, srv, "/v1/contents/"+done.ID+"/publication", key, `{"published":true}`)

	held := decode[content](t, upload(t, srv, key, past, "sedang-dipegang"))
	postJSON(t, srv, "/v1/contents/"+held.ID+"/claim", key, `{}`)

	upload(t, srv, key, at(24*time.Hour), "besok")

	byScope := func(scope string) []content {
		return decode[page](t, call(t, srv, "GET", "/v1/contents?scope="+scope, key, nil, "")).Items
	}

	// Yang sudah tayang keluar dari stok; yang dipegang integrator masih stok.
	if unused := byScope("unused"); len(unused) != 2 {
		t.Fatalf("stok harusnya dua (dipegang + besok), dapat %+v", unused)
	}

	// Terlewat jadwal: yang dipegang ikut, yang besok tidak, yang sudah tayang
	// tidak.
	overdue := byScope("overdue")
	if len(overdue) != 1 || overdue[0].ID != held.ID {
		t.Fatalf("terlewat jadwal harusnya cuma yang dipegang, dapat %+v", overdue)
	}

	// Antrean kerja tetap kosong — yang dipegang tidak boleh diambil dua kali.
	if due := byScope("due"); len(due) != 0 {
		t.Fatalf("antrean kerja harusnya kosong, dapat %+v", due)
	}
}

// TestTotalCountsBeyondThePage adalah alasan field total ada.
//
// Panel tidak bisa menyimpulkan jumlah halaman dari jumlah baris yang diterima:
// halaman yang kebetulan pas penuh tidak bisa dibedakan dari halaman yang masih
// ada sambungannya.
func TestTotalCountsBeyondThePage(t *testing.T) {
	srv := setup(t)
	key := registerApp(t, srv, "poster-1")

	for i := 0; i < 5; i++ {
		upload(t, srv, key, at(time.Duration(i)*time.Hour), "isi-berkas")
	}

	first := decode[page](t, call(t, srv, "GET", "/v1/contents?limit=2", key, nil, ""))
	if len(first.Items) != 2 || first.Total != 5 {
		t.Fatalf("halaman berisi 2 dari total 5, dapat %d baris total %d", len(first.Items), first.Total)
	}

	// Halaman berikutnya tidak mengulang isi halaman pertama.
	second := decode[page](t, call(t, srv, "GET", "/v1/contents?limit=2&offset=2", key, nil, ""))
	if second.Total != 5 || second.Offset != 2 || second.Items[0].ID == first.Items[0].ID {
		t.Fatalf("halaman kedua salah: %+v", second)
	}

	// Total ikut penyaringnya, bukan jumlah seluruh tabel. Kalau tidak, panel
	// menampilkan halaman kosong untuk scope yang isinya sedikit.
	byScope := decode[page](t, call(t, srv, "GET", "/v1/contents?scope=published", key, nil, ""))
	if byScope.Total != 0 {
		t.Fatalf("total harus ikut scope, dapat %d", byScope.Total)
	}

	// Limit yang tidak masuk akal dibetulkan, dan nilai betulannya dipantulkan
	// supaya panel tidak salah menghitung halaman.
	fixed := decode[page](t, call(t, srv, "GET", "/v1/contents?limit=9999", key, nil, ""))
	if fixed.Limit != 50 {
		t.Fatalf("limit di luar batas harus dibetulkan jadi 50, dapat %d", fixed.Limit)
	}
}

// TestOnlyTheClaimerCanReport menutup lubang yang membuat claim jadi sekadar
// saran.
//
// Tanpa ini, aplikasi yang kalah lomba tinggal mengabaikan jawaban 409, tetap
// memposting, lalu melapor — dan engine mencatatnya sebagai tayang. Kontennya
// tayang dua kali, dan tidak ada galat yang muncul di mana pun.
func TestOnlyTheClaimerCanReport(t *testing.T) {
	srv := setup(t)
	winner := registerApp(t, srv, "poster-1")
	loser := registerApp(t, srv, "poster-2")

	c := decode[content](t, upload(t, srv, winner, at(-time.Hour), "isi-berkas"))
	postJSON(t, srv, "/v1/contents/"+c.ID+"/claim", winner, `{}`)

	res := postJSON(t, srv, "/v1/contents/"+c.ID+"/publication", loser, `{"published":true}`)
	if res.Status != "already_claimed" {
		t.Fatalf("yang bukan pengclaim harus ditolak, dapat %+v", res)
	}

	// Kontennya tidak boleh ikut berubah gara-gara laporan yang ditolak.
	after := decode[content](t, call(t, srv, "GET", "/v1/contents/"+c.ID, winner, nil, ""))
	if after.Status != "claimed" {
		t.Fatalf("status berubah oleh laporan yang ditolak: %+v", after)
	}

	// Yang mengclaim tetap bisa melapor.
	if res := postJSON(t, srv, "/v1/contents/"+c.ID+"/publication", winner, `{"published":true}`); res.Code != 200 {
		t.Fatalf("pengclaim harus bisa melapor: %+v", res)
	}
}
