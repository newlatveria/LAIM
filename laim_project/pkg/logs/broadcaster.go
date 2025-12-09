package logs

import "sync"

var (
	SubscribersMu sync.Mutex
	Subscribers   = map[chan string]struct{}{}

	TailMu    sync.Mutex
	TailLines []string
	TailMax   = 500
)
