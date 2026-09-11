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
	"github.com/gemgum/stock-engine/internal/services"
	"github.com/gemgum/stock-engine/migrations"
)

const adminKey = "kunci-induk-untuk-pengujian-yang-panjangnya-cukup"

// siapkan merakit servis lengkap di atas SQLite sungguhan.
//
// Bukan tiruan: yang paling ingin dibuktikan justru perilaku database — claim
// yang cuma boleh menang sekali, dan urutan waktu yang dibandingkan sebagai
// teks. Tiruan akan menjawab apa pun yang kita suruh.
func siapkan(t *testing.T) *httptest.Server {
	srv, _ := siapkanLengkap(t)
	return srv
}

// siapkanLengkap ikut mengembalikan pengaturannya, untuk uji yang perlu
// mengintip isi folder penyimpanan.
func siapkanLengkap(t *testing.T) (*httptest.Server, config.Config) {
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

type jawaban struct {
	Code    int             `json:"code"`
	Status  string          `json:"status"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

func panggil(t *testing.T, srv *httptest.Server, method, path, key string,
	body io.Reader, contentType string) jawaban {
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

	var out jawaban
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		t.Fatalf("%s %s: jawaban bukan JSON: %v", method, path, err)
	}
	if out.Code != res.StatusCode {
		t.Fatalf("%s %s: code di amplop %d, di header %d", method, path, out.Code, res.StatusCode)
	}
	return out
}

func kirimJSON(t *testing.T, srv *httptest.Server, path, key, body string) jawaban {
	t.Helper()
	return panggil(t, srv, "POST", path, key, bytes.NewBufferString(body), "application/json")
}

// unggah mengirim satu berkas beserta jadwalnya.
func unggah(t *testing.T, srv *httptest.Server, key, scheduledAt, isi string) jawaban {
	t.Helper()

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)

	bagian, err := mw.CreateFormFile("file", "poster.jpg")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bagian.Write([]byte(isi)); err != nil {
		t.Fatal(err)
	}
	_ = mw.WriteField("scheduled_at", scheduledAt)
	_ = mw.WriteField("caption", "halo dunia")
	_ = mw.WriteField("platform", "instagram")
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}

	return panggil(t, srv, "POST", "/v1/contents", key, &buf, mw.FormDataContentType())
}

func baca[T any](t *testing.T, res jawaban) T {
	t.Helper()

	var out T
	if err := json.Unmarshal(res.Data, &out); err != nil {
		t.Fatalf("data tidak terbaca: %v (%s)", err, res.Data)
	}
	return out
}

type konten struct {
	ID            string  `json:"id"`
	Status        string  `json:"status"`
	Size          int64   `json:"size"`
	Checksum      string  `json:"checksum"`
	ClaimedBy     *string `json:"claimed_by"`
	FailureReason *string `json:"failure_reason"`
}

// daftarkanApp menerbitkan satu kunci aplikasi lewat kunci induk.
func daftarkanApp(t *testing.T, srv *httptest.Server, nama string) string {
	t.Helper()

	res := kirimJSON(t, srv, "/v1/apps", adminKey, `{"name":"`+nama+`"}`)
	if res.Code != http.StatusCreated {
		t.Fatalf("daftar app gagal: %+v", res)
	}

	app := baca[struct {
		Key string `json:"key"`
	}](t, res)

	if app.Key == "" {
		t.Fatal("kunci tidak ikut di balasan, padahal cuma muncul sekali")
	}
	return app.Key
}

func TestKunciDitolakKalauTidakDikenal(t *testing.T) {
	srv := siapkan(t)

	res := panggil(t, srv, "GET", "/v1/contents", "sk_bukan-kunci-siapa-siapa", nil, "")
	if res.Status != "unauthorized" {
		t.Fatalf("kunci asing harus unauthorized, dapat %q", res.Status)
	}
}

func TestKunciAplikasiTidakBolehMendaftarkanAplikasi(t *testing.T) {
	srv := siapkan(t)
	key := daftarkanApp(t, srv, "poster-1")

	res := kirimJSON(t, srv, "/v1/apps", key, `{"name":"aplikasi-selundupan"}`)
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("kunci aplikasi harus ditolak di /v1/apps, dapat %+v", res)
	}
}

func TestKunciYangDicabutBerhentiBekerja(t *testing.T) {
	srv := siapkan(t)
	key := daftarkanApp(t, srv, "poster-1")

	daftar := baca[[]struct {
		ID string `json:"id"`
	}](t, panggil(t, srv, "GET", "/v1/apps", adminKey, nil, ""))
	if len(daftar) != 1 {
		t.Fatalf("harusnya ada satu aplikasi, dapat %d", len(daftar))
	}

	if res := kirimJSON(t, srv, "/v1/apps/"+daftar[0].ID+"/revocation", adminKey, `{}`); res.Code != 200 {
		t.Fatalf("pencabutan gagal: %+v", res)
	}

	if res := panggil(t, srv, "GET", "/v1/contents", key, nil, ""); res.Status != "unauthorized" {
		t.Fatalf("kunci yang dicabut harus berhenti bekerja, dapat %q", res.Status)
	}
}

func TestScopeYangTidakDikenalDitolak(t *testing.T) {
	srv := siapkan(t)
	key := daftarkanApp(t, srv, "poster-1")

	res := panggil(t, srv, "GET", "/v1/contents?scope=besok", key, nil, "")
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
	srv := siapkan(t)
	key := daftarkanApp(t, srv, "poster-1")

	lampau := time.Now().Add(-time.Hour).Format(time.RFC3339)
	besok := time.Now().Add(24 * time.Hour).Format(time.RFC3339)

	siap := baca[konten](t, unggah(t, srv, key, lampau, "isi-berkas"))
	unggah(t, srv, key, besok, "belum-waktunya")

	antrean := baca[[]konten](t, panggil(t, srv, "GET", "/v1/contents?scope=due", key, nil, ""))
	if len(antrean) != 1 || antrean[0].ID != siap.ID {
		t.Fatalf("antrean harusnya berisi satu yang sudah waktunya, dapat %+v", antrean)
	}
}

// TestClaimCumaMenangSekali adalah alasan seluruh mekanisme claim ada.
func TestClaimCumaMenangSekali(t *testing.T) {
	srv := siapkan(t)
	satu := daftarkanApp(t, srv, "poster-1")
	dua := daftarkanApp(t, srv, "poster-2")

	lampau := time.Now().Add(-time.Hour).Format(time.RFC3339)
	c := baca[konten](t, unggah(t, srv, satu, lampau, "isi-berkas"))

	menang := kirimJSON(t, srv, "/v1/contents/"+c.ID+"/claim", satu, `{}`)
	if menang.Code != 200 || baca[konten](t, menang).Status != "claimed" {
		t.Fatalf("claim pertama harus menang: %+v", menang)
	}

	kalah := kirimJSON(t, srv, "/v1/contents/"+c.ID+"/claim", dua, `{}`)
	if kalah.Status != "already_claimed" || kalah.Code != http.StatusConflict {
		t.Fatalf("claim kedua harus already_claimed 409, dapat %+v", kalah)
	}

	// Yang sudah diambil tidak boleh muncul lagi di antrean, kalau tidak
	// integrator ketiga akan memposting konten yang sama.
	antrean := baca[[]konten](t, panggil(t, srv, "GET", "/v1/contents?scope=due", satu, nil, ""))
	if len(antrean) != 0 {
		t.Fatalf("yang sudah diclaim masih muncul di antrean: %+v", antrean)
	}
}

func TestClaimKontenYangTidakAda(t *testing.T) {
	srv := siapkan(t)
	key := daftarkanApp(t, srv, "poster-1")

	res := kirimJSON(t, srv, "/v1/contents/tidak-ada/claim", key, `{}`)
	if res.Status != "not_found" {
		t.Fatalf("id asing harus not_found, bukan %q", res.Status)
	}
}

func TestLaporanHanyaUntukYangSudahDiclaim(t *testing.T) {
	srv := siapkan(t)
	key := daftarkanApp(t, srv, "poster-1")

	lampau := time.Now().Add(-time.Hour).Format(time.RFC3339)
	c := baca[konten](t, unggah(t, srv, key, lampau, "isi-berkas"))

	res := kirimJSON(t, srv, "/v1/contents/"+c.ID+"/publication", key, `{"published":true}`)
	if res.Status != "invalid_input" {
		t.Fatalf("lapor tanpa claim harus ditolak, dapat %+v", res)
	}
}

func TestGagalWajibMenyebutAlasan(t *testing.T) {
	srv := siapkan(t)
	key := daftarkanApp(t, srv, "poster-1")

	lampau := time.Now().Add(-time.Hour).Format(time.RFC3339)
	c := baca[konten](t, unggah(t, srv, key, lampau, "isi-berkas"))
	kirimJSON(t, srv, "/v1/contents/"+c.ID+"/claim", key, `{}`)

	res := kirimJSON(t, srv, "/v1/contents/"+c.ID+"/publication", key, `{"published":false}`)
	if res.Status != "invalid_input" {
		t.Fatalf("gagal tanpa alasan harus ditolak, dapat %+v", res)
	}
}

// TestAlurPenuh menelusuri satu konten dari unggah sampai tayang, lalu satu lagi
// yang gagal dan dijadwalkan ulang.
func TestAlurPenuh(t *testing.T) {
	srv := siapkan(t)
	key := daftarkanApp(t, srv, "poster-1")

	lampau := time.Now().Add(-time.Hour).Format(time.RFC3339)
	c := baca[konten](t, unggah(t, srv, key, lampau, "isi-berkas"))

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
	isi, _ := io.ReadAll(res.Body)
	_ = res.Body.Close()
	if string(isi) != "isi-berkas" {
		t.Fatalf("isi berkas berubah: %q", isi)
	}

	kirimJSON(t, srv, "/v1/contents/"+c.ID+"/claim", key, `{}`)

	tayang := baca[konten](t, kirimJSON(t, srv, "/v1/contents/"+c.ID+"/publication", key,
		`{"published":true}`))
	if tayang.Status != "published" {
		t.Fatalf("harusnya published, dapat %q", tayang.Status)
	}

	// Yang sudah tayang tidak boleh dijadwalkan ulang — itu akan memposting
	// ulang konten yang sama.
	ulang := kirimJSON(t, srv, "/v1/contents/"+c.ID+"/schedule", key,
		`{"scheduled_at":"`+time.Now().Format(time.RFC3339)+`"}`)
	if ulang.Status != "invalid_input" {
		t.Fatalf("menjadwalkan ulang yang sudah tayang harus ditolak, dapat %+v", ulang)
	}
}

func TestGagalBisaDijadwalkanUlang(t *testing.T) {
	srv := siapkan(t)
	key := daftarkanApp(t, srv, "poster-1")

	lampau := time.Now().Add(-time.Hour).Format(time.RFC3339)
	c := baca[konten](t, unggah(t, srv, key, lampau, "isi-berkas"))

	kirimJSON(t, srv, "/v1/contents/"+c.ID+"/claim", key, `{}`)
	gagal := baca[konten](t, kirimJSON(t, srv, "/v1/contents/"+c.ID+"/publication", key,
		`{"published":false,"reason":"token instagram kedaluwarsa"}`))
	if gagal.Status != "failed" || gagal.FailureReason == nil {
		t.Fatalf("harusnya failed beserta alasannya: %+v", gagal)
	}

	// Dijadwalkan ulang ke masa lalu supaya langsung masuk antrean lagi.
	kembali := baca[konten](t, kirimJSON(t, srv, "/v1/contents/"+c.ID+"/schedule", key,
		`{"scheduled_at":"`+lampau+`"}`))
	if kembali.Status != "scheduled" || kembali.ClaimedBy != nil || kembali.FailureReason != nil {
		t.Fatalf("penjadwalan ulang harus membersihkan jejak percobaan sebelumnya: %+v", kembali)
	}

	antrean := baca[[]konten](t, panggil(t, srv, "GET", "/v1/contents?scope=due", key, nil, ""))
	if len(antrean) != 1 {
		t.Fatalf("yang dijadwalkan ulang harus kembali ke antrean: %+v", antrean)
	}
}

func TestArsipKeluarDariAntrean(t *testing.T) {
	srv := siapkan(t)
	key := daftarkanApp(t, srv, "poster-1")

	lampau := time.Now().Add(-time.Hour).Format(time.RFC3339)
	c := baca[konten](t, unggah(t, srv, key, lampau, "isi-berkas"))

	if res := kirimJSON(t, srv, "/v1/contents/"+c.ID+"/archival", key, `{}`); res.Code != 200 {
		t.Fatalf("arsip gagal: %+v", res)
	}

	antrean := baca[[]konten](t, panggil(t, srv, "GET", "/v1/contents?scope=due", key, nil, ""))
	if len(antrean) != 0 {
		t.Fatalf("yang diarsipkan masih di antrean: %+v", antrean)
	}

	// Tapi masih terbaca. Servis ini tidak pernah menghapus apa pun.
	if res := panggil(t, srv, "GET", "/v1/contents/"+c.ID, key, nil, ""); res.Code != 200 {
		t.Fatalf("yang diarsipkan harus tetap terbaca: %+v", res)
	}
}

func TestJadwalWajibMenyebutZonaWaktu(t *testing.T) {
	srv := siapkan(t)
	key := daftarkanApp(t, srv, "poster-1")

	res := unggah(t, srv, key, "2026-09-12 09:00:00", "isi-berkas")
	if res.Status != "invalid_input" {
		t.Fatalf("jadwal tanpa zona waktu harus ditolak, dapat %+v", res)
	}
}

// TestBerkasTidakTertinggalSaatUnggahanDitolak menjaga janji yang paling mudah
// dilanggar: berkas ditulis lebih dulu, barisnya belakangan — jadi unggahan yang
// ditolak sesudah berkasnya jadi harus ikut membersihkan berkasnya.
//
// Berkas yatim tidak muncul di mana pun. Ia cuma memakan disk sampai ada yang
// menyadarinya bertahun-tahun kemudian.
func TestBerkasTidakTertinggalSaatUnggahanDitolak(t *testing.T) {
	srv, cfg := siapkanLengkap(t)
	key := daftarkanApp(t, srv, "poster-1")

	lampau := time.Now().Add(-time.Hour).Format(time.RFC3339)

	// Berkas kosong baru ketahuan sesudah seluruh isinya mengalir — jadi
	// berkasnya sudah terlanjur ditulis saat unggahannya ditolak.
	res := unggah(t, srv, key, lampau, "")
	if res.Status != "invalid_input" {
		t.Fatalf("berkas kosong harus ditolak, dapat %+v", res)
	}

	sisa, err := filepath.Glob(filepath.Join(cfg.FileDir(), "*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(sisa) != 0 {
		t.Fatalf("berkas tertinggal setelah unggahan ditolak: %v", sisa)
	}

	daftar := baca[[]konten](t, panggil(t, srv, "GET", "/v1/contents?scope=all", key, nil, ""))
	if len(daftar) != 0 {
		t.Fatalf("unggahan yang ditolak tidak boleh meninggalkan baris: %+v", daftar)
	}
}
