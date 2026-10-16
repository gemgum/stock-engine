package services

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gemgum/stock-engine/internal/models"
)

// Files menyimpan berkas konten di disk.
//
// Namanya di disk cuma id kontennya, tanpa ekstensi. Nama asli dan tipe
// berkasnya tinggal di database, dan itu disengaja: nama kiriman datang dari
// luar, dan nama dari luar yang dipakai sebagai nama berkas adalah cara paling
// umum membuat lubang path traversal.
type Files struct {
	dir string
}

// NewFiles menyimpan foldernya sebagai path mutlak, karena Path ikut dikirim
// ke Redis Stream — dan path relatif tidak berarti apa-apa bagi proses lain.
func NewFiles(dir string) (Files, error) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return Files{}, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Files{}, err
	}
	return Files{dir: dir}, nil
}

// Save menulis berkas sambil menghitung ukuran dan checksum-nya sekali jalan.
//
// Sekali jalan, bukan tulis-lalu-baca-ulang: berkas 200MB yang dibaca dua kali
// berarti dua kali kerja disk untuk angka yang sama.
//
// Tulisannya lewat berkas sementara lalu dipindahkan. Rename di dalam satu
// filesystem bersifat atomik, jadi berkas yang terlihat di folder tujuan selalu
// berkas yang sudah utuh — unggahan yang putus di tengah tidak meninggalkan
// berkas separuh yang terlihat sah.
func (f Files) Save(id string, src io.Reader) (size int64, checksum string, err error) {
	tmp, err := os.CreateTemp(f.dir, tempPrefix+"*")
	if err != nil {
		return 0, "", err
	}
	tmpName := tmp.Name()

	defer func() {
		if err != nil {
			_ = os.Remove(tmpName)
		}
	}()

	hash := sha256.New()
	size, err = io.Copy(io.MultiWriter(tmp, hash), src)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return 0, "", err
	}

	// CreateTemp membuatnya 0600. Grup ikut boleh membaca, supaya integrator di
	// mesin yang sama bisa membuka berkasnya lewat `path` di Redis Stream —
	// asal user-nya masuk grup pemilik folder ini. Yang lain tetap tidak.
	if err = os.Chmod(tmpName, 0o640); err != nil {
		return 0, "", err
	}

	if err = os.Rename(tmpName, f.Path(id)); err != nil {
		return 0, "", err
	}
	return size, hex.EncodeToString(hash.Sum(nil)), nil
}

// Path menyebut letak berkasnya. Dipakai handler untuk menyajikannya.
//
// id selalu datang dari database, tidak pernah langsung dari alamat yang
// diketik pemanggil — itu yang membuat filepath.Join di sini aman.
func (f Files) Path(id string) string { return filepath.Join(f.dir, id) }

// tempPrefix menandai berkas yang belum selesai ditulis.
//
// Dipakai dua arah: os.CreateTemp membuatnya, dan RemoveStale mengenalinya
// kembali. Kalau keduanya memakai awalan yang berbeda, sapuan tidak akan
// menemukan apa-apa dan tidak ada yang menyadarinya — berkasnya cuma menumpuk.
const tempPrefix = "upload-"

// RemoveStale membuang berkas sementara yang ditinggalkan.
//
// Servis yang mati **mendadak** di tengah penulisan — OOM killer, crash, listrik
// padam, atau restart yang melewati tenggang — meninggalkan berkas separuh yang
// tidak punya baris di database. Tidak ada yang tahu id-nya, jadi sapuan retensi
// tidak akan pernah melihatnya, dan ia memakan disk selamanya.
//
// Berhenti normal (SIGTERM) tidak menghasilkan ini: unggahan yang berjalan
// diberi waktu selesai, dan yang gagal membersihkan dirinya sendiri.
//
// olderThan harus jauh lebih lama daripada unggahan terlama yang masuk akal.
// Berkas yang masih ditulis saat ini pun aman kalau terlanjur terhapus di Linux
// — penulisnya memegang fd, bukan namanya — tapi pemindahannya nanti gagal, dan
// unggahan yang sah tidak boleh dikorbankan demi kebersihan.
func (f Files) RemoveStale(olderThan time.Duration) (int, error) {
	entries, err := os.ReadDir(f.dir)
	if err != nil {
		return 0, err
	}

	batas := time.Now().Add(-olderThan)
	dibuang := 0

	for _, e := range entries {
		if e.IsDir() || !strings.HasPrefix(e.Name(), tempPrefix) {
			continue
		}

		info, err := e.Info()
		if err != nil || info.ModTime().After(batas) {
			continue
		}
		if err := os.Remove(filepath.Join(f.dir, e.Name())); err == nil {
			dibuang++
		}
	}
	return dibuang, nil
}

// Remove membuang berkas.
//
// Dipakai satu tempat saja: membatalkan unggahan yang barisnya gagal ditulis.
// Berkas tanpa baris tidak akan pernah ditemukan siapa pun, dan cuma memakan
// disk sampai ada yang menyadarinya bertahun-tahun kemudian.
func (f Files) Remove(id string) error {
	err := os.Remove(f.Path(id))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// Pastikan Files memenuhi janji yang ditulis di models.
var _ models.Files = Files{}
