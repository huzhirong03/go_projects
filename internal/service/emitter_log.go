package service

import "excel-master/internal/pipeline"

// NewLogEmitterFactory 返回一个把所有事件打到控制台的工厂。
// 主要用于命令行或开发期调试（app.go 不用这个，会注入 WailsEmitterFactory）。
func NewLogEmitterFactory() EmitterFactory {
	return func(taskID string) Emitter {
		return pipeline.LogEmitter{}
	}
}
