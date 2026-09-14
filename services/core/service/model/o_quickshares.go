package model

// QuickShareDBModel is a single-file, URL-based share link: unlike
// SharesDBModel (SMB, folder-only, local-network-only, no expiry), this is
// meant to be handed to anyone as a short public URL - so ID is itself the
// unguessable capability token, never an auto-increment integer, and the
// redemption route it backs takes no other input (no raw path parameter).
type QuickShareDBModel struct {
	ID        string `gorm:"column:id;primary_key" json:"id"`
	Path      string `json:"path"`
	Name      string `json:"name"`
	// Unix seconds; 0 means "never expires".
	ExpiresAt int64 `json:"expires_at"`
	Created   int64 `gorm:"autoCreateTime" json:"created"`
}

func (p *QuickShareDBModel) TableName() string {
	return "o_quick_shares"
}
