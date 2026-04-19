package gateway

import "errors"

var (
	// ErrRuntimeAccessDenied 表示运行时拒绝当前主体访问目标资源。
	ErrRuntimeAccessDenied = errors.New("runtime access denied")
	// ErrRuntimeResourceNotFound 表示运行时未找到目标资源。
	ErrRuntimeResourceNotFound = errors.New("runtime resource not found")
)

