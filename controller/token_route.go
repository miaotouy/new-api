package controller

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

type tokenRouteRuleInput struct {
	Kind      string `json:"kind"`
	GroupName string `json:"group"`
	ChannelID int    `json:"channel_id"`
}

type tokenRouteRulesInput struct {
	Items []tokenRouteRuleInput `json:"items"`
}

type tokenRouteChannelOption struct {
	ID           int     `json:"id"`
	Name         string  `json:"name"`
	Group        string  `json:"group"`
	Status       int     `json:"status"`
	Models       string  `json:"models"`
	Priority     int64   `json:"priority"`
	Weight       int     `json:"weight"`
	ResponseTime int     `json:"response_time"`
	GroupRatio   float64 `json:"group_ratio"`
}

func GetTokenRoutes(c *gin.Context) {
	token, err := getOwnedTokenForRoute(c)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	rules, err := model.GetTokenRouteRules(token.Id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": rules})
}

func UpdateTokenRoutes(c *gin.Context) {
	token, err := getOwnedTokenForRoute(c)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	var input tokenRouteRulesInput
	if err := c.ShouldBindJSON(&input); err != nil {
		common.ApiError(c, err)
		return
	}
	if len(input.Items) > 128 {
		common.ApiError(c, fmt.Errorf("路由规则不能超过 128 条"))
		return
	}

	userGroup, err := model.GetUserGroup(c.GetInt("id"), false)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	usableGroups := service.GetUserUsableGroups(userGroup)
	rules := make([]model.TokenRouteRule, 0, len(input.Items))
	seen := make(map[string]struct{}, len(input.Items))
	for index, item := range input.Items {
		kind := strings.TrimSpace(item.Kind)
		if kind != model.TokenRouteKindGroup && kind != model.TokenRouteKindChannel {
			common.ApiError(c, fmt.Errorf("第 %d 条路由规则类型无效", index+1))
			return
		}
		rule := model.TokenRouteRule{Position: index, Kind: kind, Enabled: true}
		if kind == model.TokenRouteKindGroup {
			groupName := strings.TrimSpace(item.GroupName)
			if groupName == "" || groupName == "auto" {
				common.ApiError(c, fmt.Errorf("第 %d 条分组路由无效", index+1))
				return
			}
			if _, ok := usableGroups[groupName]; !ok {
				common.ApiError(c, fmt.Errorf("无权使用分组 %s", groupName))
				return
			}
			rule.GroupName = groupName
		} else {
			if item.ChannelID <= 0 {
				common.ApiError(c, fmt.Errorf("第 %d 条渠道路由无效", index+1))
				return
			}
			channel, err := model.GetChannelById(item.ChannelID, false)
			if err != nil {
				common.ApiError(c, fmt.Errorf("渠道 #%d 不存在", item.ChannelID))
				return
			}
			if channel.Group == "" || channel.Group == "auto" {
				common.ApiError(c, fmt.Errorf("渠道 #%d 没有有效分组", item.ChannelID))
				return
			}
			channelGroup := ""
			for _, candidateGroup := range strings.Split(channel.Group, ",") {
				candidateGroup = strings.TrimSpace(candidateGroup)
				if _, ok := usableGroups[candidateGroup]; ok && candidateGroup != "auto" {
					channelGroup = candidateGroup
					break
				}
			}
			if channelGroup == "" {
				common.ApiError(c, fmt.Errorf("无权使用渠道 #%d 所属分组", item.ChannelID))
				return
			}
			rule.GroupName = channelGroup
			rule.ChannelID = item.ChannelID
		}
		key := fmt.Sprintf("%s:%s:%d", rule.Kind, rule.GroupName, rule.ChannelID)
		if _, ok := seen[key]; ok {
			common.ApiError(c, fmt.Errorf("路由规则不能重复"))
			return
		}
		seen[key] = struct{}{}
		rules = append(rules, rule)
	}

	if err := model.ReplaceTokenRouteRules(token.Id, rules); err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": rules})
}

func GetTokenRouteOptions(c *gin.Context) {
	if _, err := getOwnedTokenForRoute(c); err != nil {
		common.ApiError(c, err)
		return
	}
	userGroup, err := model.GetUserGroup(c.GetInt("id"), false)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	usableGroups := service.GetUserUsableGroups(userGroup)
	channels, err := model.GetChannelRouteOptions()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	options := make([]tokenRouteChannelOption, 0, len(channels))
	for _, channel := range channels {
		if channel == nil {
			continue
		}
		for _, group := range strings.Split(channel.Group, ",") {
			group = strings.TrimSpace(group)
			if group == "auto" {
				continue
			}
			if _, ok := usableGroups[group]; !ok {
				continue
			}
			options = append(options, tokenRouteChannelOption{
				ID:           channel.Id,
				Name:         channel.Name,
				Group:        group,
				Status:       channel.Status,
				Models:       channel.Models,
				Priority:     channel.GetPriority(),
				Weight:       channel.GetWeight(),
				ResponseTime: channel.ResponseTime,
				GroupRatio:   service.GetUserGroupRatio(userGroup, group),
			})
		}
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": options})
}

func getOwnedTokenForRoute(c *gin.Context) (*model.Token, error) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		return nil, fmt.Errorf("token id 无效")
	}
	return model.GetTokenByIds(id, c.GetInt("id"))
}
