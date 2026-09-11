package models

import (
	"strings"
	"time"
)

// TimeLayout adalah satu-satunya bentuk waktu di servis ini.
//
//	2026-09-13 09:00:00
//
// Tanpa zona waktu, dan itu keputusan: seluruh waktu — yang dikirim, yang
// tersimpan, dan yang dibalas — adalah **waktu server**. Yang mengetik jadwal
// tidak perlu memikirkan konversi, dan yang membuka `stock.db` dengan `sqlite3`
// melihat angka yang persis sama dengan yang dilihat integrator.
//
// Harganya harus diketahui: **zona waktu VPS jadi bagian dari data.** Mengubah
// zona server sesudah ada isinya akan menggeser arti seluruh jadwal yang sudah
// tersimpan. Setel sekali saat menyiapkan server, lalu jangan diubah.
//
// Bentuk ini juga yang membuat urutannya benar saat dibandingkan sebagai teks —
// yang berarti `scheduled_at <= ?` bekerja apa adanya di SQLite, tanpa fungsi
// konversi di tiap kueri.
const TimeLayout = "2006-01-02 15:04:05"

// Time adalah waktu yang tahu bentuk kirimannya sendiri.
//
// Tipe sendiri, bukan time.Time telanjang, karena `encoding/json` bawaan selalu
// menulis RFC3339. Membungkusnya di sini berarti bentuknya diputuskan **satu
// kali** dan berlaku di seluruh servis — tidak ada handler yang memformat
// waktunya sendiri lalu berbeda tipis dari yang lain.
type Time struct {
	time.Time
}

// Now menyebut waktu sekarang menurut server.
func Now() Time { return Time{time.Now()} }

// ParseTime membaca waktu dari kiriman, ditafsirkan sebagai waktu server.
func ParseTime(raw string) (Time, error) {
	t, err := time.ParseInLocation(TimeLayout, strings.TrimSpace(raw), time.Local)
	return Time{t}, err
}

// String dipakai saat waktunya disimpan ke database.
func (t Time) String() string { return t.In(time.Local).Format(TimeLayout) }

func (t Time) MarshalJSON() ([]byte, error) {
	return []byte(`"` + t.String() + `"`), nil
}

func (t *Time) UnmarshalJSON(raw []byte) error {
	teks := strings.Trim(string(raw), `"`)
	if teks == "" || teks == "null" {
		*t = Time{}
		return nil
	}

	parsed, err := ParseTime(teks)
	if err != nil {
		return err
	}
	*t = parsed
	return nil
}
