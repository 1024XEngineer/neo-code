.PHONY: install-skills docs-gateway docs-gateway-check

install-skills:
	@./scripts/install_skills.sh

docs-gateway:
	@go run -tags gatewaydocgen ./scripts/generate_gateway_rpc_examples.go

docs-gateway-check:
	@go run -tags gatewaydocgen ./scripts/generate_gateway_rpc_examples.go
	@git diff --exit-code -- docs/generated/gateway-rpc-examples.json
