// Package security menerbitkan dan mencocokkan kunci API.
//
// Tidak tahu apa-apa tentang konten maupun aplikasi integrator — cuma tahu
// bahwa ada rahasia yang diterbitkan sekali dan dicocokkan berkali-kali.
package security

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
)

// Prefix menandai kunci milik servis ini.
//
// Gunanya bukan hiasan: kunci yang tidak sengaja tertulis di kode atau di isu
// bisa dikenali pemindai rahasia, dan pemiliknya tahu yang mana yang harus
// dicabut.
const Prefix = "sk_"

// NewKey menerbitkan kunci baru beserta hashnya.
//
// Kuncinya dikembalikan **satu kali saja** — yang disimpan cuma hashnya. Kalau
// hilang, tidak ada cara memulihkannya, dan itu memang tujuannya: database yang
// bocor tidak berisi satu pun kunci yang bisa dipakai.
func NewKey() (key, hash string, err error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", "", err
	}

	key = Prefix + hex.EncodeToString(raw)
	return key, Hash(key), nil
}

// Hash mengubah kunci jadi bentuk yang disimpan.
//
// SHA-256 telanjang, bukan bcrypt — dan itu disengaja. Kunci ini 256 bit acak,
// bukan kata sandi buatan manusia, jadi tidak ada kamus yang bisa menebaknya
// dan hash lambat cuma memperlambat setiap permintaan yang sah.
func Hash(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

// Equal membandingkan dua nilai dalam waktu tetap.
//
// Perbandingan biasa berhenti di huruf pertama yang berbeda, dan selisih waktu
// itu cukup untuk menebak rahasia satu huruf per satu huruf.
func Equal(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
