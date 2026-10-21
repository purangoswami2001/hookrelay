package verifier_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/purangoswami2001/hookrelay/internal/adapters/httpclient"
	"github.com/purangoswami2001/hookrelay/pkg/verifier"
)

func TestVerify_ValidSignature(t *testing.T) {
	secret := "whsec_test_secret_12345"
	payload := []byte(`{"id":"evt-123","type":"invoice.paid","data":{"amount":9900}}`)
	now := time.Now()

	sigHeader := httpclient.SignPayload(secret, payload, now)

	err := verifier.Verify(payload, sigHeader, secret, 5*time.Minute)
	if err != nil {
		t.Fatalf("expected valid signature verification, got: %v", err)
	}
}

func TestVerify_TamperedPayload(t *testing.T) {
	secret := "whsec_test_secret_12345"
	payload := []byte(`{"id":"evt-123","type":"invoice.paid","data":{"amount":9900}}`)
	tamperedPayload := []byte(`{"id":"evt-123","type":"invoice.paid","data":{"amount":9999}}`)
	now := time.Now()

	sigHeader := httpclient.SignPayload(secret, payload, now)

	err := verifier.Verify(tamperedPayload, sigHeader, secret, 5*time.Minute)
	if err != verifier.ErrInvalidSignature {
		t.Fatalf("expected ErrInvalidSignature for tampered payload, got: %v", err)
	}
}

func TestVerify_ExpiredTimestamp(t *testing.T) {
	secret := "whsec_test_secret_12345"
	payload := []byte(`{"event":"test"}`)
	past := time.Now().Add(-10 * time.Minute)

	sigHeader := httpclient.SignPayload(secret, payload, past)

	err := verifier.Verify(payload, sigHeader, secret, 5*time.Minute)
	if err != verifier.ErrTimestampExpired {
		t.Fatalf("expected ErrTimestampExpired for old timestamp, got: %v", err)
	}
}

func TestVerify_MalformedHeader(t *testing.T) {
	secret := "whsec_test_secret_12345"
	payload := []byte(`{"event":"test"}`)

	badHeaders := []string{
		"",
		"invalid",
		"t=12345",
		"v1=abcde",
		"t=invalid,v1=abcde",
	}

	for _, bh := range badHeaders {
		err := verifier.Verify(payload, bh, secret, 5*time.Minute)
		if err == nil {
			t.Errorf("expected error for header %q, got nil", bh)
		}
	}
}

func TestVerifyRequest_Valid(t *testing.T) {
	secret := "whsec_super_secret"
	payload := []byte(`{"type":"order.created","id":"ord-555"}`)
	now := time.Now()

	req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader(payload))
	req.Header.Set("X-HookRelay-Signature", httpclient.SignPayload(secret, payload, now))
	req.Header.Set("X-HookRelay-Timestamp", strconv.FormatInt(now.Unix(), 10))

	verifiedPayload, err := verifier.VerifyRequest(req, secret, 5*time.Minute)
	if err != nil {
		t.Fatalf("expected successful VerifyRequest, got: %v", err)
	}

	if !bytes.Equal(verifiedPayload, payload) {
		t.Fatalf("expected payload %s, got %s", payload, verifiedPayload)
	}
}
