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
	"net/netip"
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

	// Berapa lama berkas konten yang sudah selesai disimpan sebelum dibuang.
	//
	// "Selesai" berarti sudah tayang atau sudah dibatalkan. Stok yang masih
	// menunggu gilirannya tidak pernah disentuh, berapa lama pun sudah diunggah.
	//
	// Yang dibuang cuma berkasnya — baris, jadwal, dan riwayatnya tetap.
	// Bawaannya 90 hari. Diisi 0 berarti tidak pernah dibuang, dan itu pilihan
	// yang sah asal disknya diawasi manusia.
	RetentionDays int

	// Berapa lama konten boleh dipegang satu aplikasi sebelum claimnya dilepas.
	//
	// Integrator yang mati sesudah mengclaim membuat kontennya diam di
	// `claimed` selamanya — tidak pernah tayang, dan tidak pernah muncul lagi di
	// antrean kerja. Ambang ini yang mengembalikannya.
	//
	// Bawaannya 2 jam, dan angkanya bukan kehati-hatian berlebihan: ambang yang
	// terlalu pendek merebut konten dari integrator yang **masih** mengunggah
	// video besar di jaringan pelan, dan hasilnya konten tayang dua kali.
	// Diisi 0 berarti tidak pernah dilepas otomatis.
	ClaimTimeoutHours int

	// Alamat panel web yang boleh memanggil servis ini.
	AllowedOrigins []string

	// Jaringan tempat proxy di depan servis ini berada.
	//
	// Cuma dari sinilah header alamat asli (`X-Real-IP`, `X-Forwarded-For`)
	// dipercaya. Bawaannya loopback saja, karena nginx berjalan di mesin yang
	// sama — dan itu juga bawaan yang aman kalau servisnya ternyata terbuka
	// langsung ke internet: header dari luar tidak akan pernah dipercaya.
	TrustedProxies []netip.Prefix

	// Redis Stream tempat konten yang sudah waktunya dikabarkan.
	//
	// Kosong berarti tidak dipakai, dan servis tetap utuh tanpanya: integrator
	// yang tidak membaca stream cukup memanggil `?scope=due`. Redis yang mati
	// tidak menghentikan servis — kontennya dikabarkan begitu Redis kembali.
	RedisAddr     string
	RedisPassword string
	RedisStream   string

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
		StorageDir:        valueOr("STORAGE_DIR", "storage"),
		AdminKey:          os.Getenv("ADMIN_KEY"),
		MaxUploadMB:       whole("MAX_UPLOAD_MB", 200),
		RetentionDays:     retention("RETENTION_DAYS", 90),
		ClaimTimeoutHours: retention("CLAIM_TIMEOUT_HOURS", 2),
		AllowedOrigins:    split(valueOr("ALLOWED_ORIGINS", "http://localhost:3000")),
		TrustedProxies:    prefixes(valueOr("TRUSTED_PROXIES", "127.0.0.0/8,::1/128")),
		RedisAddr:         os.Getenv("REDIS_ADDR"),
		RedisPassword:     os.Getenv("REDIS_PASSWORD"),
		RedisStream:       valueOr("REDIS_STREAM", "content.ready"),
		Address:           valueOr("ADDRESS", ":8080"),
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

// Retention adalah RetentionDays dalam bentuk durasi. Nol berarti tidak pernah
// membuang berkas.
func (c Config) Retention() time.Duration {
	return time.Duration(c.RetentionDays) * 24 * time.Hour
}

// ClaimTimeout adalah ClaimTimeoutHours dalam bentuk durasi. Nol berarti claim
// tidak pernah dilepas otomatis.
func (c Config) ClaimTimeout() time.Duration {
	return time.Duration(c.ClaimTimeoutHours) * time.Hour
}

// retention membaca ambang yang nolnya berarti "jangan pernah".
//
// Berbeda dari whole(): **nol diterima apa adanya**, karena "jangan pernah"
// adalah pilihan yang sah. Yang ditolak cuma nilai minus dan yang bukan
// angka — keduanya lebih mungkin salah ketik daripada niat, dan salah ketik
// tidak boleh diam-diam mematikan sapuan.
func retention(key string, fallback int) int {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback
	}

	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
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

// prefixes membaca daftar jaringan yang dipercaya.
//
// Menerima CIDR (`10.0.0.0/8`) maupun alamat tunggal (`10.0.0.5`). Yang tidak
// terbaca **dibuang diam-diam**, dan itu disengaja: jaringan yang salah ketik
// lebih baik tidak dipercaya daripada dipercaya setengah-setengah.
func prefixes(raw string) []netip.Prefix {
	var list []netip.Prefix

	for _, item := range split(raw) {
		if jaringan, err := netip.ParsePrefix(item); err == nil {
			list = append(list, jaringan)
			continue
		}
		if alamat, err := netip.ParseAddr(item); err == nil {
			list = append(list, netip.PrefixFrom(alamat, alamat.BitLen()))
		}
	}
	return list
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
