package activerecord

import "testing"

// noTableName embeds ActiveRecord but declares no TableName, so the promoted
// method is the only one: this used to recurse until the stack overflowed.
type noTableName struct {
	ActiveRecord[noTableName]
	Id uint32
}

type noTableNamePtr struct {
	*ActiveRecord[noTableNamePtr]
	Id uint32
}

type withTableName struct {
	ActiveRecord[withTableName]
	Id uint32
}

func (m *withTableName) TableName() string { return "with_table_name" }

func TestTableNameNoRecursion(t *testing.T) {
	if got := (&noTableName{}).TableName(); got != "notablename" {
		t.Fatalf("value embed: got %q", got)
	}
	if got := (&ActiveRecord[noTableNamePtr]{}).TableName(); got != "notablenameptr" {
		t.Fatalf("pointer embed: got %q", got)
	}
	if got := (&ActiveRecord[withTableName]{}).TableName(); got != "with_table_name" {
		t.Fatalf("custom: got %q", got)
	}
}
