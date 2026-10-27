package model

import (
	"app/pkg/core/db/activerecord"

	"gorm.io/gorm"
)

/**
* This is the model class for table "auth_item" By Ganesh.
**/

const TableNameAuthItem = "auth_item"

type AuthItem struct {
	activerecord.ActiveRecord[AuthItem]
	Name        string  `gorm:"primaryKey;column:name;type:varchar(64);not null;" json:"name"`
	Type        int     `gorm:"index:idx_type;column:type;type:smallint(6);not null;" json:"type"`
	Description *string `gorm:"column:description;type:text;default:null;" json:"description"`
	RuleName    *string `gorm:"index:idx_rule_name;column:rule_name;type:varchar(64);default:null;" json:"rule_name"`
	Data        *string `gorm:"column:data;type:blob;default:null;" json:"data"`
	CreatedAt   uint32  `gorm:"column:created_at;type:int(11) unsigned;not null;autoCreateTime;" json:"created_at"`
	UpdatedAt   uint32  `gorm:"column:updated_at;type:int(11) unsigned;not null;autoCreateTime;autoUpdateTime;" json:"updated_at"`
}

func (m *AuthItem) TableName() string {
	return TableNameAuthItem
}

func init() {
	activerecord.RegisterModel[AuthItem]()
}

func (m *AuthItem) Set(field string, value any) error {
	return m.ActiveRecord.Set(m, field, value)
}

func (m *AuthItem) Get(field string) any { return m.ActiveRecord.Get(m, field) }

func (m *AuthItem) IsValid(key string, value any) bool {
	return m.ActiveRecord.IsValidField(m.Get(key), value)
}

func (m *AuthItem) GetDescription() string { return activerecord.Deref(m.Description) }

func (m *AuthItem) GetRuleName() string { return activerecord.Deref(m.RuleName) }

func (m *AuthItem) GetData() string { return activerecord.Deref(m.Data) }

func (m *AuthItem) GetAuthAssignmentsModel() ([]*AuthAssignment, error) {
	return activerecord.FindAllWhere[AuthAssignment]("`item_name` = ?", m.Name)
}

func (m *AuthItem) GetRuleNameModel() (*AuthRule, error) {
	return activerecord.FindOneWhere[AuthRule]("`name` = ?", m.RuleName)
}

func (m *AuthItem) GetAuthItemChildrenModel() ([]*AuthItemChild, error) {
	return activerecord.FindAllWhere[AuthItemChild]("`child` = ?", m.Name)
}

func (m *AuthItem) GetAuthItemChildren0Model() ([]*AuthItemChild, error) {
	return activerecord.FindAllWhere[AuthItemChild]("`parent` = ?", m.Name)
}

func (m *AuthItem) GetParentsModel() ([]*AuthItem, error) {
	return activerecord.FindAllJoin[AuthItem]("join `auth_item_child` on `auth_item_child`.`parent` = `auth_item`.`name`", "`child` = ?", m.Name)
}

func (m *AuthItem) GetChildrenModel() ([]*AuthItem, error) {
	return activerecord.FindAllJoin[AuthItem]("join `auth_item_child` on `auth_item_child`.`child` = `auth_item`.`name`", "`parent` = ?", m.Name)
}

func (m *AuthItem) SaveDB(fields any, tx ...*gorm.DB) (*AuthItem, error) {
	return m.ActiveRecord.SaveDB(m, fields, tx...)
}

func (m *AuthItem) SlugifyDB(field, title string) (string, error) {
	return m.ActiveRecord.SlugifyDB(m, field, title)
}
