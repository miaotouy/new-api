package common

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestJsonRawMessageToString(t *testing.T) {
	tests := []struct {
		name string
		data json.RawMessage
		want string
	}{
		{
			name: "object",
			data: json.RawMessage(`{"city":"Paris","days":0,"strict":false}`),
			want: `{"city":"Paris","days":0,"strict":false}`,
		},
		{
			name: "string",
			data: json.RawMessage(`"{\"city\":\"Paris\",\"days\":0,\"strict\":false}"`),
			want: `{"city":"Paris","days":0,"strict":false}`,
		},
		{
			name: "null",
			data: json.RawMessage(`null`),
			want: "",
		},
		{
			name: "empty",
			data: nil,
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, JsonRawMessageToString(tt.data))
		})
	}
}

func TestDecodeJsonStrictRejectsUnknownFields(t *testing.T) {
	type nested struct {
		Content string `json:"content"`
	}
	type payload struct {
		Override nested `json:"override"`
	}

	var value payload
	err := DecodeJsonStrict(strings.NewReader(`{"override":{"content":"hello","unexpected":true}}`), &value)
	require.ErrorContains(t, err, "unknown field")
}

func TestDecodeJsonStrictRejectsMultipleValues(t *testing.T) {
	var value map[string]any
	err := DecodeJsonStrict(strings.NewReader(`{"value":1} {"value":2}`), &value)
	require.ErrorContains(t, err, "multiple JSON values")
}
