package service

import (
	"sync"

	"excel-master/internal/core"
)

type filePromptBroker struct {
	mu      sync.Mutex
	pending map[string]chan core.FileBlockedChoice
}

func newFilePromptBroker() *filePromptBroker {
	return &filePromptBroker{pending: map[string]chan core.FileBlockedChoice{}}
}

func (b *filePromptBroker) register(promptID string) chan core.FileBlockedChoice {
	ch := make(chan core.FileBlockedChoice, 1)
	b.mu.Lock()
	b.pending[promptID] = ch
	b.mu.Unlock()
	return ch
}

func (b *filePromptBroker) unregister(promptID string) {
	b.mu.Lock()
	delete(b.pending, promptID)
	b.mu.Unlock()
}

func (b *filePromptBroker) respond(promptID string, choice core.FileBlockedChoice) bool {
	b.mu.Lock()
	ch, ok := b.pending[promptID]
	b.mu.Unlock()
	if !ok {
		return false
	}
	select {
	case ch <- choice:
		return true
	default:
		return false
	}
}
