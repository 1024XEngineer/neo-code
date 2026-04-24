.PHONY: install-skills docs-gateway docs-gateway-check

GATEWAY_DOCS_GENERATOR := go run -tags gatewaydocgen ./scripts/generate_gateway_rpc_examples.go

install-skills:
	@./scripts/install_skills.sh

docs-gateway:
	@$(GATEWAY_DOCS_GENERATOR)

docs-gateway-check:
	@$(GATEWAY_DOCS_GENERATOR)
	@go run ./scripts/check_gateway_docs
	@git diff --exit-code -- docs/generated/gateway-rpc-examples.json
