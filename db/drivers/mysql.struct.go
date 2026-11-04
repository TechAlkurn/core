package drivers

import "database/sql"

type DbSchema struct {
	TableName string
}

type TableSchema struct {
	Field   string
	Type    string
	Null    string
	Key     string
	Default sql.NullString
	Extra   string
}

type TableIndexesSchema struct {
	Name           string
	ColumnName     string
	IndexIsUnique  string
	IndexIsPrimary string
}

type TableReferencedConstraintsSchema struct {
	ConstraintName       string
	TableName            string
	ColumnName           string
	ReferencedTableName  string
	ReferencedColumnName string
}

type TableConstraintsSchema struct {
	Name               string
	ColumnName         string
	Type               string
	ForeignTableSchema string
	ForeignTableName   string
	ForeignColumnName  string
	OnUpdate           string
	OnDelete           string
	Position           string
}
type TableColumnsSchema struct {
	Field      string
	Type       string
	Collation  string
	Null       string
	Key        string
	Default    string
	Extra      string
	Privileges string
	Comment    string
}
