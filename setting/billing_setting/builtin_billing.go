package billing_setting

import "github.com/QuantumNous/new-api/constant"

// deepSeekPeak is DeepSeek's peak window: Monday to Friday, 09:00-12:00 and
// 14:00-18:00 Beijing time. The Chinese public holidays DeepSeek bills
// off-peak are not known in advance and bill at peak here.
const deepSeekPeak = `weekday("Asia/Shanghai") >= 1 && weekday("Asia/Shanghai") <= 5 && ((hour("Asia/Shanghai") >= 9 && hour("Asia/Shanghai") < 12) || (hour("Asia/Shanghai") >= 14 && hour("Asia/Shanghai") < 18))`

const deepSeekFlashExpr = deepSeekPeak + ` ? tier("peak", p * 0.3 + cr * 0.006 + c * 1.2) : tier("off_peak", p * 0.15 + cr * 0.003 + c * 0.6)`

// Built-in token prices use actual USD per million tokens. Keep new model
// defaults here instead of splitting them across the legacy ratio tables.
var builtinBillingExpr = map[string]string{
	// https://developers.openai.com/api/docs/pricing (Standard, 2026-09-09).
	// The Images API reports image output in output_tokens, normalized to c.
	"gpt-image-2":            `tier("standard", p * 5 + cr * 1.25 + img * 8 + img_cr * 2 + c * 30)`,
	"gpt-image-2.5-sunburst": `tier("standard", p * 5 + cr * 1.25 + img * 8 + img_cr * 2 + c * 30)`,
	"gpt-image-2.5-flare":    `tier("standard", p * 5 + cr * 1.25 + img * 8 + img_cr * 2 + c * 30)`,
	// https://developers.openai.com/api/docs/models/gpt-6-astra
	// Standard pricing; the long-context rates apply to the whole request.
	// Do not infer service-tier discounts from incoming request parameters:
	// channels filter service_tier by default, so it may not reach the upstream.
	"gpt-6-astra": `len <= 272000 ? tier("standard", p * 10 + c * 50 + cr * 1 + cc * 12.5) : tier("long_context", p * 20 + c * 75 + cr * 2 + cc * 25)`,
	// https://docs.typesafe.ai/models.md (Jev 1.13): $0.042 per million
	// input tokens, output free. Retain the installed TypeSafe usage-expression
	// contract in USD; the decisions relay supplies the same input_tokens fact.
	"jev-1.13.0":  `tier("base", u("input_tokens") * 0.042 / 1000000)`,
	"jev-latest":  `tier("base", u("input_tokens") * 0.042 / 1000000)`,
	"jev-preview": `tier("base", u("input_tokens") * 0.042 / 1000000)`,
	// OpenRouter decision model prices, USD per million tokens (2026-10-17).
	// https://openrouter.ai/api/v1/models?output_modalities=decisions
	"respan/span-02":                        `tier("base", p * 0.01)`,
	"liquid/d1-ultra":                       `tier("base", p * 0.042)`,
	"typesafe/jev-1.14":                     `tier("base", p * 0.042)`,
	"~perplexity/pplx-decider-latest":       `tier("base", p * 0.02)`,
	"scaledown/classify-1":                  `tier("base", p * 0.04)`,
	"nace-ai/drex-v2.0":                     `tier("base", p * 0.06)`,
	"microsoft/microsoft-decision-1":        `tier("base", p * 0.042)`,
	"nace-ai/drex-v1.5":                     `tier("base", p * 0.04)`,
	"cloudflare/clef-omni":                  `tier("base", p * 0.15)`,
	"upstage/solar-decide-flash":            `tier("base", p * 0.05)`,
	"perplexity/pplx-decider-v1.1-27b":      `tier("base", p * 0.02)`,
	"openai/gpt-6-luna-decisions":           `tier("base", p * 0.1)`,
	"liquid/d1":                             `tier("base", p * 0.02)`,
	"cloudflare/clef-flash":                 `tier("base", p * 0.021)`,
	"cloudflare/clef":                       `tier("base", p * 0.042)`,
	"togethercomputer/tev1-4b-experimental": `tier("base", p * 0.042)`,
	"inception/mercury-decide":              `tier("base", p * 0.02)`,
	"inception/mercury-decide:free":         `tier("base", p * 0)`,
	"upstage/solar-decide":                  `tier("base", p * 0.05)`,
	"respan/span-01":                        `tier("base", p * 0.02)`,
	"respan/span-01-lite":                   `tier("base", p * 0)`,
	"respan/span-01-lite:free":              `tier("base", p * 0)`,
	"jaredpalmer/kev-4b":                    `tier("base", p * 0.042)`,
	"~typesafe/jev-latest":                  `tier("base", p * 0.042)`,
	"typesafe/jev-1.13":                     `tier("base", p * 0.042)`,
	"jev-1.13":                              `tier("base", p * 0.042)`,
	"jev-1.14":                              `tier("base", p * 0.042)`,
	// https://api-docs.deepseek.com/quick_start/pricing (2026-10-17). Off-peak
	// is half the peak price. deepseek-v4-flash and deepseek-v4-flash-vision-exp
	// run on V4.1 Flash and bill at the Flash price. Images bill as input
	// tokens, and there is no cache-write price.
	"deepseek-flash":               deepSeekFlashExpr,
	"deepseek-v4-flash":            deepSeekFlashExpr,
	"deepseek-v4-flash-vision-exp": deepSeekFlashExpr,
	"deepseek-v4-pro":              deepSeekPeak + ` ? tier("peak", p * 1.32 + cr * 0.044 + c * 3.96) : tier("off_peak", p * 0.66 + cr * 0.022 + c * 1.98)`,
	// The Search and Fetch API routing names: $0.75 per 1,000 successful
	// requests each, the same pricing page. The calls carry no tokens.
	constant.DeepSeekSearchModel: `tier("request", fixed(0.00075))`,
	constant.DeepSeekFetchModel:  `tier("request", fixed(0.00075))`,
}
