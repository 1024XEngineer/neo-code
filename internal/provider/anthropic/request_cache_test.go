package anthropic

import (
	"strings"
	"testing"

	"neo-code/internal/provider"
	providertypes "neo-code/internal/provider/types"
)

// longStablePrefix 生成一段足以达到最小可缓存长度的稳定前缀。
func longStablePrefix() string {
	return strings.Repeat("stable-system-prompt-line\n", 200)
}

func TestBuildAnthropicSystemBlocks(t *testing.T) {
	t.Parallel()

	t.Run("空系统提示返回 nil", func(t *testing.T) {
		t.Parallel()
		blocks, used := buildAnthropicSystemBlocks(providertypes.GenerateRequest{}, true)
		if blocks != nil || used != 0 {
			t.Fatalf("blocks = %v, used = %d, want nil and 0", blocks, used)
		}
	})

	t.Run("无稳定前缀时单块且不打标", func(t *testing.T) {
		t.Parallel()
		req := providertypes.GenerateRequest{SystemPrompt: "whole prompt"}
		blocks, used := buildAnthropicSystemBlocks(req, true)
		if len(blocks) != 1 || blocks[0].Text != "whole prompt" {
			t.Fatalf("blocks = %v, want single block", blocks)
		}
		if used != 0 || blocks[0].CacheControl.Type != "" {
			t.Fatalf("expected no cache breakpoint, got used=%d control=%v", used, blocks[0].CacheControl)
		}
	})

	t.Run("前缀过短时不打标", func(t *testing.T) {
		t.Parallel()
		req := providertypes.GenerateRequest{
			SystemPrompt:       "short" + "\n\n" + "tail",
			SystemStablePrefix: "short",
		}
		blocks, used := buildAnthropicSystemBlocks(req, true)
		if used != 0 {
			t.Fatalf("used = %d, want 0 for short prefix", used)
		}
		if len(blocks) != 2 || blocks[0].Text != "short" || blocks[1].Text != "\n\ntail" {
			t.Fatalf("blocks = %v, want split without cache control", blocks)
		}
	})

	t.Run("够长的前缀打标并保留尾部", func(t *testing.T) {
		t.Parallel()
		prefix := longStablePrefix()
		req := providertypes.GenerateRequest{
			SystemPrompt:       prefix + "\n\ndynamic",
			SystemStablePrefix: prefix,
		}
		blocks, used := buildAnthropicSystemBlocks(req, true)
		if used != 1 {
			t.Fatalf("used = %d, want 1", used)
		}
		if len(blocks) != 2 {
			t.Fatalf("len(blocks) = %d, want 2", len(blocks))
		}
		if blocks[0].CacheControl.Type != "ephemeral" {
			t.Fatalf("first block cache control = %q, want ephemeral", blocks[0].CacheControl.Type)
		}
		if blocks[1].CacheControl.Type != "" {
			t.Fatal("tail block must not carry cache control")
		}
		if got := blocks[0].Text + blocks[1].Text; got != req.SystemPrompt {
			t.Fatal("split blocks must reconstruct the original system prompt")
		}
	})

	t.Run("整体稳定时单块打标", func(t *testing.T) {
		t.Parallel()
		prefix := longStablePrefix()
		req := providertypes.GenerateRequest{SystemPrompt: prefix, SystemStablePrefix: prefix}
		blocks, used := buildAnthropicSystemBlocks(req, true)
		if used != 1 || len(blocks) != 1 || blocks[0].CacheControl.Type != "ephemeral" {
			t.Fatalf("blocks = %v, used = %d, want single cached block", blocks, used)
		}
	})

	t.Run("禁用缓存时不打标但仍分段", func(t *testing.T) {
		t.Parallel()
		prefix := longStablePrefix()
		req := providertypes.GenerateRequest{
			SystemPrompt:       prefix + "\n\ndynamic",
			SystemStablePrefix: prefix,
		}
		blocks, used := buildAnthropicSystemBlocks(req, false)
		if used != 0 {
			t.Fatalf("used = %d, want 0 when cache disabled", used)
		}
		if len(blocks) != 2 || blocks[0].CacheControl.Type != "" {
			t.Fatalf("blocks = %v, want split without cache control", blocks)
		}
	})
}

func TestBuildRequestAppliesPromptCache(t *testing.T) {
	t.Parallel()

	prefix := longStablePrefix()
	req := providertypes.GenerateRequest{
		Model:              "claude-sonnet-4",
		SystemPrompt:       prefix + "\n\ndynamic",
		SystemStablePrefix: prefix,
		Tools: []providertypes.ToolSpec{
			{Name: "read_file", Description: strings.Repeat("read a file ", 300)},
			{Name: "grep", Description: strings.Repeat("search text ", 300)},
		},
	}

	params, err := BuildRequest(t.Context(), provider.RuntimeConfig{DefaultModel: "claude-sonnet-4"}, req)
	if err != nil {
		t.Fatalf("BuildRequest() error = %v", err)
	}
	if len(params.System) != 2 {
		t.Fatalf("len(System) = %d, want 2", len(params.System))
	}
	if params.System[0].CacheControl.Type != "ephemeral" {
		t.Fatal("system stable block should carry cache control")
	}
	if len(params.Tools) != 2 {
		t.Fatalf("len(Tools) = %d, want 2", len(params.Tools))
	}
	lastTool := params.Tools[len(params.Tools)-1]
	if lastTool.OfTool == nil || lastTool.OfTool.CacheControl.Type != "ephemeral" {
		t.Fatal("last tool should carry a cache breakpoint")
	}
}

func TestBuildRequestCacheDisabled(t *testing.T) {
	t.Parallel()

	prefix := longStablePrefix()
	req := providertypes.GenerateRequest{
		Model:              "claude-sonnet-4",
		SystemPrompt:       prefix + "\n\ndynamic",
		SystemStablePrefix: prefix,
	}
	params, err := BuildRequest(t.Context(), provider.RuntimeConfig{
		DefaultModel:    "claude-sonnet-4",
		PromptCacheMode: provider.PromptCacheModeOff,
	}, req)
	if err != nil {
		t.Fatalf("BuildRequest() error = %v", err)
	}
	for i, block := range params.System {
		if block.CacheControl.Type != "" {
			t.Fatalf("system block %d must not carry cache control when disabled", i)
		}
	}
}
