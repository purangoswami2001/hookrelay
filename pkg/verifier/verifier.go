package verifier

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

var (
	ErrMissingHeader       = errors.New("hookrelay: missing X-HookRelay-Signature header")
	ErrInvalidHeaderFormat = errors.New("hookrelay: invalid signature header format")
	ErrTimestampExpired    = errors.New("hookrelay: timestamp outside tolerance window (potential replay)")
	ErrInvalidSignature    = errors.New("hookrelay: signature mismatch")
	ErrEmptySecret         = errors.New("hookrelay: secret cannot be empty")
)

const (
	// DefaultTolerance is the recommended timestamp drift tolerance (5 minutes)
	DefaultTolerance = 5 * time.Minute
	// HeaderSignature is the HTTP header containing the signature
	HeaderSignature = "X-HookRelay-Signature"
)

// Verify verifies the HMAC-SHA256 signature and timestamp tolerance of an inbound HookRelay webhook payload.
// Returns nil if valid, or a descriptive error if verification fails.
func Verify(payload []byte, signatureHeader string, secret string, tolerance time.Duration) error {
	if strings.TrimSpace(secret) == "" {
		return ErrEmptySecret
	}

	timestampStr, signatureHex, err := parseHeader(signatureHeader)
	if err != nil {
		return err
	}

	ts, err := strconv.ParseInt(timestampStr, 10, 64)
	if err != nil {
		return fmt.Errorf("%w: invalid timestamp: %v", ErrInvalidHeaderFormat, err)
	}

	// Verify timestamp drift to prevent replay attacks
	if tolerance > 0 {
		webhookTime := time.Unix(ts, 0)
		diff := time.Since(webhookTime)
		if diff < 0 {
			diff = -diff
		}
		if diff > tolerance {
			return ErrTimestampExpired
		}
	}

	// Compute expected HMAC-SHA256: HMAC(secret, timestamp + "." + payload)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestampStr + "."))
	mac.Write(payload)
	expectedSig := hex.EncodeToString(mac.Sum(nil))

	// Constant-time comparison to protect against timing attacks
	if !hmac.Equal([]byte(expectedSig), []byte(signatureHex)) {
		return ErrInvalidSignature
	}

	return nil
}

// VerifyRequest extracts the signature header and body from an *http.Request, verifies it,
// and returns the raw payload bytes for downstream processing.
func VerifyRequest(r *http.Request, secret string, tolerance time.Duration) ([]byte, error) {
	sigHeader := r.Header.Get(HeaderSignature)
	if sigHeader == "" {
		return nil, ErrMissingHeader
	}

	payload, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, fmt.Errorf("hookrelay: failed to read request body: %w", err)
	}

	if err := Verify(payload, sigHeader, secret, tolerance); err != nil {
		return nil, err
	}

	return payload, nil
}

func parseHeader(header string) (timestamp string, sigHex string, err error) {
	parts := strings.Split(header, ",")
	for _, part := range parts {
		kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(kv) != 2 {
			continue
		}
		switch kv[0] {
		case "t":
			timestamp = kv[1]
		case "v1":
			sigHex = kv[1]
		}
	}

	if timestamp == "" || sigHex == "" {
		return "", "", ErrInvalidHeaderFormat
	}

	return timestamp, sigHex, nil
}
