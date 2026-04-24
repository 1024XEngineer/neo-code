//go:build gatewaydocgen
// +build gatewaydocgen

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"neo-code/internal/gateway"
	"neo-code/internal/gateway/protocol"
)

type generatedExamples struct {
	GatewayBindStream struct {
		Request  protocol.JSONRPCRequest  `json:"request"`
		Response protocol.JSONRPCResponse `json:"response"`
	} `json:"gateway.bindStream"`
	GatewayRun struct {
		Request  protocol.JSONRPCRequest  `json:"request"`
		Response protocol.JSONRPCResponse `json:"response"`
	} `json:"gateway.run"`
	CommonError struct {
		Response protocol.JSONRPCResponse `json:"response"`
	} `json:"common.error"`
}

func main() {
	examples, err := buildExamples()
	if err != nil {
		fail("build examples", err)
	}
	raw, err := json.MarshalIndent(examples, "", "  ")
	if err != nil {
		fail("marshal examples", err)
	}
	outputPath := filepath.Join("docs", "generated", "gateway-rpc-examples.json")
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		fail("create output directory", err)
	}
	if err := os.WriteFile(outputPath, append(raw, '\n'), 0o644); err != nil {
		fail("write output file", err)
	}
	fmt.Printf("generated %s\n", outputPath)
}

func buildExamples() (generatedExamples, error) {
	var examples generatedExamples

	bindStreamRequestIDRaw, err := marshalRaw("bind-1")
	if err != nil {
		return generatedExamples{}, err
	}
	bindStreamParamsRaw, err := marshalRaw(protocol.BindStreamParams{
		SessionID: "sess-1",
		RunID:     "run-1",
		Channel:   "ws",
	})
	if err != nil {
		return generatedExamples{}, err
	}
	examples.GatewayBindStream.Request = protocol.JSONRPCRequest{
		JSONRPC: protocol.JSONRPCVersion,
		ID:      bindStreamRequestIDRaw,
		Method:  protocol.MethodGatewayBindStream,
		Params:  bindStreamParamsRaw,
	}
	bindStreamResultRaw, err := marshalRaw(gateway.MessageFrame{
		Type:      gateway.FrameTypeAck,
		Action:    gateway.FrameActionBindStream,
		RequestID: "bind-1",
		SessionID: "sess-1",
		RunID:     "run-1",
		Payload: map[string]any{
			"message": "stream binding updated",
			"channel": "ws",
		},
	})
	if err != nil {
		return generatedExamples{}, err
	}
	examples.GatewayBindStream.Response = protocol.JSONRPCResponse{
		JSONRPC: protocol.JSONRPCVersion,
		ID:      bindStreamRequestIDRaw,
		Result:  bindStreamResultRaw,
	}

	runRequestIDRaw, err := marshalRaw("run-req-1")
	if err != nil {
		return generatedExamples{}, err
	}
	runParamsRaw, err := marshalRaw(protocol.RunParams{
		SessionID: "sess-1",
		RunID:     "run-1",
		InputText: "Please review README",
		InputParts: []protocol.RunInputPart{
			{Type: "text", Text: "Please review README"},
		},
		Workdir: "/workspace/demo",
	})
	if err != nil {
		return generatedExamples{}, err
	}
	examples.GatewayRun.Request = protocol.JSONRPCRequest{
		JSONRPC: protocol.JSONRPCVersion,
		ID:      runRequestIDRaw,
		Method:  protocol.MethodGatewayRun,
		Params:  runParamsRaw,
	}
	runResultRaw, err := marshalRaw(gateway.MessageFrame{
		Type:      gateway.FrameTypeAck,
		Action:    gateway.FrameActionRun,
		RequestID: "run-req-1",
		SessionID: "sess-1",
		RunID:     "run-1",
		Payload: map[string]any{
			"message": "run accepted",
		},
	})
	if err != nil {
		return generatedExamples{}, err
	}
	examples.GatewayRun.Response = protocol.JSONRPCResponse{
		JSONRPC: protocol.JSONRPCVersion,
		ID:      runRequestIDRaw,
		Result:  runResultRaw,
	}

	commonErrorRequestIDRaw, err := marshalRaw("req-err-1")
	if err != nil {
		return generatedExamples{}, err
	}
	examples.CommonError.Response = protocol.NewJSONRPCErrorResponse(
		commonErrorRequestIDRaw,
		protocol.NewJSONRPCError(
			protocol.MapGatewayCodeToJSONRPCCode(gateway.ErrorCodeUnauthorized.String()),
			"unauthorized",
			gateway.ErrorCodeUnauthorized.String(),
		),
	)

	return examples, nil
}

func marshalRaw(payload any) (json.RawMessage, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(raw), nil
}

func fail(message string, err error) {
	fmt.Fprintf(os.Stderr, "%s: %v\n", message, err)
	os.Exit(1)
}
