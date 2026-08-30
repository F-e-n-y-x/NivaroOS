package main

import (
	interfaces "github.com/F-e-n-y-x/NivaroOS/services/common"
)

type migrationTool struct{}

func (u *migrationTool) IsMigrationNeeded() (bool, error) {
	return false, nil
}

func (u *migrationTool) PreMigrate() error {
	return nil
}

func (u *migrationTool) Migrate() error {
	return nil
}

func (u *migrationTool) PostMigrate() error {
	return nil
}

func NewMigrationDummy() interfaces.MigrationTool {
	return &migrationTool{}
}
