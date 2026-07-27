package dto

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/constant"
)

const ChannelTestRequestConfigVersion = 1

var channelTestSupportedEndpoints = map[constant.EndpointType]struct{}{
	constant.EndpointTypeOpenAI:                {},
	constant.EndpointTypeAnthropic:             {},
	constant.EndpointTypeGemini:                {},
	constant.EndpointTypeOpenAIResponse:        {},
	constant.EndpointTypeOpenAIResponseCompact: {},
	constant.EndpointTypeEmbeddings:            {},
	constant.EndpointTypeImageGeneration:       {},
	constant.EndpointTypeJinaRerank:            {},
}

type ChannelTestRequestConfig struct {
	Version   int                                   `json:"version"`
	Overrides map[string]ChannelTestContentOverride `json:"overrides"`
}

type ChannelTestContentOverride struct {
	Mode      string   `json:"mode,omitempty"`
	Content   *string  `json:"content,omitempty"`
	Input     *string  `json:"input,omitempty"`
	Prompt    *string  `json:"prompt,omitempty"`
	Query     *string  `json:"query,omitempty"`
	Documents []string `json:"documents,omitempty"`
}

type ChannelTestRequest struct {
	Model                string                                `json:"model"`
	EndpointType         string                                `json:"endpoint_type"`
	Stream               bool                                  `json:"stream"`
	TestRequestOverrides map[string]ChannelTestContentOverride `json:"test_request_overrides"`
}

type ChannelTestConfigPanelData struct {
	Version          int                                   `json:"version"`
	BuiltinOverrides map[string]ChannelTestContentOverride `json:"builtin_overrides"`
	Overrides        map[string]ChannelTestContentOverride `json:"overrides"`
}

func IsSupportedChannelTestEndpoint(endpoint constant.EndpointType) bool {
	_, ok := channelTestSupportedEndpoints[endpoint]
	return ok
}

func ValidateChannelTestRequestConfig(config ChannelTestRequestConfig, allowModes bool) error {
	if config.Version != ChannelTestRequestConfigVersion {
		return fmt.Errorf("unsupported channel test request config version: %d", config.Version)
	}
	if len(config.Overrides) > len(channelTestSupportedEndpoints) {
		return fmt.Errorf("too many channel test request overrides")
	}
	for endpointName, override := range config.Overrides {
		if err := ValidateChannelTestContentOverride(constant.EndpointType(endpointName), override, allowModes); err != nil {
			return err
		}
	}
	return nil
}

func ValidateChannelTestOverrides(overrides map[string]ChannelTestContentOverride) error {
	if len(overrides) > len(channelTestSupportedEndpoints) {
		return fmt.Errorf("too many channel test request overrides")
	}
	for endpointName, override := range overrides {
		if err := ValidateChannelTestContentOverride(constant.EndpointType(endpointName), override, true); err != nil {
			return err
		}
	}
	return nil
}

func ValidateChannelTestContentOverride(endpoint constant.EndpointType, override ChannelTestContentOverride, allowModes bool) error {
	if !IsSupportedChannelTestEndpoint(endpoint) {
		return fmt.Errorf("unsupported channel test endpoint: %s", endpoint)
	}
	if allowModes {
		if override.Mode != "builtin" && override.Mode != "custom" {
			return fmt.Errorf("invalid channel test override mode for %s", endpoint)
		}
		if override.Mode == "builtin" {
			if override.Content != nil || override.Input != nil || override.Prompt != nil || override.Query != nil || override.Documents != nil {
				return fmt.Errorf("builtin channel test override for %s must not include content", endpoint)
			}
			return nil
		}
	} else if override.Mode != "" {
		return fmt.Errorf("persistent channel test override for %s must not include mode", endpoint)
	}

	switch endpoint {
	case constant.EndpointTypeOpenAI, constant.EndpointTypeAnthropic, constant.EndpointTypeGemini, constant.EndpointTypeOpenAIResponse, constant.EndpointTypeOpenAIResponseCompact:
		if err := validateChannelTestString("content", override.Content); err != nil {
			return err
		}
		if override.Input != nil || override.Prompt != nil || override.Query != nil || override.Documents != nil {
			return fmt.Errorf("invalid fields for %s channel test override", endpoint)
		}
	case constant.EndpointTypeEmbeddings:
		if err := validateChannelTestString("input", override.Input); err != nil {
			return err
		}
		if override.Content != nil || override.Prompt != nil || override.Query != nil || override.Documents != nil {
			return fmt.Errorf("invalid fields for %s channel test override", endpoint)
		}
	case constant.EndpointTypeImageGeneration:
		if err := validateChannelTestString("prompt", override.Prompt); err != nil {
			return err
		}
		if override.Content != nil || override.Input != nil || override.Query != nil || override.Documents != nil {
			return fmt.Errorf("invalid fields for %s channel test override", endpoint)
		}
	case constant.EndpointTypeJinaRerank:
		if err := validateChannelTestString("query", override.Query); err != nil {
			return err
		}
		if override.Content != nil || override.Input != nil || override.Prompt != nil {
			return fmt.Errorf("invalid fields for %s channel test override", endpoint)
		}
		if len(override.Documents) < 1 || len(override.Documents) > 8 {
			return fmt.Errorf("jina-rerank documents must contain 1 to 8 items")
		}
		total := 0
		for _, document := range override.Documents {
			if !utf8.ValidString(document) || utf8.RuneCountInString(document) > 4096 || strings.TrimSpace(document) == "" {
				return fmt.Errorf("invalid jina-rerank document")
			}
			total += utf8.RuneCountInString(document)
		}
		if total > 16384 {
			return fmt.Errorf("jina-rerank documents are too long")
		}
	}
	return nil
}

func validateChannelTestString(name string, value *string) error {
	if value == nil || !utf8.ValidString(*value) || utf8.RuneCountInString(*value) > 4096 || strings.TrimSpace(*value) == "" {
		return fmt.Errorf("invalid channel test %s", name)
	}
	return nil
}
