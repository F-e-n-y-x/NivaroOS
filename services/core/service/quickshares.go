package service

import (
	"crypto/rand"
	"encoding/base32"
	"strings"
	"time"

	model2 "github.com/F-e-n-y-x/NivaroOS/services/core/service/model"
	"gorm.io/gorm"
)

type QuickShareService interface {
	CreateQuickShare(path string, name string, expiresIn time.Duration) (model2.QuickShareDBModel, error)
	GetQuickShareByID(id string) (share model2.QuickShareDBModel, found bool)
	GetQuickSharesList() (shares []model2.QuickShareDBModel)
	DeleteQuickShare(id string)
	PurgeExpiredQuickShares()
}

type quickShareStruct struct {
	db *gorm.DB
}

// 15 random bytes (120 bits) base32-encoded, lowercased, unpadded - short
// enough for a URL, never meant to be typed by hand so no need to avoid
// visually-ambiguous characters the way a human-entered code would.
func generateQuickShareToken() (string, error) {
	buf := make([]byte, 15)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return strings.ToLower(strings.TrimRight(base32.StdEncoding.EncodeToString(buf), "=")), nil
}

func (s *quickShareStruct) CreateQuickShare(path string, name string, expiresIn time.Duration) (model2.QuickShareDBModel, error) {
	token, err := generateQuickShareToken()
	if err != nil {
		return model2.QuickShareDBModel{}, err
	}
	var expiresAt int64 = 0
	if expiresIn > 0 {
		expiresAt = time.Now().Add(expiresIn).Unix()
	}
	share := model2.QuickShareDBModel{
		ID:        token,
		Path:      path,
		Name:      name,
		ExpiresAt: expiresAt,
	}
	if err := s.db.Create(&share).Error; err != nil {
		return model2.QuickShareDBModel{}, err
	}
	return share, nil
}

func (s *quickShareStruct) GetQuickShareByID(id string) (share model2.QuickShareDBModel, found bool) {
	result := s.db.Where("id = ?", id).First(&share)
	return share, result.Error == nil
}

func (s *quickShareStruct) GetQuickSharesList() (shares []model2.QuickShareDBModel) {
	s.db.Order("created desc").Find(&shares)
	return
}

func (s *quickShareStruct) DeleteQuickShare(id string) {
	s.db.Where("id = ?", id).Delete(&model2.QuickShareDBModel{})
}

func (s *quickShareStruct) PurgeExpiredQuickShares() {
	s.db.Where("expires_at > 0 AND expires_at < ?", time.Now().Unix()).Delete(&model2.QuickShareDBModel{})
}

func NewQuickShareService(db *gorm.DB) QuickShareService {
	return &quickShareStruct{db: db}
}
