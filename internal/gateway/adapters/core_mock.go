package adapters

import "context"

const defaultPongMessage = "Pong"

// CoreClient 定义网关访问核心能力的最小端口接口。
type CoreClient interface {
	Ping(ctx context.Context) (string, error)
}

// CoreMock 是 CoreClient 的最小可运行 mock 实现。
type CoreMock struct {
	pong string
}

// NewCoreMock 创建默认返回 Pong 的核心 mock。
func NewCoreMock() *CoreMock {
	return &CoreMock{pong: defaultPongMessage}
}

// NewCoreMockWithPong 创建可自定义 Pong 文案的核心 mock。
func NewCoreMockWithPong(pong string) *CoreMock {
	if pong == "" {
		pong = defaultPongMessage
	}
	return &CoreMock{pong: pong}
}

// Ping 返回 mock 端固定的 pong 结果。
func (m *CoreMock) Ping(_ context.Context) (string, error) {
	if m == nil {
		return defaultPongMessage, nil
	}
	return m.pong, nil
}
