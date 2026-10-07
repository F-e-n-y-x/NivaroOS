/*
 * @Author: LinkLeong link@icewhale.com
 * @Date: 2022-05-13 18:15:46
 * @LastEditors: LinkLeong
 * @LastEditTime: 2022-07-11 17:57:00
 * @Description:
 * Copyright (c) 2022 by icewhale, All Rights Reserved.
 */
package model

import "time"

// Soon to be removed
type UserDBModel struct {
	Id       int    `gorm:"column:id;primary_key" json:"id"`
	Username string `json:"username"`
	Password string `json:"password,omitempty"`
	Role     string `json:"role"`
	Email    string `json:"email"`
	Nickname string `json:"nickname"`
	Avatar   string `json:"avatar"`
	// AvatarVersion: hash of the stored picture, "" when none is set.
	// Clients show the initials fallback for "" and add it as ?v= to the
	// avatar URL, so a new picture is a new (long-cacheable) URL.
	AvatarVersion string `gorm:"column:avatar_version" json:"avatar_version"`
	Description   string `json:"description"`
	// Sessions (tokens) issued before this unix time are no longer valid:
	// set when the password changes.
	TokensValidAfter int64 `gorm:"column:tokens_valid_after" json:"-"`
	// TokenGeneration: every token of this account carries it; revoking
	// all its sessions (password change, sign out everywhere) increments
	// it. Published to every service (service/sessions.go).
	TokenGeneration int64 `gorm:"column:token_generation;not null;default:0" json:"-"`
	// TokenRevokeReason: why the generation last changed (for the 401).
	TokenRevokeReason string    `gorm:"column:token_revoke_reason" json:"-"`
	CreatedAt         time.Time `gorm:"<-:create;autoCreateTime" json:"created_at,omitempty"`
	UpdatedAt         time.Time `gorm:"<-:create;<-:update;autoUpdateTime" json:"updated_at,omitempty"`
}

func (p *UserDBModel) TableName() string {
	return "o_users"
}
