package ports

import "context"

type WebhookSender interface {
	Send(
		ctx context.Context,
		endpoint string,
		payload []byte,
		headers map[string]string,
	) (statusCode int, err error)
}
