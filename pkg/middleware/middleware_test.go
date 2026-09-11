package middleware_test

import (
	"net/http"
	"net/netip"
	"testing"

	"github.com/gemgum/stock-engine/pkg/middleware"
)

func jaringan(t *testing.T, daftar ...string) []netip.Prefix {
	t.Helper()

	var out []netip.Prefix
	for _, item := range daftar {
		p, err := netip.ParsePrefix(item)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, p)
	}
	return out
}

func permintaan(remote string, header map[string]string) *http.Request {
	r, _ := http.NewRequest("GET", "/", nil)
	r.RemoteAddr = remote
	for k, v := range header {
		r.Header.Set(k, v)
	}
	return r
}

// TestClientIPSeesThroughTrustedProxy adalah alasan seluruh fungsi ini ada.
//
// Tanpa itu, servis di balik nginx melihat 127.0.0.1 untuk semua permintaan, dan
// seluruh integrator berbagi satu jatah pembatas laju.
func TestClientIPSeesThroughTrustedProxy(t *testing.T) {
	loopback := jaringan(t, "127.0.0.0/8", "::1/128")

	uji := []struct {
		nama   string
		remote string
		header map[string]string
		mau    string
	}{
		{
			nama:   "nginx meneruskan alamat asli",
			remote: "127.0.0.1:52341",
			header: map[string]string{"X-Real-IP": "203.0.113.77"},
			mau:    "203.0.113.77",
		},
		{
			nama:   "proxy yang cuma memasang X-Forwarded-For",
			remote: "127.0.0.1:52341",
			header: map[string]string{"X-Forwarded-For": "203.0.113.77"},
			mau:    "203.0.113.77",
		},
		{
			nama:   "rantai proxy: ambil yang paling kanan dan bukan milik kita",
			remote: "127.0.0.1:52341",
			header: map[string]string{"X-Forwarded-For": "203.0.113.77, 198.51.100.9, 127.0.0.1"},
			mau:    "198.51.100.9",
		},
		{
			// Inilah yang membuat header itu boleh dipercaya sama sekali: kalau
			// permintaannya datang langsung dari internet, headernya karangan
			// pemanggil sendiri — dan mempercayainya berarti jatah tak terbatas
			// untuk siapa pun yang tahu menuliskannya.
			nama:   "header dari pemanggil langsung diabaikan",
			remote: "198.51.100.9:41000",
			header: map[string]string{"X-Real-IP": "127.0.0.1", "X-Forwarded-For": "1.2.3.4"},
			mau:    "198.51.100.9",
		},
		{
			nama:   "header karangan yang bukan alamat diabaikan",
			remote: "127.0.0.1:52341",
			header: map[string]string{"X-Real-IP": "bukan-alamat"},
			mau:    "127.0.0.1",
		},
		{
			nama:   "tanpa header apa pun",
			remote: "127.0.0.1:52341",
			mau:    "127.0.0.1",
		},
	}

	for _, u := range uji {
		if dapat := middleware.ClientIP(permintaan(u.remote, u.header), loopback); dapat != u.mau {
			t.Errorf("%s: dapat %q, mau %q", u.nama, dapat, u.mau)
		}
	}
}

// TestClientIPWithoutTrustedProxies memastikan bawaan yang paling aman: kalau
// tidak ada proxy yang dipercaya, tidak ada header yang dipercaya.
func TestClientIPWithoutTrustedProxies(t *testing.T) {
	r := permintaan("127.0.0.1:52341", map[string]string{"X-Real-IP": "203.0.113.77"})

	if dapat := middleware.ClientIP(r, nil); dapat != "127.0.0.1" {
		t.Fatalf("tanpa daftar proxy, header tidak boleh dipercaya; dapat %q", dapat)
	}
}
