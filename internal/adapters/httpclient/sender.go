package httpclient

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
)

type Sender struct {
	client *http.Client
}

func NewSender(timeout time.Duration) *Sender {
	return NewSenderWithOptions(timeout, false)
}

func NewSenderWithOptions(timeout time.Duration, allowPrivateIPs bool) *Sender {
	return &Sender{
		client: &http.Client{
			Timeout:   timeout,
			Transport: NewSafeTransport(timeout, allowPrivateIPs),
			// don't follow redirects to prevent leaking signed payloads
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

func (s *Sender) Send(
	ctx context.Context,
	endpoint string,
	payload []byte,
	headers map[string]string,
) (int, error) {
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		endpoint,
		bytes.NewReader(payload),
	)
	if err != nil {
		return 0, fmt.Errorf("create webhook request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "HookRelay/1.0")

	for key, value := range headers {
		req.Header.Set(key, value)
	}

	// debug: fmt.Printf("POST %s (%d bytes)\n", endpoint, len(payload))

	resp, err := s.client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("send webhook request: %w", err)
	}
	defer resp.Body.Close()

	// read up to 64KB so connection can be reused
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64*1024))

	return resp.StatusCode, nil
}
