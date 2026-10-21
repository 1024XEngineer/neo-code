package types

import "testing"

func TestGenerateRequestResolvedStablePrefix(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		req  GenerateRequest
		want string
	}{
		{
			name: "空前缀返回空",
			req:  GenerateRequest{SystemPrompt: "stable dynamic"},
			want: "",
		},
		{
			name: "真实前缀被接受",
			req: GenerateRequest{
				SystemPrompt:       "stable\n\ndynamic",
				SystemStablePrefix: "stable",
			},
			want: "stable",
		},
		{
			name: "非真实前缀被拒",
			req: GenerateRequest{
				SystemPrompt:       "stable\n\ndynamic",
				SystemStablePrefix: "other",
			},
			want: "",
		},
		{
			name: "整段作为前缀",
			req: GenerateRequest{
				SystemPrompt:       "all-stable",
				SystemStablePrefix: "all-stable",
			},
			want: "all-stable",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.req.ResolvedStablePrefix(); got != tc.want {
				t.Fatalf("ResolvedStablePrefix() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestGenerateRequestVolatileSystemSuffixPreservesBytes(t *testing.T) {
	t.Parallel()

	const stable = "## 角色\n你是编码助手。"
	const dynamic = "## 当前任务\n修复登录。"
	req := GenerateRequest{
		SystemPrompt:       stable + "\n\n" + dynamic,
		SystemStablePrefix: stable,
	}

	if got, want := req.VolatileSystemSuffix(), "\n\n"+dynamic; got != want {
		t.Fatalf("VolatileSystemSuffix() = %q, want %q", got, want)
	}
	if got := req.ResolvedStablePrefix() + req.VolatileSystemSuffix(); got != req.SystemPrompt {
		t.Fatalf("prefix + suffix = %q, want %q", got, req.SystemPrompt)
	}
}

func TestGenerateRequestVolatileSystemSuffixWithoutPrefix(t *testing.T) {
	t.Parallel()

	req := GenerateRequest{SystemPrompt: "whole"}
	if got := req.VolatileSystemSuffix(); got != "whole" {
		t.Fatalf("VolatileSystemSuffix() = %q, want %q", got, "whole")
	}
}

func TestUsageTotalInputTokensAndHitRate(t *testing.T) {
	t.Parallel()

	usage := Usage{
		InputTokens:           500,
		CacheReadInputTokens:  1500,
		CacheWriteInputTokens: 0,
		OutputTokens:          200,
	}
	if got, want := usage.TotalInputTokens(), 2000; got != want {
		t.Fatalf("TotalInputTokens() = %d, want %d", got, want)
	}
	if got, want := usage.CacheHitRate(), 0.75; got != want {
		t.Fatalf("CacheHitRate() = %v, want %v", got, want)
	}

	if got := (Usage{}).CacheHitRate(); got != 0 {
		t.Fatalf("CacheHitRate() on empty usage = %v, want 0", got)
	}
}
