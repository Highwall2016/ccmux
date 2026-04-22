package middleware

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

var (
	trustedProxies    []*net.IPNet
	trustedProxiesMu sync.RWMutex
)

// SetTrustedProxies configures the CIDRs that are allowed to set X-Forwarded-For.
func SetTrustedProxies(cidrs []string) error {
	var nets []*net.IPNet
	for _, cidr := range cidrs {
		_, n, err := net.ParseCIDR(cidr)
		if err != nil {
			return err
		}
		nets = append(nets, n)
	}
	trustedProxiesMu.Lock()
	trustedProxies = nets
	trustedProxiesMu.Unlock()
	return nil
}

func isTrusted(ip string) bool {
	parsedIP := net.ParseIP(ip)
	if parsedIP == nil {
		return false
	}
	trustedProxiesMu.RLock()
	defer trustedProxiesMu.RUnlock()
	for _, n := range trustedProxies {
		if n.Contains(parsedIP) {
			return true
		}
	}
	return false
}

type visitorEntry struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// RateLimiter returns a per-IP token bucket middleware.
// r is the sustained rate (requests/second); burst is the maximum burst size.
func RateLimiter(r rate.Limit, burst int) func(http.Handler) http.Handler {
	var mu sync.Mutex
	visitors := map[string]*visitorEntry{}

	// Evict idle entries every minute to prevent unbounded growth.
	go func() {
		for range time.Tick(time.Minute) {
			mu.Lock()
			for ip, v := range visitors {
				if time.Since(v.lastSeen) > 5*time.Minute {
					delete(visitors, ip)
				}
			}
			mu.Unlock()
		}
	}()

	get := func(ip string) *rate.Limiter {
		mu.Lock()
		defer mu.Unlock()
		v, ok := visitors[ip]
		if !ok {
			v = &visitorEntry{limiter: rate.NewLimiter(r, burst)}
			visitors[ip] = v
		}
		v.lastSeen = time.Now()
		return v.limiter
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip, _, err := net.SplitHostPort(r.RemoteAddr)
			if err != nil {
				ip = r.RemoteAddr
			}

			if isTrusted(ip) {
				ip = realClientIP(r, ip)
			}

			if !get(ip).Allow() {
				http.Error(w, "too many requests", http.StatusTooManyRequests)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// realClientIP returns the true client IP from a request that has already
// been confirmed to arrive from a trusted proxy.
//
// Detection order:
//
//  1. CF-Connecting-IP — set by Cloudflare Tunnel and Cloudflare proxy.
//     Cloudflare STRIPS any client-supplied version of this header, so it
//     cannot be forged end-to-end. Always reliable when behind Cloudflare.
//
//  2. X-Forwarded-For — rightmost (last) entry only.
//     Proxies APPEND their observation of the client address; the leftmost
//     entry is supplied by the original sender and can be freely forged.
//     Reading the last entry gives us the IP seen by the outermost trusted
//     proxy, which is the closest we can get to the real client address.
//
//  3. Fallback — the proxy's own TCP address (already known to be trusted).
func realClientIP(r *http.Request, fallback string) string {
	// 1. Cloudflare: prefer CF-Connecting-IP.
	if cf := strings.TrimSpace(r.Header.Get("CF-Connecting-IP")); cf != "" {
		return cf
	}

	// 2. Standard proxy: walk X-Forwarded-For from right to left.
	//    The rightmost untrusted IP is the real client.
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		parts := strings.Split(fwd, ",")
		for i := len(parts) - 1; i >= 0; i-- {
			ip := strings.TrimSpace(parts[i])
			if !isTrusted(ip) {
				return ip
			}
		}
		// If all hops are trusted, the leftmost IP is the original client
		if ip := strings.TrimSpace(parts[0]); ip != "" {
			return ip
		}
	}

	// 3. Fallback to the proxy connection address.
	return fallback
}
