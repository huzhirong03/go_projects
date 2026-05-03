package service

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"excel-master/internal/core"
)

// Emitter 是 service 层用到的事件发射器。和 core.EventEmitter 一致，
// 但带 TaskID 的场景由 WailsEmitter 实现。service 内部不关心是哪种实现。
type Emitter = core.EventEmitter

// EmitterFactory 根据 taskID 构造一个事件发射器。
// 测试时返回 Noop/Buffered；生产时返回 WailsEmitter。
type EmitterFactory func(taskID string, broker *filePromptBroker) Emitter

// Service 是应用服务层单例。由 main.go 或 app.go 创建，注入到 Wails App。
type Service struct {
	factory EmitterFactory
	broker  *filePromptBroker

	mu       sync.Mutex
	registry map[string]context.CancelFunc
	seq      uint64
}

// NewService 构造 Service。factory 必须非 nil；可以用 NewLogEmitterFactory 得到一个控制台实现。
func NewService(factory EmitterFactory) *Service {
	if factory == nil {
		panic("service: factory must not be nil")
	}
	return &Service{
		factory:  factory,
		broker:   newFilePromptBroker(),
		registry: map[string]context.CancelFunc{},
	}
}

// newTaskID 生成单调递增 + 时间的 TaskID。
func (s *Service) newTaskID() string {
	n := atomic.AddUint64(&s.seq, 1)
	return fmt.Sprintf("t-%d-%d", time.Now().UnixMilli(), n)
}

func (s *Service) register(taskID string, cancel context.CancelFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.registry[taskID] = cancel
}

func (s *Service) unregister(taskID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.registry, taskID)
}

// CancelTask 按 taskID 取消任务，未知 taskID 返回 false。
func (s *Service) CancelTask(taskID string) bool {
	s.mu.Lock()
	cancel, ok := s.registry[taskID]
	s.mu.Unlock()
	if !ok {
		return false
	}
	cancel()
	return true
}

// ActiveTasks 返回当前尚未完成的任务 ID 列表。
func (s *Service) ActiveTasks() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.registry))
	for id := range s.registry {
		out = append(out, id)
	}
	return out
}

func (s *Service) RespondFileBlocked(promptID, action string) bool {
	switch core.FileBlockedChoice(action) {
	case core.FileBlockedRetry, core.FileBlockedSkip, core.FileBlockedCancel:
		return s.broker.respond(promptID, core.FileBlockedChoice(action))
	default:
		return false
	}
}
