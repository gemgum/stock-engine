package config_test

import (
	"os"
	"testing"

	"github.com/gemgum/stock-engine/internal/config"
)

// siapkanEnv mengosongkan environment supaya satu uji tidak terpengaruh
// pengaturan uji sebelumnya — atau pengaturan mesin yang menjalankannya.
func siapkanEnv(t *testing.T, isi map[string]string) {
	t.Helper()

	for _, key := range []string{"ADMIN_KEY", "STORAGE_DIR", "MAX_UPLOAD_MB",
		"RETENTION_DAYS", "CLAIM_TIMEOUT_HOURS", "ALLOWED_ORIGINS",
		"TRUSTED_PROXIES", "ADDRESS"} {
		t.Setenv(key, "")
		_ = os.Unsetenv(key)
	}
	for k, v := range isi {
		t.Setenv(k, v)
	}

	// Load() membaca berkas .env kalau ada; uji harus berjalan di folder yang
	// tidak punya satu pun.
	t.Chdir(t.TempDir())
}

const kunci = "kunci-induk-untuk-pengujian-yang-panjangnya-cukup"

// TestBawaanTerisi menjaga seluruh pengaturan punya nilai tanpa environment.
//
// Ada karena pernah kejadian: `TrustedProxies` sempat ditulis di struct dan
// dipakai di router, tapi **tidak pernah diisi** di Load(). Ujinya lulus, karena
// yang diuji fungsinya, bukan penyambungannya — dan pembatas lajunya tetap
// buta di balik nginx.
func TestBawaanTerisi(t *testing.T) {
	siapkanEnv(t, map[string]string{"ADMIN_KEY": kunci})

	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}

	if len(cfg.TrustedProxies) == 0 {
		t.Fatal("TrustedProxies kosong: header alamat asli tidak akan pernah dipercaya")
	}
	if cfg.RetentionDays != 90 || cfg.ClaimTimeoutHours != 2 || cfg.MaxUploadMB != 200 {
		t.Fatalf("bawaan tidak sesuai: %+v", cfg)
	}
	if cfg.StorageDir == "" || cfg.Address == "" {
		t.Fatalf("pengaturan wajib kosong: %+v", cfg)
	}
}

func TestKunciPendekDitolak(t *testing.T) {
	siapkanEnv(t, map[string]string{"ADMIN_KEY": "pendek"})

	if _, err := config.Load(); err == nil {
		t.Fatal("kunci induk pendek harus ditolak")
	}
}

// TestNolBerartiJanganPernah menjaga pilihan mematikan sapuan tetap berarti apa
// adanya — berbeda dari salah ketik, yang justru harus kembali ke bawaan.
func TestNolBerartiJanganPernah(t *testing.T) {
	siapkanEnv(t, map[string]string{
		"ADMIN_KEY":           kunci,
		"RETENTION_DAYS":      "0",
		"CLAIM_TIMEOUT_HOURS": "bukan-angka",
	})

	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.RetentionDays != 0 {
		t.Fatalf("nol harus diterima apa adanya, dapat %d", cfg.RetentionDays)
	}
	if cfg.ClaimTimeoutHours != 2 {
		t.Fatalf("nilai yang bukan angka harus kembali ke bawaan, dapat %d", cfg.ClaimTimeoutHours)
	}
}

func TestProxyDipercayaBisaDiatur(t *testing.T) {
	siapkanEnv(t, map[string]string{
		"ADMIN_KEY":       kunci,
		"TRUSTED_PROXIES": "10.0.0.0/8, 192.168.1.5, ngawur",
	})

	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.TrustedProxies) != 2 {
		t.Fatalf("dua yang sah harus terbaca, yang ngawur dibuang: %v", cfg.TrustedProxies)
	}
	if cfg.TrustedProxies[1].Bits() != 32 {
		t.Fatalf("alamat tunggal harus jadi /32: %v", cfg.TrustedProxies[1])
	}
}
