// Package ratelimit membatasi berapa sering satu pemanggil boleh datang.
//
// Ember token per pemanggil, dihitung di memori satu proses. Itu cukup selama
// servisnya satu salinan — dan saat ia berjalan lebih dari satu salinan,
// pembatas ini dan pilihan SQLite jatuh tempo di hari yang sama.
package ratelimit

import (
	"math"
	"sync"
	"time"
)

type bucket struct {
	tokens float64
	seen   time.Time
}

// Limiter memberi tiap pemanggil embernya sendiri.
//
// burst adalah isi penuh ember: berapa permintaan boleh datang beruntun tanpa
// jeda. perMinute adalah kecepatan ember terisi lagi.
type Limiter struct {
	burst     float64
	perSecond float64

	mu      sync.Mutex
	buckets map[string]*bucket
}

func New(burst, perMinute int) *Limiter {
	return &Limiter{
		burst:     float64(burst),
		perSecond: float64(perMinute) / 60,
		buckets:   make(map[string]*bucket),
	}
}

// Allow mengambil satu token milik pemanggil. False berarti embernya kosong.
func (l *Limiter) Allow(caller string) bool {
	now := time.Now()

	l.mu.Lock()
	defer l.mu.Unlock()

	b, ada := l.buckets[caller]
	if !ada {
		l.buckets[caller] = &bucket{tokens: l.burst - 1, seen: now}
		return true
	}

	b.tokens = math.Min(l.burst, b.tokens+now.Sub(b.seen).Seconds()*l.perSecond)
	b.seen = now

	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// Sweep membuang ember yang sudah lama tidak datang.
//
// Tanpa ini map-nya tumbuh selamanya, dan pemindai porta meninggalkan ribuan
// baris yang tidak akan pernah dipakai lagi.
func (l *Limiter) Sweep(olderThan time.Duration) {
	batas := time.Now().Add(-olderThan)

	l.mu.Lock()
	defer l.mu.Unlock()

	for caller, b := range l.buckets {
		if b.seen.Before(batas) {
			delete(l.buckets, caller)
		}
	}
}
