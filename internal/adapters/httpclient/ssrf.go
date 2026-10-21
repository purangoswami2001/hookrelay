package httpclient

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"syscall"
	"time"
)

// blockedCIDRs contains private, loopback, link-local, multicast, and cloud metadata subnets
var blockedCIDRs = []*net.IPNet{
	// IPv4 Special/Private ranges
	mustParseCIDR("0.0.0.0/8"),          // Current network
	mustParseCIDR("10.0.0.0/8"),         // Private network (RFC 1918)
	mustParseCIDR("100.64.0.0/10"),      // Shared Address Space / CGNAT
	mustParseCIDR("127.0.0.0/8"),        // Loopback
	mustParseCIDR("169.254.0.0/16"),     // Link-Local (AWS/GCP/Azure IMDS)
	mustParseCIDR("172.16.0.0/12"),      // Private network (RFC 1918)
	mustParseCIDR("192.0.0.0/24"),       // IETF Protocol Assignments
	mustParseCIDR("192.0.2.0/24"),       // Documentation (TEST-NET-1)
	mustParseCIDR("192.168.0.0/16"),     // Private network (RFC 1918)
	mustParseCIDR("198.18.0.0/15"),      // Benchmark testing
	mustParseCIDR("198.51.100.0/24"),    // Documentation (TEST-NET-2)
	mustParseCIDR("203.0.113.0/24"),     // Documentation (TEST-NET-3)
	mustParseCIDR("224.0.0.0/4"),        // Multicast
	mustParseCIDR("240.0.0.0/4"),        // Reserved for future use
	mustParseCIDR("255.255.255.255/32"), // Broadcast

	// IPv6 Special/Private ranges
	mustParseCIDR("::/128"),        // Unspecified
	mustParseCIDR("::1/128"),       // Loopback
	mustParseCIDR("fc00::/7"),      // Unique Local Addresses (ULA)
	mustParseCIDR("fe80::/10"),     // Link-Local unicast
	mustParseCIDR("ff00::/8"),      // Multicast
	mustParseCIDR("2001:db8::/32"), // Documentation
}

func mustParseCIDR(s string) *net.IPNet {
	_, ipNet, err := net.ParseCIDR(s)
	if err != nil {
		panic(fmt.Sprintf("invalid CIDR %s: %v", s, err))
	}
	return ipNet
}

// IsBlockedIP checks if an IP belongs to any private, loopback, or metadata network
func IsBlockedIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	for _, cidr := range blockedCIDRs {
		if cidr.Contains(ip) {
			return true
		}
	}
	return false
}

// ValidateDestinationURL validates scheme and ensures hostname is not a prohibited IP literal or localhost
func ValidateDestinationURL(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("unsupported protocol scheme %q; only http and https are allowed", u.Scheme)
	}

	hostname := strings.ToLower(u.Hostname())
	if hostname == "" {
		return fmt.Errorf("URL hostname is required")
	}

	if hostname == "localhost" || strings.HasSuffix(hostname, ".localhost") {
		return fmt.Errorf("destination URL cannot target localhost")
	}

	if ip := net.ParseIP(hostname); ip != nil {
		if IsBlockedIP(ip) {
			return fmt.Errorf("destination URL cannot target private, loopback, or metadata IP addresses (%s)", ip.String())
		}
	}

	return nil
}


// NewSafeTransport creates an http.Transport with kernel-level socket controls to prevent SSRF and DNS rebinding attacks.
func NewSafeTransport(timeout time.Duration, allowPrivateIPs bool) *http.Transport {
	dialer := &net.Dialer{
		Timeout:   timeout,
		KeepAlive: 30 * time.Second,
		Control: func(network, address string, c syscall.RawConn) error {
			if allowPrivateIPs {
				return nil
			}

			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return fmt.Errorf("invalid socket address %q: %w", address, err)
			}

			ip := net.ParseIP(host)
			if ip == nil {
				return fmt.Errorf("invalid IP address in socket dial: %q", host)
			}

			if IsBlockedIP(ip) {
				return fmt.Errorf("SSRF protection: connection to restricted IP %s is blocked", ip.String())
			}

			return nil
		},
	}

	return &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return dialer.DialContext(ctx, network, addr)
		},
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   10,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
}
