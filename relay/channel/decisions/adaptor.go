package decisions

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/relay/channel/openai"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
)

// Adaptor shares the gateway HTTP transport while retaining native decision payloads.
type Adaptor struct{ openai.Adaptor }

func (a *Adaptor) GetRequestURL(info *relaycommon.RelayInfo) (string, error) {
	base := strings.TrimRight(info.ChannelBaseUrl, "/")
	base = strings.TrimSuffix(base, "/v1")
	path := "/v1/systemone"
	switch info.ChannelType {
	case constant.ChannelTypeOpenRouter:
		path = "/alpha/decisions"
	case constant.ChannelTypeNewAPI:
		path = "/v1/decisions"
	case constant.ChannelTypeTaskPlugin:
		if base == "" {
			base = "https://api.typesafe.ai"
		}
	}
	return base + path, nil
}

func (a *Adaptor) ConvertDecisionsRequest(info *relaycommon.RelayInfo, request *dto.DecisionsRequest) ([]byte, error) {
	var body map[string]json.RawMessage
	if err := common.Unmarshal(request.RawBody, &body); err != nil {
		return nil, err
	}
	model := request.Model
	if info.ChannelType == constant.ChannelTypeOpenRouter {
		switch {
		case model == "jev-latest":
			model = "~typesafe/jev-latest"
		case model == "jev-1.13.0":
			model = "typesafe/jev-1.13"
		case strings.HasPrefix(model, "jev-"):
			model = "typesafe/" + model
		}
	}
	info.UpstreamModelName = model
	encodedModel, err := common.Marshal(model)
	if err != nil {
		return nil, err
	}
	body["model"] = encodedModel
	return common.Marshal(body)
}

func (a *Adaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, body io.Reader) (any, error) {
	return channel.DoApiRequest(a, c, info, body)
}

func (a *Adaptor) SetupRequestHeader(c *gin.Context, headers *http.Header, info *relaycommon.RelayInfo) error {
	if err := a.Adaptor.SetupRequestHeader(c, headers, info); err != nil {
		return err
	}
	headers.Set("Content-Type", "application/json")
	headers.Set("Accept", "application/json")
	return nil
}
