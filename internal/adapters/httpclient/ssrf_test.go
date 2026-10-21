package httpclient_test

import (
	"net"
	"testing"

	"github.com/purangoswami2001/hookrelay/internal/adapters/httpclient"
)

func TestIsBlockedIP(t *testing.T) {
	tests := []struct {
		ip      string
		blocked bool
	}{
		{"127.0.0.1", true},
		{"::1", true},
		{"10.0.0.1", true},
		{"10.254.0.1", true},
		{"172.16.0.1", true},
		{"172.31.255.255", true},
		{"192.168.1.1", true},
		{"169.254.169.254", true}, // AWS / GCP / Azure IMDS
		{"0.0.0.0", true},
		{"8.8.8.8", false},
		{"1.1.1.1", false},
		{"93.184.216.34", false},
	}

	for _, tc := range tests {
		ip := net.ParseIP(tc.ip)
		if ip == nil {
			t.Fatalf("failed to parse IP %s", tc.ip)
		}
		got := httpclient.IsBlockedIP(ip)
		if got != tc.blocked {
			t.Errorf("IsBlockedIP(%s) = %v, expected %v", tc.ip, got, tc.blocked)
		}
	}
}

func TestValidateDestinationURL(t *testing.T) {
	tests := []struct {
		url     string
		wantErr bool
	}{
		{"https://api.example.com/webhook", false},
		{"http://my-service.domain.org/events", false},
		{"http://169.254.169.254/latest/meta-data", true}, // IMDS
		{"http://127.0.0.1:8080/hook", true},              // Localhost IP
		{"http://localhost:3000/webhook", true},           // Localhost hostname
		{"http://10.0.0.5/api", true},                     // Private IP literal
		{"ftp://example.com/webhook", true},               // Invalid scheme
		{"javascript:alert(1)", true},                     // Invalid scheme
	}

	for _, tc := range tests {
		err := httpclient.ValidateDestinationURL(tc.url)
		if (err != nil) != tc.wantErr {
			t.Errorf("ValidateDestinationURL(%q) err = %v, wantErr = %v", tc.url, err, tc.wantErr)
		}
	}
}
