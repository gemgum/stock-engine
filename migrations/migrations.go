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

// All mengembalikan seluruh migrasi, berurutan menurut nama berkasnya.
//
// ponytail: belum ada tabel versi. Seluruh isinya IF NOT EXISTS, jadi aman
// diputar ulang berapa kali pun. Tambahkan tabel versi begitu ada migrasi yang
// tidak idempoten — ALTER TABLE yang pertama akan jadi tandanya.
func All() (string, error) {
	entries, err := files.ReadDir(".")
	if err != nil {
		return "", err
	}

	var names []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	var b strings.Builder
	for _, name := range names {
		content, err := files.ReadFile(name)
		if err != nil {
			return "", err
		}
		b.Write(content)
		b.WriteString(";\n")
	}
	return b.String(), nil
}
