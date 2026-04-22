.PHONY: install-skills docs-gateway docs-gateway-check

install-skills:
	@./scripts/install_skills.sh

docs-gateway:
	@go run ./scripts/generate_gateway_rpc_examples

docs-gateway-check:
	@go run ./scripts/generate_gateway_rpc_examples
	@git diff --exit-code -- docs/reference/gateway-rpc-api.md
