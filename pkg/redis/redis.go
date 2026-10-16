// Package redis menambahkan event ke satu Redis Stream.
//
// Cuma satu perintah yang dipakai — XADD — jadi yang ditulis di sini protokol
// RESP secukupnya, bukan pustaka klien lengkap. Tidak tahu apa-apa tentang
// konten: isi event-nya disusun pemanggil.
package redis

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"
)

// timeout membatasi satu perintah, dari menyambung sampai jawabannya terbaca.
// Redis-nya di mesin yang sama; yang lebih lama dari ini berarti ia tidak
// menjawab, bukan sedang sibuk.
const timeout = 5 * time.Second

// maxLen membatasi panjang stream. Tanpa batas, event yang sudah dibaca tetap
// tinggal di memori Redis selamanya. "~" membiarkan Redis memangkas kira-kira,
// yang jauh lebih murah daripada tepat.
const maxLen = "10000"

type Stream struct {
	addr     string
	password string
	name     string
}

func NewStream(addr, password, name string) *Stream {
	return &Stream{addr: addr, password: password, name: name}
}

// Add menambahkan satu event berisi pasangan field-nilai.
//
// ponytail: satu koneksi per perintah. Cukup untuk puluhan event per menit di
// localhost; ganti ke koneksi tetap kalau jumlahnya ratusan.
func (s *Stream) Add(ctx context.Context, fields ...string) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, "tcp", s.addr)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()

	deadline, _ := ctx.Deadline()
	_ = conn.SetDeadline(deadline)

	reader := bufio.NewReader(conn)
	if s.password != "" {
		if err := command(conn, reader, "AUTH", s.password); err != nil {
			return err
		}
	}

	args := append([]string{"XADD", s.name, "MAXLEN", "~", maxLen, "*"}, fields...)
	return command(conn, reader, args...)
}

// command mengirim satu perintah lalu membaca baris pertama jawabannya.
//
// Yang diperiksa cuma galat atau bukan. Isi jawabannya — "+OK" untuk AUTH, id
// event untuk XADD — tidak dipakai siapa pun.
func command(conn net.Conn, reader *bufio.Reader, args ...string) error {
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "*%d\r\n", len(args))
	for _, a := range args {
		fmt.Fprintf(&buf, "$%d\r\n%s\r\n", len(a), a)
	}
	if _, err := conn.Write(buf.Bytes()); err != nil {
		return err
	}

	line, err := reader.ReadString('\n')
	if err != nil {
		return err
	}

	switch line[0] {
	case '+', '$', ':':
		return nil
	case '-':
		return errors.New("redis menolak: " + strings.TrimSpace(line[1:]))
	default:
		return fmt.Errorf("jawaban redis tidak dikenal: %q", line)
	}
}
