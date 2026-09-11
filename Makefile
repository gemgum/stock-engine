# Satu pintu untuk seluruh servis.
#
# Alasannya bukan kerapian: perintah yang cuma ada di kepala seseorang akan
# hilang bersama orang itu, dan yang berikutnya menebak-nebak.

.PHONY: help
help:                     ## Tampilkan daftar perintah ini
	@grep -E '^[a-z-]+:.*##' $(MAKEFILE_LIST) | sed 's/:.*##/\t/' | column -t -s "$$(printf '\t')"

.PHONY: dev
dev:                      ## Jalankan servis. Pengaturannya dibaca dari .env
	go run ./cmd/api

.PHONY: test
test:                     ## Uji seluruh servis di atas SQLite sekali pakai
	go test ./...

.PHONY: lint
lint:                     ## Periksa gaya dan kesalahan yang lolos dari kompilator
	gofmt -l .
	go vet ./...
	@command -v golangci-lint >/dev/null && golangci-lint run || \
		echo "golangci-lint belum terpasang — lewati. Pasang: https://golangci-lint.run/welcome/install/"

.PHONY: build
build:                    ## Rakit binary untuk dikirim ke VPS
	CGO_ENABLED=0 go build -o bin/stock-engine ./cmd/api

.PHONY: key
key:                      ## Terbitkan nilai acak untuk ADMIN_KEY
	@head -c 32 /dev/urandom | od -An -tx1 | tr -d ' \n'; echo

.PHONY: backup
backup:                   ## Salin seluruh isi storage/ ke folder cadangan
	bash ops/backup.sh
