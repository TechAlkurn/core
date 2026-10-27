package model

import (
	"app/pkg/core/db/activerecord"

	"gorm.io/gorm"
)

/**
* This is the model class for table "auth_rule" By Ganesh.
**/

const TableNameAuthRule = "auth_rule"

type AuthRule struct {
	activerecord.ActiveRecord[AuthRule]
	Name      string  `gorm:"primaryKey;column:name;type:varchar(64);not null;" json:"name"`
	Data      *string `gorm:"column:data;type:blob;default:null;" json:"data"`
	CreatedAt uint32  `gorm:"column:created_at;type:int(11) unsigned;not null;autoCreateTime;" json:"created_at"`
	UpdatedAt uint32  `gorm:"column:updated_at;type:int(11) unsigned;not null;autoCreateTime;autoUpdateTime;" json:"updated_at"`
}

func (m *AuthRule) TableName() string {
	return TableNameAuthRule
}

func init() {
	activerecord.RegisterModel[AuthRule]()
}

func (m *AuthRule) Set(field string, value any) error {
	return m.ActiveRecord.Set(m, field, value)
}

func (m *AuthRule) Get(field string) any { return m.ActiveRecord.Get(m, field) }

func (m *AuthRule) IsValid(key string, value any) bool {
	return m.ActiveRecord.IsValidField(m.Get(key), value)
}

func (m *AuthRule) GetData() string { return activerecord.Deref(m.Data) }

func (m *AuthRule) GetAuthItemsModel() ([]*AuthItem, error) {
	return activerecord.FindAllWhere[AuthItem]("`rule_name` = ?", m.Name)
}

func (m *AuthRule) SaveDB(fields any, tx ...*gorm.DB) (*AuthRule, error) {
	return m.ActiveRecord.SaveDB(m, fields, tx...)
}

func (m *AuthRule) SlugifyDB(field, title string) (string, error) {
	return m.ActiveRecord.SlugifyDB(m, field, title)
}
