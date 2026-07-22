package model

import (
	"errors"
	"sort"

	"gorm.io/gorm"
)

const (
	TokenRouteKindGroup   = "group"
	TokenRouteKindChannel = "channel"
)

// TokenRouteRule stores an ordered, user-owned route candidate for a token.
// A group rule delegates channel selection to the normal priority/weight logic;
// a channel rule pins the candidate to one channel ID.
type TokenRouteRule struct {
	ID        int    `json:"id"`
	TokenID   int    `json:"token_id" gorm:"index;uniqueIndex:idx_token_route_position,priority:1"`
	Position  int    `json:"position" gorm:"uniqueIndex:idx_token_route_position,priority:2"`
	Kind      string `json:"kind" gorm:"type:varchar(16)"`
	GroupName string `json:"group,omitempty" gorm:"type:varchar(64)"`
	ChannelID int    `json:"channel_id,omitempty"`
	Enabled   bool   `json:"enabled"`
}

func GetTokenRouteRules(tokenID int) ([]TokenRouteRule, error) {
	if tokenID <= 0 {
		return nil, errors.New("token id 无效")
	}
	var rules []TokenRouteRule
	err := DB.Where("token_id = ? AND enabled = ?", tokenID, true).
		Order("position ASC, id ASC").Find(&rules).Error
	return rules, err
}

// ReplaceTokenRouteRules atomically replaces the complete ordered rule list.
func ReplaceTokenRouteRules(tokenID int, rules []TokenRouteRule) error {
	if tokenID <= 0 {
		return errors.New("token id 无效")
	}
	ordered := append([]TokenRouteRule(nil), rules...)
	sort.SliceStable(ordered, func(i, j int) bool {
		return ordered[i].Position < ordered[j].Position
	})

	return DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("token_id = ?", tokenID).Delete(&TokenRouteRule{}).Error; err != nil {
			return err
		}
		for index := range ordered {
			ordered[index].ID = 0
			ordered[index].TokenID = tokenID
			ordered[index].Position = index
			ordered[index].Enabled = true
			if err := tx.Create(&ordered[index]).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func DeleteTokenRouteRules(tokenID int) error {
	if tokenID <= 0 {
		return errors.New("token id 无效")
	}
	return DB.Where("token_id = ?", tokenID).Delete(&TokenRouteRule{}).Error
}
