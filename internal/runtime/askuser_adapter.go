package runtime

import (
	"context"

	"neo-code/internal/runtime/askuser"
	"neo-code/internal/tools"
)

// askUserBrokerAdapter 将 runtime askuser.Broker 适配为 tools.AskUserBroker 接口。
type askUserBrokerAdapter struct {
	broker *askuser.Broker
}

func newAskUserBrokerAdapter(broker *askuser.Broker) *askUserBrokerAdapter {
	return &askUserBrokerAdapter{broker: broker}
}

func (a *askUserBrokerAdapter) Open(ctx context.Context, request tools.AskUserRequest) (string, tools.AskUserResult, error) {
	req := askuser.Request{
		RequestID:   request.RequestID,
		QuestionID:  request.QuestionID,
		Title:       request.Title,
		Description: request.Description,
		Kind:        request.Kind,
		Options:     convertAskUserOptions(request.Options),
		Required:    request.Required,
		AllowSkip:   request.AllowSkip,
		MaxChoices:  request.MaxChoices,
		TimeoutSec:  request.TimeoutSec,
	}

	requestID, result, err := a.broker.Open(ctx, req)
	if err != nil {
		return requestID, tools.AskUserResult{
			Status:     result.Status,
			QuestionID: result.QuestionID,
			Values:     result.Values,
			Message:    result.Message,
		}, err
	}

	return requestID, tools.AskUserResult{
		Status:     result.Status,
		QuestionID: result.QuestionID,
		Values:     result.Values,
		Message:    result.Message,
	}, nil
}

func convertAskUserOptions(options []tools.AskUserOption) []askuser.Option {
	if len(options) == 0 {
		return nil
	}
	result := make([]askuser.Option, len(options))
	for i, o := range options {
		result[i] = askuser.Option{
			Label:       o.Label,
			Description: o.Description,
		}
	}
	return result
}
