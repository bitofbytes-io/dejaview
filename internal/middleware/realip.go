package middleware

import (
	"context"
	"net"
	"net/http"
	"net/netip"
	"strings"
)

type verifiedClientIPKey struct{}

// RealIP sets r.RemoteAddr to the client IP and records whether that IP is
// known to be the real client (see VerifiedClientIP). With trusted proxies
// configured, a peer outside them is a direct client, and a trusted peer's
// client is the rightmost X-Forwarded-For address that is not itself a
// trusted proxy. With none configured, forwarded headers are ignored and
// the IP is never verified, since the app cannot tell whether it sits
// behind a proxy.
func RealIP(trusted []netip.Prefix) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if ip, verified, ok := clientIP(r, trusted); ok {
				r.RemoteAddr = ip.String()
				if verified {
					r = r.WithContext(context.WithValue(r.Context(), verifiedClientIPKey{}, true))
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// ClientIP returns the client IP from r.RemoteAddr, with or without a port.
func ClientIP(r *http.Request) string {
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

// VerifiedClientIP returns the client IP and true only when RealIP resolved
// it from a trusted configuration rather than guessing.
func VerifiedClientIP(r *http.Request) (string, bool) {
	verified, _ := r.Context().Value(verifiedClientIPKey{}).(bool)
	return ClientIP(r), verified
}

func clientIP(r *http.Request, trusted []netip.Prefix) (ip netip.Addr, verified, ok bool) {
	peer, err := netip.ParseAddr(ClientIP(r))
	if err != nil {
		return netip.Addr{}, false, false
	}
	peer = peer.Unmap()
	if len(trusted) == 0 {
		return peer, false, true
	}
	if !isTrusted(peer, trusted) {
		return peer, true, true
	}
	var hops []string
	for _, header := range r.Header.Values("X-Forwarded-For") {
		hops = append(hops, strings.Split(header, ",")...)
	}
	for i := len(hops) - 1; i >= 0; i-- {
		addr, err := netip.ParseAddr(strings.TrimSpace(hops[i]))
		if err != nil {
			break
		}
		addr = addr.Unmap()
		if !isTrusted(addr, trusted) {
			return addr, true, true
		}
	}
	// A trusted proxy sent no usable client address.
	return peer, false, true
}

func isTrusted(addr netip.Addr, trusted []netip.Prefix) bool {
	for _, prefix := range trusted {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}
