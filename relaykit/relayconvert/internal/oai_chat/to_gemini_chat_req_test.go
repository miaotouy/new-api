package oaichat

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/convmeta"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpenAIChatToGeminiMarkdownImages(t *testing.T) {
	const png = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+aZ1sAAAAASUVORK5CYII="
	const image = "![image](data:image/png;base64," + png + ")"
	imagePart := dto.GeminiPart{InlineData: &dto.GeminiInlineData{MimeType: "image/png", Data: png}}
	enabled := &convmeta.Values{Options: &convmeta.Options{Gemini: convmeta.GeminiOptions{MarkdownImageConversionEnabled: true}}}
	tests := []struct {
		name string
		meta convmeta.Meta
		text string
		want []dto.GeminiPart
	}{
		{
			name: "disabled by default preserves valid image and text",
			text: "before " + image + " after",
			want: []dto.GeminiPart{{Text: "before " + image + " after"}},
		},
		{
			name: "explicitly disabled preserves invalid example",
			meta: &convmeta.Values{Options: &convmeta.Options{Gemini: convmeta.GeminiOptions{MarkdownImageConversionEnabled: false}}},
			text: "example `![...](data:...)`",
			want: []dto.GeminiPart{{Text: "example `![...](data:...)`"}},
		},
		{
			name: "enabled converts image and preserves surrounding text",
			meta: enabled,
			text: "before " + image + " after",
			want: []dto.GeminiPart{{Text: "before "}, imagePart, {Text: " after"}},
		},
		{
			name: "enabled preserves the recalled bug example",
			meta: enabled,
			text: "![link](https://example.com/a.png) then `![...](data:...)` after",
			want: []dto.GeminiPart{{Text: "![link](https://example.com/a.png) then `![...](data:...)` after"}},
		},
		{
			name: "enabled preserves invalid base64 and non-image data",
			meta: enabled,
			text: "![bad](data:image/png;base64,AAAA=) ![file](data:text/plain;base64,aGVsbG8=)",
			want: []dto.GeminiPart{{Text: "![bad](data:image/png;base64,AAAA=) ![file](data:text/plain;base64,aGVsbG8=)"}},
		},
		{
			name: "multiple images preserve intervening invalid markdown and suffix",
			meta: enabled,
			text: image + " ![bad](data:image/png;base64,AAAA=) " + image + " end",
			want: []dto.GeminiPart{imagePart, {Text: " ![bad](data:image/png;base64,AAAA=) "}, imagePart, {Text: " end"}},
		},
		{
			name: "unrelated markdown image remains intact",
			meta: enabled,
			text: "![link](https://example.com/a.png) before " + image,
			want: []dto.GeminiPart{{Text: "![link](https://example.com/a.png) before "}, imagePart},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := OpenAIChatRequestToGeminiGenerateContent(context.Background(), dto.GeneralOpenAIRequest{
				Model:    "gemini-test",
				Messages: []dto.Message{{Role: "user", Content: tt.text}},
			}, tt.meta)
			require.NoError(t, err)
			require.Len(t, got.Contents, 1)
			assert.Equal(t, tt.want, got.Contents[0].Parts)
		})
	}
}
