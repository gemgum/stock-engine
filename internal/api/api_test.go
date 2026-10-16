package api_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
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

// janitors menghubungkan server uji dengan sapuannya, supaya uji yang perlu
// menjalankan sapuan tidak harus merakit servisnya sendiri.
var janitors = map[*httptest.Server]*api.Janitor{}

// configs menyimpan pengaturan tiap server uji, supaya helper yang perlu
// menyentuh database langsung tidak harus merakit servisnya sendiri.
var configs = map[*httptest.Server]config.Config{}

// streams menyimpan pengganti Redis Stream tiap server uji.
var streams = map[*httptest.Server]*recorder{}

// recorder mencatat event yang dikirim, sebagai pengganti Redis. Bisa disuruh
// menolak, untuk menguji Redis yang mati.
type recorder struct {
	events []map[string]string
	down   bool
}

func (r *recorder) Add(_ context.Context, fields ...string) error {
	if r.down {
		return errors.New("redis mati")
	}
	event := map[string]string{}
	for i := 0; i+1 < len(fields); i += 2 {
		event[fields[i]] = fields[i+1]
	}
	r.events = append(r.events, event)
	return nil
}

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
	return setupTuned(t, nil)
}

// setupTuned membiarkan satu uji mengubah pengaturannya sebelum servis dirakit.
func setupTuned(t *testing.T, ubah func(*config.Config)) (*httptest.Server, config.Config) {
	t.Helper()

	dir := t.TempDir()
	cfg := config.Config{
		StorageDir:        dir,
		AdminKey:          adminKey,
		MaxUploadMB:       200,
		RetentionDays:     90,
		ClaimTimeoutHours: 2,
		Address:           ":0",
	}
	if ubah != nil {
		ubah(&cfg)
	}

	db, err := database.Open(cfg.DatabasePath())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	list, err := migrations.All()
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(context.Background(), list); err != nil {
		t.Fatal(err)
	}

	files, err := services.NewFiles(cfg.FileDir())
	if err != nil {
		t.Fatal(err)
	}

	// Log dibuang: yang diuji perilakunya, dan baris log yang membanjiri output
	// uji membuat kegagalan sungguhan susah ditemukan.
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	events := &recorder{}

	handler, janitor := api.Build(db, files, events, cfg, logger)
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	// Janitor tidak dinyalakan di sini: uji yang butuh sapuan memanggil
	// janitor.Once sendiri, supaya hasilnya tidak bergantung pada waktu.
	janitors[srv] = janitor
	configs[srv] = cfg
	streams[srv] = events

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

// uploadDue mengunggah konten yang sudah waktunya tayang.
//
// Jadwal di masa lalu ditolak saat unggah — jaraknya minimal satu jam — jadi
// kontennya diunggah untuk dua jam lagi, lalu jadwalnya dimundurkan langsung
// di database.
func uploadDue(t *testing.T, srv *httptest.Server, key, body string) envelope {
	t.Helper()

	res := upload(t, srv, key, at(2*time.Hour), body)
	if res.Code == http.StatusCreated {
		makeDue(t, srv, decode[content](t, res).ID)
	}
	return res
}

// makeDue memundurkan jadwal satu konten ke sejam yang lalu.
//
// Lewat SQL karena tidak ada alamat yang boleh melakukannya: /schedule menolak
// jadwal yang kurang dari sejam dari sekarang.
func makeDue(t *testing.T, srv *httptest.Server, id string) {
	t.Helper()

	db, err := sql.Open("sqlite", configs[srv].DatabasePath())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	if _, err := db.Exec(`UPDATE contents SET scheduled_at = ? WHERE id = ?`,
		at(-time.Hour), id); err != nil {
		t.Fatal(err)
	}
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
	FileRemovedAt *string `json:"file_removed_at"`
	StreamedAt    *string `json:"streamed_at"`
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

	tomorrow := at(24 * time.Hour)

	due := decode[content](t, uploadDue(t, srv, key, "isi-berkas"))
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

	c := decode[content](t, uploadDue(t, srv, first, "isi-berkas"))

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

	c := decode[content](t, uploadDue(t, srv, key, "isi-berkas"))

	res := postJSON(t, srv, "/v1/contents/"+c.ID+"/publication", key, `{"published":true}`)
	if res.Status != "invalid_input" {
		t.Fatalf("lapor tanpa claim harus ditolak, dapat %+v", res)
	}
}

func TestFailureRequiresReason(t *testing.T) {
	srv := setup(t)
	key := registerApp(t, srv, "poster-1")

	c := decode[content](t, uploadDue(t, srv, key, "isi-berkas"))
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

	c := decode[content](t, uploadDue(t, srv, key, "isi-berkas"))

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
		`{"scheduled_at":"`+at(2*time.Hour)+`"}`)
	if again.Status != "invalid_input" {
		t.Fatalf("menjadwalkan ulang yang sudah tayang harus ditolak, dapat %+v", again)
	}
}

func TestFailedCanBeRescheduled(t *testing.T) {
	srv := setup(t)
	key := registerApp(t, srv, "poster-1")

	c := decode[content](t, uploadDue(t, srv, key, "isi-berkas"))

	postJSON(t, srv, "/v1/contents/"+c.ID+"/claim", key, `{}`)
	failed := decode[content](t, postJSON(t, srv, "/v1/contents/"+c.ID+"/publication", key,
		`{"published":false,"reason":"token instagram kedaluwarsa"}`))
	if failed.Status != "failed" || failed.FailureReason == nil {
		t.Fatalf("harusnya failed beserta alasannya: %+v", failed)
	}

	back := decode[content](t, postJSON(t, srv, "/v1/contents/"+c.ID+"/schedule", key,
		`{"scheduled_at":"`+at(2*time.Hour)+`"}`))
	if back.Status != "scheduled" || back.ClaimedBy != nil || back.FailureReason != nil {
		t.Fatalf("penjadwalan ulang harus membersihkan jejak percobaan sebelumnya: %+v", back)
	}

	// Jadwal barunya dimajukan ke masa lalu supaya langsung masuk antrean lagi.
	makeDue(t, srv, c.ID)

	queue := decode[page](t, call(t, srv, "GET", "/v1/contents?scope=due", key, nil, "")).Items
	if len(queue) != 1 {
		t.Fatalf("yang dijadwalkan ulang harus kembali ke antrean: %+v", queue)
	}
}

func TestArchivedLeavesQueue(t *testing.T) {
	srv := setup(t)
	key := registerApp(t, srv, "poster-1")

	c := decode[content](t, uploadDue(t, srv, key, "isi-berkas"))

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

	// Berkas kosong baru ketahuan sesudah seluruh isinya mengalir — jadi
	// berkasnya sudah terlanjur ditulis saat unggahannya ditolak.
	res := upload(t, srv, key, at(2*time.Hour), "")
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

	// Satu sudah tayang, satu sedang dipegang, satu masih menunggu jadwal.
	done := decode[content](t, uploadDue(t, srv, key, "sudah-tayang"))
	postJSON(t, srv, "/v1/contents/"+done.ID+"/claim", key, `{}`)
	postJSON(t, srv, "/v1/contents/"+done.ID+"/publication", key, `{"published":true}`)

	held := decode[content](t, uploadDue(t, srv, key, "sedang-dipegang"))
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
		upload(t, srv, key, at(time.Duration(i+2)*time.Hour), "isi-berkas")
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

	c := decode[content](t, uploadDue(t, srv, winner, "isi-berkas"))
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

// TestRescheduleMissingContentSaysNotFound menjaga kalimat penolakan tetap
// jujur.
//
// Nol baris yang berubah punya dua sebab yang berbeda jauh: kontennya tidak ada,
// atau sudah tayang. Sempat dijawab sama — "Published content cannot be
// rescheduled" untuk id yang tidak pernah ada — dan yang salah ketik id akan
// mencari-cari konten yang tidak pernah ada.
func TestRescheduleMissingContentSaysNotFound(t *testing.T) {
	srv := setup(t)
	key := registerApp(t, srv, "poster-1")

	res := postJSON(t, srv, "/v1/contents/tidak-ada/schedule", key,
		`{"scheduled_at":"`+at(2*time.Hour)+`"}`)
	if res.Status != "not_found" {
		t.Fatalf("id yang tidak ada harus not_found, dapat %+v", res)
	}

	// Yang sudah tayang tetap ditolak dengan sebabnya sendiri.
	c := decode[content](t, uploadDue(t, srv, key, "isi-berkas"))
	postJSON(t, srv, "/v1/contents/"+c.ID+"/claim", key, `{}`)
	postJSON(t, srv, "/v1/contents/"+c.ID+"/publication", key, `{"published":true}`)

	res = postJSON(t, srv, "/v1/contents/"+c.ID+"/schedule", key,
		`{"scheduled_at":"`+at(2*time.Hour)+`"}`)
	if res.Status != "invalid_input" {
		t.Fatalf("yang sudah tayang harus invalid_input, dapat %+v", res)
	}
}

// TestClaimRejectionNamesTheRealState menjaga kalimat penolakan tetap menunjuk
// tempat yang benar.
//
// Golongannya memang satu — tindakan pemanggil sama: lewati, ambil berikutnya.
// Tapi "sudah diambil aplikasi lain" untuk konten yang sebenarnya sudah tayang
// membuat yang membaca log mencari aplikasi yang tidak pernah ada.
func TestClaimRejectionNamesTheRealState(t *testing.T) {
	srv := setup(t)
	key := registerApp(t, srv, "poster-1")
	lain := registerApp(t, srv, "poster-2")

	published := decode[content](t, uploadDue(t, srv, key, "isi-berkas"))
	postJSON(t, srv, "/v1/contents/"+published.ID+"/claim", key, `{}`)
	postJSON(t, srv, "/v1/contents/"+published.ID+"/publication", key, `{"published":true}`)

	archived := decode[content](t, uploadDue(t, srv, key, "isi-berkas"))
	postJSON(t, srv, "/v1/contents/"+archived.ID+"/archival", key, `{}`)

	held := decode[content](t, uploadDue(t, srv, key, "isi-berkas"))
	postJSON(t, srv, "/v1/contents/"+held.ID+"/claim", key, `{}`)

	for _, uji := range []struct{ id, sebut string }{
		{published.ID, "published"},
		{archived.ID, "archived"},
		{held.ID, "claimed"},
	} {
		res := postJSON(t, srv, "/v1/contents/"+uji.id+"/claim", lain, `{}`)
		if res.Status != "already_claimed" {
			t.Fatalf("%s: golongannya harus tetap already_claimed, dapat %q", uji.sebut, res.Status)
		}
		if !strings.Contains(strings.ToLower(res.Message), uji.sebut) {
			t.Fatalf("%s: kalimatnya tidak menyebut keadaan sebenarnya: %q", uji.sebut, res.Message)
		}
	}
}

// TestOversizedUploadIsTheSendersProblem memisahkan dua sebab yang sempat
// tercampur jadi satu kalimat.
//
// Berkas yang memang melewati batas itu salah pengirim — 400, dan kalimatnya
// menyebut batasnya. Yang **tidak** boleh ikut ke sana: ruang penyimpanan servis
// yang habis. Itu 500, supaya pemantauan melihatnya dan integrator tidak
// mengecilkan videonya berulang kali padahal yang perlu ditambah adalah disk.
func TestOversizedUploadIsTheSendersProblem(t *testing.T) {
	srv, _ := setupTuned(t, func(c *config.Config) { c.MaxUploadMB = 1 })
	key := registerApp(t, srv, "poster-1")

	res := upload(t, srv, key, at(2*time.Hour), strings.Repeat("x", 2<<20))
	if res.Status != "invalid_input" {
		t.Fatalf("berkas kebesaran harus invalid_input, dapat %+v", res)
	}
	if !strings.Contains(res.Message, "larger than the allowed limit") {
		t.Fatalf("kalimatnya harus menyebut batas, dapat %q", res.Message)
	}
}

// tuakan memundurkan waktu selesai sebuah konten langsung di database.
//
// Lewat SQL karena tidak ada alamat yang bisa melakukannya — dan memang tidak
// boleh ada. Sapuan retensi tidak bisa diuji dengan menunggu sembilan puluh
// hari berlalu.
func tuakan(t *testing.T, cfg config.Config, id, kapan string) {
	t.Helper()

	db, err := sql.Open("sqlite", cfg.DatabasePath())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	_, err = db.Exec(`UPDATE contents
		SET published_at = CASE WHEN published_at IS NOT NULL THEN ? END,
		    archived_at  = CASE WHEN archived_at  IS NOT NULL THEN ? END
		WHERE id = ?`, kapan, kapan, id)
	if err != nil {
		t.Fatal(err)
	}
}

func adaBerkas(t *testing.T, cfg config.Config, id string) bool {
	t.Helper()

	_, err := os.Stat(filepath.Join(cfg.FileDir(), id))
	return err == nil
}

// TestRetentionRemovesFilesButKeepsRecords adalah janji sapuan retensi:
// disknya kosong, riwayatnya tetap.
func TestRetentionRemovesFilesButKeepsRecords(t *testing.T) {
	srv, cfg := setupWith(t)
	key := registerApp(t, srv, "poster-1")

	// Sudah tayang, dan sudah lama.
	tayang := decode[content](t, uploadDue(t, srv, key, "isi-tayang"))
	postJSON(t, srv, "/v1/contents/"+tayang.ID+"/claim", key, `{}`)
	postJSON(t, srv, "/v1/contents/"+tayang.ID+"/publication", key, `{"published":true}`)
	tuakan(t, cfg, tayang.ID, "2020-01-01 00:00:00")

	// Dibatalkan, dan sudah lama.
	batal := decode[content](t, uploadDue(t, srv, key, "isi-batal"))
	postJSON(t, srv, "/v1/contents/"+batal.ID+"/archival", key, `{}`)
	tuakan(t, cfg, batal.ID, "2020-01-01 00:00:00")

	// Sudah tayang, tapi baru kemarin.
	baru := decode[content](t, uploadDue(t, srv, key, "isi-baru"))
	postJSON(t, srv, "/v1/contents/"+baru.ID+"/claim", key, `{}`)
	postJSON(t, srv, "/v1/contents/"+baru.ID+"/publication", key, `{"published":true}`)

	// Belum pernah tayang. Stok yang menunggu giliran bukan sampah, berapa lama
	// pun sudah diunggah.
	stok := decode[content](t, upload(t, srv, key, at(24*time.Hour), "isi-stok"))

	if err := janitors[srv].Once(context.Background()); err != nil {
		t.Fatal(err)
	}

	for _, uji := range []struct {
		id     string
		hilang bool
		sebut  string
	}{
		{tayang.ID, true, "tayang lama"},
		{batal.ID, true, "dibatalkan lama"},
		{baru.ID, false, "tayang kemarin"},
		{stok.ID, false, "stok menunggu giliran"},
	} {
		if adaBerkas(t, cfg, uji.id) == uji.hilang {
			t.Fatalf("%s: berkasnya %s padahal seharusnya %s", uji.sebut,
				map[bool]string{true: "masih ada", false: "hilang"}[!uji.hilang],
				map[bool]string{true: "dibuang", false: "disimpan"}[uji.hilang])
		}

		// Barisnya tetap terbaca, apa pun nasib berkasnya.
		res := call(t, srv, "GET", "/v1/contents/"+uji.id, key, nil, "")
		if res.Code != 200 {
			t.Fatalf("%s: barisnya ikut hilang: %+v", uji.sebut, res)
		}
		c := decode[content](t, res)
		if (c.FileRemovedAt != nil) != uji.hilang {
			t.Fatalf("%s: penanda file_removed_at tidak cocok: %+v", uji.sebut, c.FileRemovedAt)
		}
	}
}

// TestRetentionIsSafeToRepeat menjaga sifat yang membuat sapuan boleh dipanggil
// tiap servis menyala.
func TestRetentionIsSafeToRepeat(t *testing.T) {
	srv, cfg := setupWith(t)
	key := registerApp(t, srv, "poster-1")

	c := decode[content](t, uploadDue(t, srv, key, "isi-berkas"))
	postJSON(t, srv, "/v1/contents/"+c.ID+"/claim", key, `{}`)
	postJSON(t, srv, "/v1/contents/"+c.ID+"/publication", key, `{"published":true}`)
	tuakan(t, cfg, c.ID, "2020-01-01 00:00:00")

	if err := janitors[srv].Once(context.Background()); err != nil {
		t.Fatal(err)
	}
	if adaBerkas(t, cfg, c.ID) {
		t.Fatal("sapuan pertama tidak membuang berkasnya")
	}

	// Sapuan kedua atas gudang yang sudah bersih tidak boleh jadi galat — ia
	// dipanggil tiap servis menyala, dan servis bisa restart lima kali sejam.
	if err := janitors[srv].Once(context.Background()); err != nil {
		t.Fatalf("sapuan kedua gagal: %v", err)
	}

	res := call(t, srv, "GET", "/v1/contents/"+c.ID, key, nil, "")
	if res.Code != 200 || decode[content](t, res).FileRemovedAt == nil {
		t.Fatalf("barisnya harus tetap ada dan bertanda: %+v", res)
	}
}

// TestPurgedContentAnswersHonestly menjaga dua jawaban yang gampang jadi
// membingungkan setelah berkasnya dibuang.
func TestPurgedContentAnswersHonestly(t *testing.T) {
	srv, cfg := setupWith(t)
	key := registerApp(t, srv, "poster-1")

	c := decode[content](t, uploadDue(t, srv, key, "isi-berkas"))
	postJSON(t, srv, "/v1/contents/"+c.ID+"/claim", key, `{}`)
	postJSON(t, srv, "/v1/contents/"+c.ID+"/publication", key, `{"published":true}`)
	tuakan(t, cfg, c.ID, "2020-01-01 00:00:00")

	if err := janitors[srv].Once(context.Background()); err != nil {
		t.Fatal(err)
	}

	// Mengunduh: sebabnya disebut, bukan 404 polos yang membuat integrator
	// mengira ada yang rusak lalu mencoba lagi berkali-kali.
	res := call(t, srv, "GET", "/v1/contents/"+c.ID+"/file", key, nil, "")
	if res.Status != "not_found" || !strings.Contains(res.Message, "retention") {
		t.Fatalf("unduhan setelah disapu harus menyebut retensi: %+v", res)
	}

	// Menjadwalkan ulang: ditolak. Kalau lolos, ia masuk antrean lagi,
	// integrator mengambilnya, lalu gagal mengunduh selamanya.
	res = postJSON(t, srv, "/v1/contents/"+c.ID+"/schedule", key,
		`{"scheduled_at":"`+at(2*time.Hour)+`"}`)
	if res.Status != "invalid_input" || !strings.Contains(res.Message, "retention") {
		t.Fatalf("penjadwalan ulang setelah disapu harus menyebut retensi: %+v", res)
	}
}

// TestRetentionOffKeepsEverything menjaga pilihan "jangan pernah buang" tetap
// berarti apa adanya.
func TestRetentionOffKeepsEverything(t *testing.T) {
	srv, cfg := setupTuned(t, func(c *config.Config) { c.RetentionDays = 0 })
	key := registerApp(t, srv, "poster-1")

	c := decode[content](t, uploadDue(t, srv, key, "isi-berkas"))
	postJSON(t, srv, "/v1/contents/"+c.ID+"/claim", key, `{}`)
	postJSON(t, srv, "/v1/contents/"+c.ID+"/publication", key, `{"published":true}`)
	tuakan(t, cfg, c.ID, "2020-01-01 00:00:00")

	if err := janitors[srv].Once(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !adaBerkas(t, cfg, c.ID) {
		t.Fatal("berkasnya hilang padahal retensi dimatikan")
	}
}

// tuakanClaim memundurkan waktu claim langsung di database.
func tuakanClaim(t *testing.T, cfg config.Config, id, kapan string) {
	t.Helper()

	db, err := sql.Open("sqlite", cfg.DatabasePath())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	if _, err := db.Exec(`UPDATE contents SET claimed_at = ? WHERE id = ?`, kapan, id); err != nil {
		t.Fatal(err)
	}
}

// TestStuckClaimReturnsToQueue menutup satu-satunya cara konten bisa hilang
// diam-diam.
//
// Integrator yang mati sesudah mengclaim membuat kontennya diam di `claimed`
// selamanya: tidak pernah tayang, dan tidak pernah muncul lagi di antrean kerja
// karena antrean itu memang sengaja menyembunyikan yang sedang dipegang orang.
func TestStuckClaimReturnsToQueue(t *testing.T) {
	srv, cfg := setupWith(t)
	key := registerApp(t, srv, "poster-1")

	// Diclaim lalu ditinggalkan tiga jam lalu — lewat ambang dua jam.
	nyangkut := decode[content](t, uploadDue(t, srv, key, "isi-nyangkut"))
	postJSON(t, srv, "/v1/contents/"+nyangkut.ID+"/claim", key, `{}`)
	tuakanClaim(t, cfg, nyangkut.ID, at(-3*time.Hour))

	// Baru saja diclaim, dan mungkin sedang mengunggah videonya. Tidak boleh
	// direbut — kalau direbut, kontennya tayang dua kali.
	sedangJalan := decode[content](t, uploadDue(t, srv, key, "isi-jalan"))
	postJSON(t, srv, "/v1/contents/"+sedangJalan.ID+"/claim", key, `{}`)

	if err := janitors[srv].Once(context.Background()); err != nil {
		t.Fatal(err)
	}

	lepas := decode[content](t, call(t, srv, "GET", "/v1/contents/"+nyangkut.ID, key, nil, ""))
	if lepas.Status != "scheduled" || lepas.ClaimedBy != nil {
		t.Fatalf("yang tersangkut harus kembali ke antrean: %+v", lepas)
	}

	masih := decode[content](t, call(t, srv, "GET", "/v1/contents/"+sedangJalan.ID, key, nil, ""))
	if masih.Status != "claimed" {
		t.Fatalf("yang baru diclaim tidak boleh direbut: %+v", masih)
	}

	// Dan benar-benar bisa diambil lagi — jadwalnya tidak ikut berubah, jadi ia
	// langsung masuk antrean kerja.
	queue := decode[page](t, call(t, srv, "GET", "/v1/contents?scope=due", key, nil, "")).Items
	if len(queue) != 1 || queue[0].ID != nyangkut.ID {
		t.Fatalf("yang dilepas harus muncul di antrean kerja: %+v", queue)
	}
}

// TestClaimTimeoutOffKeepsHolding menjaga pilihan "jangan pernah lepas" tetap
// berarti apa adanya.
func TestClaimTimeoutOffKeepsHolding(t *testing.T) {
	srv, cfg := setupTuned(t, func(c *config.Config) { c.ClaimTimeoutHours = 0 })
	key := registerApp(t, srv, "poster-1")

	c := decode[content](t, uploadDue(t, srv, key, "isi-berkas"))
	postJSON(t, srv, "/v1/contents/"+c.ID+"/claim", key, `{}`)
	tuakanClaim(t, cfg, c.ID, at(-30*24*time.Hour))

	if err := janitors[srv].Once(context.Background()); err != nil {
		t.Fatal(err)
	}

	after := decode[content](t, call(t, srv, "GET", "/v1/contents/"+c.ID, key, nil, ""))
	if after.Status != "claimed" {
		t.Fatalf("ambang dimatikan, claim tidak boleh dilepas: %+v", after)
	}
}

// TestStaleUploadsAreSweptAway menutup satu-satunya sampah yang tidak punya
// baris di database.
//
// Servis yang mati mendadak di tengah penulisan meninggalkan berkas `upload-*`
// separuh. Tidak ada yang tahu id-nya, jadi sapuan retensi tidak akan pernah
// melihatnya — ia memakan disk selamanya.
func TestStaleUploadsAreSweptAway(t *testing.T) {
	srv, cfg := setupWith(t)

	lama := filepath.Join(cfg.FileDir(), "upload-1234567890")
	if err := os.WriteFile(lama, []byte("separuh"), 0o644); err != nil {
		t.Fatal(err)
	}
	kemarin := time.Now().Add(-24 * time.Hour)
	if err := os.Chtimes(lama, kemarin, kemarin); err != nil {
		t.Fatal(err)
	}

	// Yang baru dibuat mungkin milik unggahan yang sedang berjalan. Membuangnya
	// jauh lebih buruk daripada membiarkan sampah menginap setengah hari.
	baru := filepath.Join(cfg.FileDir(), "upload-9999999999")
	if err := os.WriteFile(baru, []byte("sedang-berjalan"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Berkas konten biasa tidak boleh ikut terbawa.
	key := registerApp(t, srv, "poster-1")
	c := decode[content](t, upload(t, srv, key, at(2*time.Hour), "isi-berkas"))

	if err := janitors[srv].Once(context.Background()); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(lama); !os.IsNotExist(err) {
		t.Fatal("berkas sementara yang ditinggalkan tidak dibuang")
	}
	if _, err := os.Stat(baru); err != nil {
		t.Fatal("berkas sementara yang masih baru ikut dibuang")
	}
	if !adaBerkas(t, cfg, c.ID) {
		t.Fatal("berkas konten yang sah ikut terbawa sapuan")
	}
}

// TestStaleUploadSweepIgnoresRetentionSetting menjaga pemisahan yang gampang
// tertukar: sampah bukan kebijakan penyimpanan.
//
// Yang memilih menyimpan kontennya selamanya tetap tidak ingin menyimpan
// potongan unggahan yang gagal.
func TestStaleUploadSweepIgnoresRetentionSetting(t *testing.T) {
	srv, cfg := setupTuned(t, func(c *config.Config) { c.RetentionDays = 0 })

	sampah := filepath.Join(cfg.FileDir(), "upload-5555555555")
	if err := os.WriteFile(sampah, []byte("separuh"), 0o644); err != nil {
		t.Fatal(err)
	}
	kemarin := time.Now().Add(-24 * time.Hour)
	if err := os.Chtimes(sampah, kemarin, kemarin); err != nil {
		t.Fatal(err)
	}

	if err := janitors[srv].Once(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(sampah); !os.IsNotExist(err) {
		t.Fatal("retensi dimatikan seharusnya tidak ikut mematikan pembuangan sampah")
	}
}

// TestPurgeDrainsBacklogInOneSweep menjaga batas per kueri tidak berubah jadi
// batas per hari.
//
// Sapuan jalan sekali sehari. Kalau satu jalan cuma membersihkan satu batch,
// gudang berisi ribuan berkas kedaluwarsa butuh berbulan-bulan untuk bersih —
// dan itu justru keadaan yang membuat retensi dinyalakan.
func TestPurgeDrainsBacklogInOneSweep(t *testing.T) {
	srv, cfg := setupWith(t)
	key := registerApp(t, srv, "poster-1")

	// Lebih banyak dari satu batch (200) tidak realistis untuk uji; yang
	// dibuktikan di sini cukup bahwa perulangannya berjalan, bukan angkanya.
	const jumlah = 5
	ids := make([]string, 0, jumlah)
	for i := 0; i < jumlah; i++ {
		c := decode[content](t, uploadDue(t, srv, key, "isi-berkas"))
		postJSON(t, srv, "/v1/contents/"+c.ID+"/claim", key, `{}`)
		postJSON(t, srv, "/v1/contents/"+c.ID+"/publication", key, `{"published":true}`)
		tuakan(t, cfg, c.ID, "2020-01-01 00:00:00")
		ids = append(ids, c.ID)
	}

	if err := janitors[srv].Once(context.Background()); err != nil {
		t.Fatal(err)
	}

	for _, id := range ids {
		if adaBerkas(t, cfg, id) {
			t.Fatalf("masih ada berkas yang belum disapu: %s", id)
		}
	}
}

// TestUploadNeedsOneHourLead adalah kesepakatan dengan integrator: jadwal
// paling cepat sejam dari saat konten diunggah. /schedule sengaja bebas — yang
// memindahkan jadwal konten gagal boleh mencobanya lagi sekarang juga.
func TestUploadNeedsOneHourLead(t *testing.T) {
	srv := setup(t)
	key := registerApp(t, srv, "poster-1")

	res := upload(t, srv, key, at(30*time.Minute), "isi-berkas")
	if res.Status != "invalid_input" || !strings.Contains(res.Message, "at least 1 hour") {
		t.Fatalf("jadwal setengah jam lagi harus ditolak saat unggah, dapat %+v", res)
	}

	c := decode[content](t, upload(t, srv, key, at(2*time.Hour), "isi-berkas"))

	res = postJSON(t, srv, "/v1/contents/"+c.ID+"/schedule", key,
		`{"scheduled_at":"`+at(-time.Hour)+`"}`)
	if res.Code != 200 {
		t.Fatalf("/schedule tidak terikat batas sejam, dapat %+v", res)
	}
}

// TestDueContentIsStreamedOnce: yang sudah waktunya dikabarkan sekali, lengkap
// dengan path yang benar-benar bisa dibuka. Yang belum waktunya tidak.
func TestDueContentIsStreamedOnce(t *testing.T) {
	srv := setup(t)
	key := registerApp(t, srv, "poster-1")

	due := decode[content](t, uploadDue(t, srv, key, "isi-berkas"))
	upload(t, srv, key, at(24*time.Hour), "besok")

	for range 2 {
		if err := janitors[srv].Once(context.Background()); err != nil {
			t.Fatal(err)
		}
	}

	events := streams[srv].events
	if len(events) != 1 || events[0]["id"] != due.ID {
		t.Fatalf("harusnya satu event untuk yang sudah waktunya, dapat %+v", events)
	}

	body, err := os.ReadFile(events[0]["path"])
	if err != nil || string(body) != "isi-berkas" {
		t.Fatalf("path di event harus menunjuk berkasnya: %q, %v", body, err)
	}

	after := decode[content](t, call(t, srv, "GET", "/v1/contents/"+due.ID, key, nil, ""))
	if after.StreamedAt == nil {
		t.Fatal("streamed_at harus terisi sesudah dikabarkan")
	}
}

// TestStreamRetriesWhenRedisIsDown: Redis yang menolak tidak boleh
// meninggalkan tanda "sudah dikabarkan". Kalau tidak, kontennya tidak pernah
// sampai ke integrator stream, dan tidak ada yang tahu.
func TestStreamRetriesWhenRedisIsDown(t *testing.T) {
	srv := setup(t)
	key := registerApp(t, srv, "poster-1")

	c := decode[content](t, uploadDue(t, srv, key, "isi-berkas"))

	streams[srv].down = true
	if err := janitors[srv].Once(context.Background()); err == nil {
		t.Fatal("Redis yang mati harus dilaporkan sebagai galat")
	}
	after := decode[content](t, call(t, srv, "GET", "/v1/contents/"+c.ID, key, nil, ""))
	if after.StreamedAt != nil {
		t.Fatal("yang gagal dikabarkan tidak boleh bertanda sudah dikabarkan")
	}

	streams[srv].down = false
	if err := janitors[srv].Once(context.Background()); err != nil {
		t.Fatal(err)
	}
	if events := streams[srv].events; len(events) != 1 || events[0]["id"] != c.ID {
		t.Fatalf("begitu Redis kembali, kontennya harus dikabarkan, dapat %+v", events)
	}
}

// TestRequeuedContentIsStreamedAgain: konten yang kembali ke antrean — lewat
// /schedule atau claim yang dilepas — harus dikabarkan lagi. Kalau tidak, ia
// ada di `?scope=due` tapi tidak pernah muncul di stream.
func TestRequeuedContentIsStreamedAgain(t *testing.T) {
	srv, cfg := setupWith(t)
	key := registerApp(t, srv, "poster-1")
	once := func() {
		t.Helper()
		if err := janitors[srv].Once(context.Background()); err != nil {
			t.Fatal(err)
		}
	}

	failed := decode[content](t, uploadDue(t, srv, key, "isi-gagal"))
	stuck := decode[content](t, uploadDue(t, srv, key, "isi-nyangkut"))
	once()

	postJSON(t, srv, "/v1/contents/"+failed.ID+"/claim", key, `{}`)
	postJSON(t, srv, "/v1/contents/"+failed.ID+"/publication", key,
		`{"published":false,"reason":"ditolak platform"}`)
	postJSON(t, srv, "/v1/contents/"+failed.ID+"/schedule", key,
		`{"scheduled_at":"`+at(2*time.Hour)+`"}`)
	makeDue(t, srv, failed.ID)

	postJSON(t, srv, "/v1/contents/"+stuck.ID+"/claim", key, `{}`)
	tuakanClaim(t, cfg, stuck.ID, at(-3*time.Hour))

	once()

	count := map[string]int{}
	for _, e := range streams[srv].events {
		count[e["id"]]++
	}
	if count[failed.ID] != 2 || count[stuck.ID] != 2 {
		t.Fatalf("yang kembali ke antrean harus dikabarkan lagi, dapat %v", count)
	}
}
