package service

import (
	"path/filepath"
	"testing"

	model2 "github.com/F-e-n-y-x/NivaroOS/services/core/service/model"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// A share made before one-time links and passwords existed keeps working
// after the columns are added: unlimited, no password.
func TestQuickShareMigratesOldRows(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "casaOS.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			sqlDB.Close()
		}
	})
	db.Exec(`CREATE TABLE o_quick_shares (id text PRIMARY KEY, path text, name text, expires_at integer, created integer)`)
	db.Exec(`INSERT INTO o_quick_shares VALUES ('old', '/DATA/a.txt', 'a.txt', 0, 1)`)
	if err := db.AutoMigrate(&model2.QuickShareDBModel{}); err != nil {
		t.Fatal(err)
	}
	s := NewQuickShareService(db)
	share, ok := s.GetQuickShareByID("old")
	if !ok || share.MaxDownloads != 0 || share.PasswordHash != "" || !CheckQuickSharePassword(share, "") {
		t.Fatalf("old share = %+v, %v", share, ok)
	}
	for i := 0; i < 3; i++ {
		if !s.ClaimQuickShareDownload("old") {
			t.Fatalf("claim %d refused", i)
		}
	}

	one, err := s.CreateQuickShare("/DATA/b.txt", "b.txt", 0, 1, "pw")
	if err != nil {
		t.Fatal(err)
	}
	if CheckQuickSharePassword(one, "nope") || !CheckQuickSharePassword(one, "pw") {
		t.Fatal("password check")
	}
	if !s.ClaimQuickShareDownload(one.ID) || s.ClaimQuickShareDownload(one.ID) {
		t.Fatal("one-time link claimed twice")
	}
	if _, ok := s.GetQuickShareByID(one.ID); ok {
		t.Fatal("used-up share still there")
	}
}
