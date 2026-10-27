package model

import (
	"app/pkg/core/db/activerecord"

	"gorm.io/gorm"
)

/**
* This is the model class for table "auth_item_child" By Ganesh.
**/

const TableNameAuthItemChild = "auth_item_child"

type AuthItemChild struct {
	activerecord.ActiveRecord[AuthItemChild]
	Parent string `gorm:"primaryKey;column:parent;type:varchar(64);not null;" json:"parent"`
	Child  string `gorm:"primaryKey;index:idx_child;column:child;type:varchar(64);not null;" json:"child"`
}

func (m *AuthItemChild) TableName() string {
	return TableNameAuthItemChild
}

func init() {
	activerecord.RegisterModel[AuthItemChild]()
}

func (m *AuthItemChild) Set(field string, value any) error {
	return m.ActiveRecord.Set(m, field, value)
}

func (m *AuthItemChild) Get(field string) any { return m.ActiveRecord.Get(m, field) }

func (m *AuthItemChild) IsValid(key string, value any) bool {
	return m.ActiveRecord.IsValidField(m.Get(key), value)
}

func (m *AuthItemChild) GetChild0Model() (*AuthItem, error) {
	return activerecord.FindOneWhere[AuthItem]("`name` = ?", m.Child)
}

func (m *AuthItemChild) GetParent0Model() (*AuthItem, error) {
	return activerecord.FindOneWhere[AuthItem]("`name` = ?", m.Parent)
}

func (m *AuthItemChild) SaveDB(fields any, tx ...*gorm.DB) (*AuthItemChild, error) {
	return m.ActiveRecord.SaveDB(m, fields, tx...)
}

func (m *AuthItemChild) SlugifyDB(field, title string) (string, error) {
	return m.ActiveRecord.SlugifyDB(m, field, title)
}
