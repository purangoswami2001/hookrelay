package httpclient

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"time"
)

// SignPayload computes HMAC-SHA256 signature (format: t=<ts>,v1=<sig>)
func SignPayload(secret string, payload []byte, timestamp time.Time) string {
	unixTimestamp := strconv.FormatInt(timestamp.Unix(), 10)
	message := unixTimestamp + "." + string(payload)

	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(message))

	// receiver verify: hmac.Equal(received, expected)
	return fmt.Sprintf(
		"t=%s,v1=%s",
		unixTimestamp,
		hex.EncodeToString(mac.Sum(nil)),
	)
}
