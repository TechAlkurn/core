package model

import (
	"app/pkg/core/db/activerecord"

	"gorm.io/gorm"
)

/**
* This is the model class for table "auth_assignment" By Ganesh.
**/

const TableNameAuthAssignment = "auth_assignment"

type AuthAssignment struct {
	activerecord.ActiveRecord[AuthAssignment]
	UserId    uint32  `gorm:"primaryKey;index:idx_user_id;column:user_id;type:int(10) unsigned;not null;" json:"user_id"`
	ItemName  string  `gorm:"primaryKey;column:item_name;type:varchar(64);not null;" json:"item_name"`
	CreatedAt *uint32 `gorm:"column:created_at;type:int(10) unsigned;default:null;autoCreateTime;" json:"created_at"`
}

func (m *AuthAssignment) TableName() string {
	return TableNameAuthAssignment
}

func init() {
	activerecord.RegisterModel[AuthAssignment]()
}

func (m *AuthAssignment) Set(field string, value any) error {
	return m.ActiveRecord.Set(m, field, value)
}

func (m *AuthAssignment) Get(field string) any { return m.ActiveRecord.Get(m, field) }

func (m *AuthAssignment) IsValid(key string, value any) bool {
	return m.ActiveRecord.IsValidField(m.Get(key), value)
}

func (m *AuthAssignment) GetCreatedAt() uint32 { return activerecord.Deref(m.CreatedAt) }

func (m *AuthAssignment) GetItemNameModel() (*AuthItem, error) {
	return activerecord.FindOneWhere[AuthItem]("`name` = ?", m.ItemName)
}

func (m *AuthAssignment) SaveDB(fields any, tx ...*gorm.DB) (*AuthAssignment, error) {
	return m.ActiveRecord.SaveDB(m, fields, tx...)
}

func (m *AuthAssignment) SlugifyDB(field, title string) (string, error) {
	return m.ActiveRecord.SlugifyDB(m, field, title)
}
