package service

import (
	"strings"
	"testing"

	"excel-master/internal/core"
)

// captureEmitter 只关心 Error 调用，做最小 mock。
type captureEmitter struct {
	errs []error
}

func (c *captureEmitter) Progress(_ core.Progress) {}
func (c *captureEmitter) Log(_, _ string)          {}
func (c *captureEmitter) Done(_ any)               {}
func (c *captureEmitter) Error(err error) {
	c.errs = append(c.errs, err)
}

// TestRecoverToEmitter_NoPanic：没 panic 时 emitter 不应被调用。
func TestRecoverToEmitter_NoPanic(t *testing.T) {
	e := &captureEmitter{}
	func() {
		defer recoverToEmitter(e)
		_ = 1 + 1 // 正常执行
	}()
	if len(e.errs) != 0 {
		t.Fatalf("无 panic 时不应触发 emitter.Error，得到 %d 个错误", len(e.errs))
	}
}

// TestRecoverToEmitter_StringPanic：字符串 panic 应被转成 INTERNAL_PANIC 错误。
func TestRecoverToEmitter_StringPanic(t *testing.T) {
	e := &captureEmitter{}
	func() {
		defer recoverToEmitter(e)
		panic("boom: something went wrong")
	}()
	if len(e.errs) != 1 {
		t.Fatalf("期望 1 个错误，得到 %d", len(e.errs))
	}
	msg := e.errs[0].Error()
	if !strings.Contains(msg, "INTERNAL_PANIC") {
		t.Errorf("错误信息缺 INTERNAL_PANIC 标识：%s", msg)
	}
	if !strings.Contains(msg, "boom: something went wrong") {
		t.Errorf("错误信息没保留原 panic 值：%s", msg)
	}
	if !strings.Contains(msg, "堆栈追踪") {
		t.Errorf("错误信息缺堆栈追踪：%s", msg)
	}
}

// TestRecoverToEmitter_NilDeref：典型空指针 panic 应能恢复。
func TestRecoverToEmitter_NilDeref(t *testing.T) {
	e := &captureEmitter{}
	func() {
		defer recoverToEmitter(e)
		var p *struct{ X int }
		_ = p.X // 故意 nil deref
	}()
	if len(e.errs) != 1 {
		t.Fatalf("nil deref 应被恢复并 emit 1 个错误，得到 %d", len(e.errs))
	}
	if !strings.Contains(e.errs[0].Error(), "INTERNAL_PANIC") {
		t.Errorf("错误信息缺 INTERNAL_PANIC：%s", e.errs[0].Error())
	}
}
