package sessions

import (
	"sync"

	"github.com/ollama/ollama/api"
)

var SessionHistory = struct {
	sync.Mutex
	H map[string][]api.Message
}{H: make(map[string][]api.Message)}
