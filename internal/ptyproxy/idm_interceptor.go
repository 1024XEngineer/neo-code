//go:build !windows

package ptyproxy

import "strings"

const (
	idmAIPrefix        = "@ai "
	idmEscapedAIPrefix = "\\@ai "
)

// idmRouteKind 描述 IDM 单行输入路由目标。
type idmRouteKind string

const (
	idmRoutePassThrough idmRouteKind = "pass_through"
	idmRouteAskAI       idmRouteKind = "ask_ai"
	idmRouteExit        idmRouteKind = "exit"
)

// idmRouteDecision 描述 IDM 单行路由决议。
type idmRouteDecision struct {
	Kind    idmRouteKind
	Payload string
}

// routeIDMInput 根据输入内容执行三级路由判定。
func routeIDMInput(line string) idmRouteDecision {
	trimmed := strings.TrimSpace(line)
	if strings.EqualFold(trimmed, "exit") {
		return idmRouteDecision{Kind: idmRouteExit}
	}
	if strings.HasPrefix(line, idmEscapedAIPrefix) {
		return idmRouteDecision{
			Kind:    idmRoutePassThrough,
			Payload: strings.TrimPrefix(line, "\\"),
		}
	}
	if strings.HasPrefix(line, idmAIPrefix) {
		return idmRouteDecision{
			Kind:    idmRouteAskAI,
			Payload: strings.TrimSpace(strings.TrimPrefix(line, idmAIPrefix)),
		}
	}
	return idmRouteDecision{
		Kind:    idmRoutePassThrough,
		Payload: line,
	}
}
