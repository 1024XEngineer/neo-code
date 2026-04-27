package gateway

import "testing"

func installHandlerRegistryForTest(t *testing.T, handlers map[FrameAction]requestFrameHandler) {
	t.Helper()

	originalRegistry := defaultRegistry
	replacement := &ActionRegistry{
		core:     make(map[FrameAction]requestFrameHandler, len(handlers)),
		extended: make(map[FrameAction]requestFrameHandler),
	}
	for action, handler := range handlers {
		replacement.core[action] = handler
	}
	defaultRegistry = replacement

	t.Cleanup(func() {
		defaultRegistry = originalRegistry
	})
}
