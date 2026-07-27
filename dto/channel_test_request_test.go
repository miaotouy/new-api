package dto

import (
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateChannelTestContentOverrideRejectsInvalidContent(t *testing.T) {
	content := "   "
	err := ValidateChannelTestContentOverride(constant.EndpointTypeOpenAI, ChannelTestContentOverride{
		Mode:    "custom",
		Content: &content,
	}, true)
	require.Error(t, err)

	tooLong := strings.Repeat("x", 4097)
	err = ValidateChannelTestContentOverride(constant.EndpointTypeEmbeddings, ChannelTestContentOverride{
		Mode:  "custom",
		Input: &tooLong,
	}, true)
	require.Error(t, err)

	err = ValidateChannelTestContentOverride(constant.EndpointTypeJinaRerank, ChannelTestContentOverride{
		Mode:      "builtin",
		Documents: []string{"must not be present"},
	}, true)
	assert.Error(t, err)
}

func TestValidateChannelTestContentOverrideAllowsRerankDocuments(t *testing.T) {
	query := "What is deep learning?"
	err := ValidateChannelTestContentOverride(constant.EndpointTypeJinaRerank, ChannelTestContentOverride{
		Mode:      "custom",
		Query:     &query,
		Documents: []string{"Neural networks have multiple layers.", "Databases store structured data."},
	}, true)
	require.NoError(t, err)
}
