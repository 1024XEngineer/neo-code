package services

import "context"

type memoryListProvider interface {
	ListMemoryItems(ctx context.Context) ([]MemoryListItem, error)
}

type manualMemoryWriter interface {
	SaveManualMemory(ctx context.Context, text string) error
}
