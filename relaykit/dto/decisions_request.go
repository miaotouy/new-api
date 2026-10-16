package dto

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/QuantumNous/new-api/relaykit/types"
)

// DecisionsRequest carries shared context and independently evaluated questions.
// RawBody retains provider routing, multimodal entries and future extensions.
type DecisionsRequest struct {
	Model               string                      `json:"model"`
	State               json.RawMessage             `json:"state"`
	Questions           map[string]DecisionQuestion `json:"questions"`
	Stream              *bool                       `json:"stream,omitempty"`
	MaxTokens           *uint                       `json:"max_tokens,omitempty"`
	MaxCompletionTokens *uint                       `json:"max_completion_tokens,omitempty"`
	MaxOutputTokens     *uint                       `json:"max_output_tokens,omitempty"`
	RawBody             json.RawMessage             `json:"-"`
}

type DecisionQuestion struct {
	Type         string          `json:"type"`
	Instructions json.RawMessage `json:"instructions,omitempty"`
	Criteria     json.RawMessage `json:"criteria,omitempty"`
}

// Validate accepts structured entries unchanged, including legacy null entries.
func (r *DecisionsRequest) Validate() error {
	if strings.TrimSpace(r.Model) == "" {
		return errors.New("model is required")
	}
	if !validDecisionEntry(r.State) {
		return errors.New("state must be text, an object, an array, or null")
	}
	if len(r.Questions) == 0 {
		return errors.New("questions must be a nonempty object")
	}
	if r.Stream != nil && *r.Stream {
		return errors.New("decisions do not support streaming")
	}
	for id, question := range r.Questions {
		if len(question.Instructions) > 0 && !validDecisionEntry(question.Instructions) {
			return fmt.Errorf("question %q has invalid instructions", id)
		}
		switch question.Type {
		case "choice", "noul":
			criteria := bytes.TrimSpace(question.Criteria)
			if question.Type == "noul" && (len(criteria) == 0 || bytes.Equal(criteria, []byte("null"))) {
				continue
			}
			var options map[string]json.RawMessage
			if err := kitutil.Unmarshal(criteria, &options); err != nil || options == nil {
				return fmt.Errorf("question %q requires object criteria", id)
			}
			if question.Type == "choice" && len(options) == 0 {
				return fmt.Errorf("question %q requires at least one choice", id)
			}
			for key, value := range options {
				if question.Type == "noul" && key != "true" && key != "false" {
					return fmt.Errorf("question %q noul criteria may only describe true and false", id)
				}
				if !validDecisionEntry(value) {
					return fmt.Errorf("question %q has invalid criteria", id)
				}
			}
		case "score":
			var levels []json.RawMessage
			if err := kitutil.Unmarshal(question.Criteria, &levels); err != nil || len(levels) == 0 {
				return fmt.Errorf("question %q requires a nonempty criteria array", id)
			}
			for _, value := range levels {
				if !validDecisionEntry(value) {
					return fmt.Errorf("question %q has invalid criteria", id)
				}
			}
		case "box":
			if !validDecisionEntry(question.Instructions) {
				return fmt.Errorf("question %q requires box instructions", id)
			}
		default:
			return fmt.Errorf("question %q has unsupported type %q", id, question.Type)
		}
	}
	return nil
}

func validDecisionEntry(raw json.RawMessage) bool {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return false
	}
	switch raw[0] {
	case '"', '{', '[':
		return true
	case 'n':
		return bytes.Equal(raw, []byte("null"))
	}
	return false
}

func (r *DecisionsRequest) GetTokenCountMeta() *types.TokenCountMeta {
	// Provider tokenizers differ; the host uses the encoded payload as a
	// input estimate and settles validated upstream usage when it is available.
	var outputBudget uint
	for _, limit := range []*uint{r.MaxTokens, r.MaxCompletionTokens, r.MaxOutputTokens} {
		if limit != nil {
			outputBudget = max(outputBudget, *limit)
		}
	}
	return &types.TokenCountMeta{
		TokenType: types.TokenTypeTextNumber, CombineText: string(r.RawBody),
		MaxTokens: int(outputBudget),
	}
}
func (r *DecisionsRequest) IsStream(_ *http.Request) bool { return false }
func (r *DecisionsRequest) SetModelName(name string) {
	if name != "" {
		r.Model = name
	}
}
