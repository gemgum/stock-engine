package services

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"

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

func NewFiles(dir string) (Files, error) {
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
	tmp, err := os.CreateTemp(f.dir, "upload-*")
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
