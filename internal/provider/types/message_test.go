package types

import "testing"

func TestMessageIsEmpty(t *testing.T) {
	tests := []struct {
		name string
		msg  Message
		want bool
	}{
		{
			name: "no parts and no tool calls",
			msg:  Message{},
			want: true,
		},
		{
			name: "whitespace-only text part",
			msg: Message{
				Parts: []ContentPart{NewTextPart("   \n\t")},
			},
			want: true,
		},
		{
			name: "non-empty text part",
			msg: Message{
				Parts: []ContentPart{NewTextPart("hello")},
			},
			want: false,
		},
		{
			name: "image part",
			msg: Message{
				Parts: []ContentPart{NewRemoteImagePart("https://example.com/image.png")},
			},
			want: false,
		},
		{
			name: "tool calls present",
			msg: Message{
				Parts:     []ContentPart{NewTextPart("")},
				ToolCalls: []ToolCall{{ID: "call_1", Name: "read_file"}},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.msg.IsEmpty(); got != tt.want {
				t.Fatalf("IsEmpty() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMessageValidate(t *testing.T) {
	t.Run("valid message", func(t *testing.T) {
		msg := Message{
			Role: "user",
			Parts: []ContentPart{
				NewTextPart("hello"),
				NewRemoteImagePart("https://example.com/image.png"),
			},
		}
		if err := msg.Validate(); err != nil {
			t.Fatalf("Validate() error = %v", err)
		}
	})

	t.Run("invalid parts", func(t *testing.T) {
		msg := Message{
			Role: "user",
			Parts: []ContentPart{{
				Kind: ContentPartImage,
				Image: &ImagePart{
					SourceType: ImageSourceType("unsupported"),
				},
			}},
		}
		if err := msg.Validate(); err == nil {
			t.Fatalf("expected Validate() to fail for unsupported image source")
		}
	})
}
