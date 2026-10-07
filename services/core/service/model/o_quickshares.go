package model

// QuickShareDBModel is a URL-based share link for one file or folder (a
// folder is served as a ZIP): unlike SharesDBModel (SMB, folder-only,
// local-network-only, no expiry), this is meant to be handed to anyone as
// a short public URL - so ID is itself the unguessable capability token,
// never an auto-increment integer, and the redemption route it backs takes
// no other input (no raw path parameter).
type QuickShareDBModel struct {
	ID   string `gorm:"column:id;primary_key" json:"id"`
	Path string `json:"path"`
	Name string `json:"name"`
	// Unix seconds; 0 means "never expires".
	ExpiresAt int64 `json:"expires_at"`
	Created   int64 `gorm:"autoCreateTime" json:"created"`
	// 0 = unlimited; 1 = a one-time link. The share is removed once
	// Downloads reaches it.
	MaxDownloads int `gorm:"not null;default:0" json:"max_downloads"`
	Downloads    int `gorm:"not null;default:0" json:"downloads"`
	// bcrypt of the link's password; empty = no password.
	PasswordHash string `gorm:"not null;default:''" json:"-"`
}

func (p *QuickShareDBModel) TableName() string {
	return "o_quick_shares"
}
