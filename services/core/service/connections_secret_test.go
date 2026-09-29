package service

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/secret"
	model2 "github.com/F-e-n-y-x/NivaroOS/services/core/service/model"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func connectionsDB(t *testing.T) (*gorm.DB, *connectionsStruct) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "casaOS.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model2.ConnectionsDBModel{}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		model2.SetSecretBox(nil)
		// Close it: other tests in this package check for leaked goroutines.
		if sqlDB, err := db.DB(); err == nil {
			sqlDB.Close()
		}
	})
	return db, &connectionsStruct{db: db}
}

func useKey(t *testing.T, path string) *secret.Box {
	t.Helper()
	b, err := secret.LoadOrCreate(path)
	if err != nil {
		t.Fatal(err)
	}
	model2.SetSecretBox(b)
	return b
}

// rawPassword is what is on disk, bypassing the model's hooks.
func rawPassword(t *testing.T, db *gorm.DB, id uint) string {
	t.Helper()
	var pw string
	if err := db.Raw("SELECT password FROM o_connections WHERE id = ?", id).Scan(&pw).Error; err != nil {
		t.Fatal(err)
	}
	return pw
}

func TestConnectionPasswordIsSealedAtRest(t *testing.T) {
	db, s := connectionsDB(t)
	useKey(t, filepath.Join(t.TempDir(), "secret.key"))

	c := model2.ConnectionsDBModel{Username: "nas", Password: "s3cret,pw", Host: "nas.example.com", Port: "445"}
	if err := s.CreateConnection(&c); err != nil {
		t.Fatal(err)
	}
	if c.ID == 0 {
		t.Fatal("not created")
	}
	if c.Password != "s3cret,pw" {
		t.Fatalf("caller's struct holds %q after save, want the plaintext back", c.Password)
	}
	onDisk := rawPassword(t, db, c.ID)
	if !secret.IsSealed(onDisk) || strings.Contains(onDisk, "s3cret") {
		t.Fatalf("stored password is not sealed: %q", onDisk)
	}

	// Every read path gives the plaintext.
	if got := s.GetConnectionByID("1").Password; got != "s3cret,pw" {
		t.Fatalf("GetConnectionByID password = %q", got)
	}
	if l := s.GetConnectionsList(); len(l) != 1 || l[0].Password != "s3cret,pw" {
		t.Fatalf("GetConnectionsList = %+v", l)
	}
	if l := s.GetConnectionByHost("nas.example.com"); len(l) != 1 || l[0].Password != "s3cret,pw" {
		t.Fatalf("GetConnectionByHost = %+v", l)
	}

	// An update (the boot remount saves the share list) keeps it sealed,
	// and doesn't seal the sealed value twice.
	got := s.GetConnectionByID("1")
	got.Directories = "media,backups"
	s.UpdateConnection(&got)
	if again := s.GetConnectionByID("1"); again.Password != "s3cret,pw" || again.Directories != "media,backups" {
		t.Fatalf("after update: %+v", again)
	}
	if !secret.IsSealed(rawPassword(t, db, c.ID)) {
		t.Fatal("update stored plaintext")
	}
}

func TestPlaintextRowsAreMigratedOnceAndStillWork(t *testing.T) {
	db, s := connectionsDB(t)
	// Rows saved by an older version: plaintext.
	db.Exec("INSERT INTO o_connections (id, username, password, host, port) VALUES (1, 'a', 'old-plain', 'h1', '445'), (2, 'b', '', 'h2', '445')")

	// Readable before the migration too (pass-through).
	if got := s.GetConnectionByID("1").Password; got != "old-plain" {
		t.Fatalf("legacy row reads %q", got)
	}

	useKey(t, filepath.Join(t.TempDir(), "secret.key"))
	n, err := s.SealStoredPasswords()
	if err != nil || n != 1 {
		t.Fatalf("SealStoredPasswords = %d, %v; want 1", n, err)
	}
	first := rawPassword(t, db, 1)
	if !secret.IsSealed(first) {
		t.Fatalf("row 1 still plaintext: %q", first)
	}
	if rawPassword(t, db, 2) != "" {
		t.Fatal("empty password was turned into something")
	}
	if got := s.GetConnectionByID("1").Password; got != "old-plain" {
		t.Fatalf("migrated row reads %q", got)
	}

	// Idempotent: a second start changes nothing.
	n, err = s.SealStoredPasswords()
	if err != nil || n != 0 {
		t.Fatalf("second run = %d, %v; want 0", n, err)
	}
	if rawPassword(t, db, 1) != first {
		t.Fatal("second run re-sealed the row")
	}
}

func TestNoKeyMeansNoPlaintextWrite(t *testing.T) {
	db, s := connectionsDB(t)
	model2.SetSecretBox(nil)
	c := model2.ConnectionsDBModel{Username: "nas", Password: "pw", Host: "h", Port: "445"}
	if err := s.CreateConnection(&c); err == nil {
		t.Fatal("CreateConnection without the key reported success")
	}
	var count int64
	db.Model(&model2.ConnectionsDBModel{}).Count(&count)
	if count != 0 {
		t.Fatalf("a connection was stored without the key (%d rows, password %q)", count, rawPassword(t, db, 1))
	}
}

func TestAnotherHostsKeyGivesNoPassword(t *testing.T) {
	_, s := connectionsDB(t)
	dir := t.TempDir()
	useKey(t, filepath.Join(dir, "a.key"))
	c := model2.ConnectionsDBModel{Username: "nas", Password: "pw", Host: "h", Port: "445"}
	s.CreateConnection(&c)
	useKey(t, filepath.Join(dir, "b.key")) // key replaced
	if got := s.GetConnectionByID("1").Password; got != "" {
		t.Fatalf("got %q; a password that can't be opened must read as empty, not ciphertext", got)
	}
}
