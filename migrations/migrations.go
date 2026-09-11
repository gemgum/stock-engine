// Package migrations menempelkan berkas SQL ke dalam binary.
//
// Ikut masuk binary supaya servis yang baru dideploy tidak bisa jalan dengan
// skema yang tertinggal — tidak ada langkah terpisah yang bisa terlupa.
package migrations

import (
	"embed"
	"sort"
	"strings"
)

//go:embed *.sql
var files embed.FS

// Migration adalah satu berkas SQL beserta namanya.
//
// Namanya ikut dibawa karena itu yang dicatat sebagai "sudah pernah
// dijalankan". Sebelum ada catatan itu, seluruh migrasi diputar ulang tiap
// servis menyala — aman selama isinya `IF NOT EXISTS`, dan berhenti aman begitu
// ada `ALTER TABLE` yang pertama.
type Migration struct {
	Name string
	SQL  string
}

// All mengembalikan seluruh migrasi, berurutan menurut nama berkasnya.
//
// Urutannya datang dari nama berkas, jadi penomorannya bukan hiasan: `0002_`
// wajib jalan sesudah `0001_`. Berkas baru selalu diberi nomor lebih besar dari
// yang sudah pernah dijalankan — yang disisipkan di tengah tidak akan pernah
// ikut, karena yang sesudahnya sudah tercatat selesai.
func All() ([]Migration, error) {
	entries, err := files.ReadDir(".")
	if err != nil {
		return nil, err
	}

	var names []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	out := make([]Migration, 0, len(names))
	for _, name := range names {
		content, err := files.ReadFile(name)
		if err != nil {
			return nil, err
		}
		out = append(out, Migration{Name: name, SQL: string(content)})
	}
	return out, nil
}
