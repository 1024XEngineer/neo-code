package services

import "context"

type memoryListProvider interface {
	ListMemoryItems(ctx context.Context) ([]MemoryListItem, error)
}
