// Package config membaca pengaturan servis.
//
// Dua sumber, dan urutannya tetap: **environment menang, berkas `.env` cuma
// mengisi yang kosong.** Di laptop hampir semuanya datang dari `.env`; di
// server tidak ada berkas itu sama sekali. Yang di tengah — berkas `.env` yang
// tertinggal ikut ke server — tidak bisa diam-diam mengembalikan pengaturan
// lama.
//
// Kalau ada yang kurang, servis berhenti saat start dengan menyebut namanya.
package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// EnvFile dibaca kalau ada, dilewati kalau tidak.
const EnvFile = ".env"

type Config struct {
	// Satu folder untuk seluruh keadaan servis: berkas database dan berkas
	// konten.
	//
	// Satu folder, bukan dua, karena itu yang membuat pencadangan mungkin
	// benar. Database yang dicadangkan terpisah dari berkasnya akan cepat atau
	// lambat tidak cocok — dan ketidakcocokan itu baru ketahuan saat restore.
	StorageDir string

	// Kunci induk. Satu-satunya yang boleh mendaftarkan dan mencabut aplikasi
	// integrator.
	//
	// Ada di environment, bukan di database, supaya selalu ada jalan masuk saat
	// belum ada satu pun aplikasi terdaftar.
	AdminKey string

	// Batas ukuran satu berkas yang diunggah.
	//
	// Bawaannya 200MB karena video ikut. Angkanya di environment supaya bisa
	// diturunkan saat disk menipis tanpa kompilasi ulang.
	MaxUploadMB int

	// Alamat panel web yang boleh memanggil servis ini.
	AllowedOrigins []string

	Address string
}

// DatabasePath dan FileDir diturunkan dari StorageDir, bukan diatur sendiri.
//
// Dua pengaturan terpisah berarti keduanya bisa menunjuk tempat berbeda, dan
// sejak itu pencadangan satu folder tidak lagi menangkap semuanya.
func (c Config) DatabasePath() string { return filepath.Join(c.StorageDir, "stock.db") }
func (c Config) FileDir() string      { return filepath.Join(c.StorageDir, "files") }

// MaxUploadBytes dipakai pembatas badan request.
func (c Config) MaxUploadBytes() int64 { return int64(c.MaxUploadMB) << 20 }

func Load() (Config, error) {
	loadEnvFile(EnvFile)

	cfg := Config{
		StorageDir:     valueOr("STORAGE_DIR", "storage"),
		AdminKey:       os.Getenv("ADMIN_KEY"),
		MaxUploadMB:    whole("MAX_UPLOAD_MB", 200),
		AllowedOrigins: split(valueOr("ALLOWED_ORIGINS", "http://localhost:3000")),
		Address:        valueOr("ADDRESS", ":8080"),
	}

	// Kunci pendek bisa ditebak, dan kunci induk yang tertebak berarti seluruh
	// gudang konten terbuka. Batasnya ditegakkan di sini, bukan diserahkan pada
	// ingatan orang yang mengisi .env.
	if len(cfg.AdminKey) < 32 {
		return Config{}, fmt.Errorf("environment belum lengkap: ADMIN_KEY (minimal 32 karakter)")
	}

	return cfg, nil
}

// loadEnvFile mengisi environment dari berkas berbentuk KUNCI=nilai.
//
// Yang sudah ada di environment tidak ditimpa — lihat alasannya di atas.
func loadEnvFile(path string) {
	file, err := os.Open(path)
	if err != nil {
		return
	}
	defer func() { _ = file.Close() }()

	lines := bufio.NewScanner(file)
	for lines.Scan() {
		line := strings.TrimSpace(lines.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		key, value, found := strings.Cut(strings.TrimPrefix(line, "export "), "=")
		if !found {
			continue
		}

		key = strings.TrimSpace(key)
		value = strings.Trim(strings.TrimSpace(value), `"'`)

		if key != "" && os.Getenv(key) == "" {
			_ = os.Setenv(key, value)
		}
	}
}

// whole membaca bilangan bulat positif dari environment.
//
// Yang bukan angka, nol, atau minus dianggap tidak diisi. Batas unggah nol
// megabyte berarti tidak ada satu pun konten yang bisa masuk, dan salah ketik
// tidak boleh berakibat sejauh itu.
func whole(key string, fallback int) int {
	n, err := strconv.Atoi(os.Getenv(key))
	if err != nil || n <= 0 {
		return fallback
	}
	return n
}

func valueOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func split(raw string) []string {
	var list []string
	for _, item := range strings.Split(raw, ",") {
		if trimmed := strings.TrimSpace(item); trimmed != "" {
			list = append(list, trimmed)
		}
	}
	return list
}

// ShutdownGrace adalah waktu yang diberikan pada permintaan yang sedang jalan
// saat servis diminta berhenti.
//
// Tiga puluh detik karena unggahan 200MB di jaringan Indonesia memang selama
// itu, dan memutusnya di tengah berarti berkasnya harus dikirim ulang dari nol.
const ShutdownGrace = 30 * time.Second
