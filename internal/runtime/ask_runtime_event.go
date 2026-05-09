package runtime

// captureAskRuntimeEvent 为 Ask 链路预留事件观测入口。
// 当前 Ask 事件在桥接层完成映射，此处保持空实现以保证统一 emit 管线稳定。
func (s *Service) captureAskRuntimeEvent(_ RuntimeEvent) {}
