package redis

import (
	"bufio"
	"context"
	"net"
	"strconv"
	"strings"
	"testing"
)

// fakeRedis menjawab satu koneksi: tiap perintah dicatat, lalu dijawab
// dengan `reply` sesuai nama perintahnya.
func fakeRedis(t *testing.T, reply map[string]string) (string, chan []string) {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	got := make(chan []string, 4)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()

		r := bufio.NewReader(conn)
		for {
			args, err := readCommand(r)
			if err != nil {
				return
			}
			got <- args
			_, _ = conn.Write([]byte(reply[args[0]]))
		}
	}()
	return ln.Addr().String(), got
}

func readCommand(r *bufio.Reader) ([]string, error) {
	line, err := r.ReadString('\n')
	if err != nil {
		return nil, err
	}
	n, _ := strconv.Atoi(strings.TrimSpace(line[1:]))

	args := make([]string, 0, n)
	for range n {
		if _, err := r.ReadString('\n'); err != nil { // $panjang
			return nil, err
		}
		arg, err := r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		args = append(args, strings.TrimSuffix(arg, "\r\n"))
	}
	return args, nil
}

// TestAddSendsAuthThenXadd memastikan bentuk perintah yang sampai ke Redis —
// itu satu-satunya yang ditulis paket ini sendiri.
func TestAddSendsAuthThenXadd(t *testing.T) {
	addr, got := fakeRedis(t, map[string]string{"AUTH": "+OK\r\n", "XADD": "$3\r\n1-0\r\n"})

	err := NewStream(addr, "rahasia", "content.ready").Add(context.Background(), "id", "abc")
	if err != nil {
		t.Fatal(err)
	}

	if auth := <-got; strings.Join(auth, " ") != "AUTH rahasia" {
		t.Fatalf("perintah pertama harus AUTH, dapat %v", auth)
	}
	want := "XADD content.ready MAXLEN ~ 10000 * id abc"
	if xadd := <-got; strings.Join(xadd, " ") != want {
		t.Fatalf("XADD salah bentuk:\n dapat %v\n mau   %s", xadd, want)
	}
}

// TestAddReportsRedisError: penolakan Redis harus jadi galat, bukan diam —
// kalau diam, kontennya ditandai sudah dikabarkan padahal tidak.
func TestAddReportsRedisError(t *testing.T) {
	addr, _ := fakeRedis(t, map[string]string{"XADD": "-WRONGTYPE Operation against a key\r\n"})

	err := NewStream(addr, "", "content.ready").Add(context.Background(), "id", "abc")
	if err == nil || !strings.Contains(err.Error(), "WRONGTYPE") {
		t.Fatalf("penolakan Redis harus jadi galat, dapat %v", err)
	}
}
