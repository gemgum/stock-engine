# Dua tahap: yang merakit tidak ikut dikirim.
#
# CGO dimatikan karena driver SQLite-nya Go murni. Itu yang membuat binary ini
# bisa jalan di image kosong tanpa satu pun pustaka sistem.
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /bin/stock-engine ./cmd/api

FROM alpine:3.20
RUN adduser -D -u 10001 engine
# storage/ dibuat lebih dulu supaya pemiliknya benar sebelum volume dipasang.
RUN mkdir -p /data && chown engine /data
USER engine
ENV STORAGE_DIR=/data
COPY --from=build /bin/stock-engine /bin/stock-engine
EXPOSE 8080
ENTRYPOINT ["/bin/stock-engine"]
