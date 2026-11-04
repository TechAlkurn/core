package drivers

import (
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type Mysql interface {
	Connection() *gorm.DB
	DB() *gorm.DB
	GetTableNames() (schema []DbSchema, err error)
	Charset() string
	DriverName() string
	PrimaryKey(tbl string) (schema []TableIndexesSchema, err error)
	GetTableSchema(tbl string) (schema []TableSchema, err error)
	FindConstraints() (schema []TableReferencedConstraintsSchema, err error)
	LoadTableConstraints() (schema []TableConstraintsSchema, err error)
	LoadTableIndexes() (schema []TableIndexesSchema, err error)
	FindColumns() (schema []TableColumnsSchema, err error)
}

type mysqld struct {
	db        *gorm.DB
	conn      string
	dbName    string
	tableName string
}

func getEnv(key string) string {
	val := strings.TrimSpace(os.Getenv(key))
	if val == "" {
		log.Fatalf("environment variable %s missing", key)
	}
	return val
}

// NewRepository creates a new repository
func NewMysql() Mysql {
	name := getEnv("DB_NAME")
	dsn := getEnv("DB_URL")
	m := &mysqld{conn: dsn, dbName: name}
	m.db = m.Connection()
	return m
}

func (drv *mysqld) DB() *gorm.DB {
	return drv.db
}

func (drv *mysqld) Connection() *gorm.DB {
	loggerConfig := logger.New(
		log.New(os.Stdout, "\r\n", log.LstdFlags), // io writer
		logger.Config{
			SlowThreshold: time.Second,   // Slow SQL threshold
			LogLevel:      logger.Silent, // Log level
			// LogLevel:                  logger.Info, // Log level
			IgnoreRecordNotFoundError: true,  // Ignore ErrRecordNotFound error for logger
			ParameterizedQueries:      false, // Don't include params in the SQL log
			Colorful:                  false, // Disable color
		},
	)
	var db *gorm.DB
	var err error
	for i := range 3 { // retry 3 times
		db, err = gorm.Open(mysql.Open(drv.conn), &gorm.Config{Logger: loggerConfig})
		if err == nil {
			break
		}
		log.Printf("Error connecting to database: %v. Retrying %d/3...", err, i+1)
		time.Sleep(2 * time.Second) // wait before retrying
	}
	if err != nil {
		log.Fatalf("Error connecting to database after retries: %v", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		log.Fatalf("Error connecting to database: %v", err)
	}
	sqlDB.SetMaxOpenConns(200)                // Max open connections (tune based on load)
	sqlDB.SetMaxIdleConns(50)                 // Max idle connections (tune based on load)
	sqlDB.SetConnMaxIdleTime(5 * time.Minute) // Idle timeout
	sqlDB.SetConnMaxLifetime(1 * time.Hour)   // Connection lifetime (consider reducing to 30 minutes for more aggressive connection closing)
	return db
}

func (drv *mysqld) Charset() string {
	return ""
}

func (drv *mysqld) DriverName() string {
	return ""
}

func (drv *mysqld) GetTableNames() (schema []DbSchema, err error) {
	query := fmt.Sprintf(`SELECT table_name FROM information_schema.tables WHERE table_schema = '%s'`, drv.dbName)
	err = drv.db.Raw(query).Scan(&schema).Error
	return
}

func (drv *mysqld) PrimaryKey(tbl string) (schema []TableIndexesSchema, err error) {
	query := fmt.Sprintf(`SHOW INDEX FROM %s WHERE Key_name = 'PRIMARY'`, tbl)
	err = drv.db.Raw(query).Scan(&schema).Error
	return
}

func (drv *mysqld) GetTableSchema(tbl string) (schema []TableSchema, err error) {
	// err = drv.db.Raw(fmt.Sprintf(" SHOW CREATE TABLE `%s`", tableName)).Scan(&schema).Error
	err = drv.db.Raw(fmt.Sprintf(" DESC `%s`", drv.tableName)).Scan(&schema).Error
	// log.Printf("%+v\n", schema)
	return
}

func (drv *mysqld) FindColumns() (schema []TableColumnsSchema, err error) {
	query := fmt.Sprintf(`SHOW FULL COLUMNS FROM %s`, drv.tableName)
	err = drv.db.Raw(query).Scan(&schema).Error
	if err != nil {
		return nil, err
	}
	err = drv.db.Raw(query).Scan(&schema).Error
	if err != nil {
		return nil, err
	}
	/* for _, row := range schema {
		 fmt.Printf("Constraint Field: %s, Type: %s, Collation: %s, Null: %s Key : %s Default : %s Extra : %s Privileges : %s Comment : %s \n", row.Field, row.Type, row.Collation, row.Null, row.Key, row.Default, row.Extra, row.Privileges, row.Comment)
		log.Printf("%+v\n", row)
	} */
	log.Printf("%+v\n", schema)
	return
}

func (drv *mysqld) FindConstraints() (schema []TableReferencedConstraintsSchema, err error) {
	query := fmt.Sprintf(`SELECT
    kcu.CONSTRAINT_NAME AS constraint_name,
	kcu.TABLE_NAME AS table_name,
    kcu.COLUMN_NAME AS column_name,
    kcu.REFERENCED_TABLE_NAME AS referenced_table_name,
    kcu.REFERENCED_COLUMN_NAME AS referenced_column_name
FROM information_schema.REFERENTIAL_CONSTRAINTS AS rc
JOIN information_schema.KEY_COLUMN_USAGE AS kcu ON
    (
        kcu.CONSTRAINT_CATALOG = rc.CONSTRAINT_CATALOG OR
        (kcu.CONSTRAINT_CATALOG IS NULL AND rc.CONSTRAINT_CATALOG IS NULL)
    ) AND
    kcu.CONSTRAINT_SCHEMA = rc.CONSTRAINT_SCHEMA AND
    kcu.CONSTRAINT_NAME = rc.CONSTRAINT_NAME
WHERE rc.CONSTRAINT_SCHEMA = database() AND kcu.TABLE_SCHEMA = database()
AND rc.TABLE_NAME = '%s' AND kcu.TABLE_NAME = '%s';
    `, drv.tableName, drv.tableName)

	err = drv.db.Raw(query).Scan(&schema).Error
	if err != nil {
		return nil, err
	}
	/* for _, row := range schema {
		  fmt.Printf("Constraint Name: %s, Table: %s, Column: %s, Referenced Table: %s, Referenced Column: %s\n", row.ConstraintName, row.TableName, row.ColumnName, row.ReferencedTableName, row.ReferencedColumnName)
		log.Printf("%+v\n", row)
	} */
	log.Printf("%+v\n", schema)
	return schema, nil
}

func (drv *mysqld) LoadTableConstraints() (schema []TableConstraintsSchema, err error) {
	query := fmt.Sprintf(`SELECT
  kcu.CONSTRAINT_NAME AS name,
  kcu.COLUMN_NAME AS column_name,
  tc.CONSTRAINT_TYPE AS type,
  CASE
	  WHEN '%s' IS NULL AND kcu.REFERENCED_TABLE_SCHEMA = DATABASE() THEN NULL
	  ELSE kcu.REFERENCED_TABLE_SCHEMA
  END AS foreign_table_schema,
  kcu.REFERENCED_TABLE_NAME AS foreign_table_name,
  kcu.REFERENCED_COLUMN_NAME AS foreign_column_name,
  rc.UPDATE_RULE AS on_update,
  rc.DELETE_RULE AS on_delete,
  kcu.ORDINAL_POSITION AS position
FROM
  information_schema.KEY_COLUMN_USAGE AS kcu,
  information_schema.REFERENTIAL_CONSTRAINTS AS rc,
  information_schema.TABLE_CONSTRAINTS AS tc
WHERE
  kcu.TABLE_SCHEMA = COALESCE('%s', DATABASE()) AND kcu.CONSTRAINT_SCHEMA = kcu.TABLE_SCHEMA AND kcu.TABLE_NAME = '%s'
  AND rc.CONSTRAINT_SCHEMA = kcu.TABLE_SCHEMA AND rc.TABLE_NAME = '%s' AND rc.CONSTRAINT_NAME = kcu.CONSTRAINT_NAME
  AND tc.TABLE_SCHEMA = kcu.TABLE_SCHEMA AND tc.TABLE_NAME = '%s' AND tc.CONSTRAINT_NAME = kcu.CONSTRAINT_NAME AND tc.CONSTRAINT_TYPE = 'FOREIGN KEY'
UNION
SELECT
  kcu.CONSTRAINT_NAME AS name,
  kcu.COLUMN_NAME AS column_name,
  tc.CONSTRAINT_TYPE AS type,
  NULL AS foreign_table_schema,
  NULL AS foreign_table_name,
  NULL AS foreign_column_name,
  NULL AS on_update,
  NULL AS on_delete,
  kcu.ORDINAL_POSITION AS position
FROM
  information_schema.KEY_COLUMN_USAGE AS kcu,
  information_schema.TABLE_CONSTRAINTS AS tc
WHERE
  kcu.TABLE_SCHEMA = COALESCE('%s', DATABASE()) AND kcu.TABLE_NAME = '%s'
  AND tc.TABLE_SCHEMA = kcu.TABLE_SCHEMA AND tc.TABLE_NAME = '%s' AND tc.CONSTRAINT_NAME = kcu.CONSTRAINT_NAME AND tc.CONSTRAINT_TYPE IN ('PRIMARY KEY', 'UNIQUE')
ORDER BY position ASC`, drv.dbName, drv.dbName, drv.tableName, drv.tableName, drv.tableName, drv.dbName, drv.tableName, drv.tableName)

	err = drv.db.Raw(query).Scan(&schema).Error
	if err != nil {
		return nil, err
	}
	/*
		for _, row := range schema {
			fmt.Printf("Constraint Name: %s, ColumnName: %s, Type: %s, ForeignTableSchema: %s, ForeignTableName: %s, ForeignColumnName: %s, OnUpdate: %s, OnDelete: %s, Position: %s, \n", row.Name, row.ColumnName, row.Type, row.ForeignTableSchema, row.ForeignTableName, row.ForeignColumnName, row.OnUpdate, row.OnDelete, row.Position)
			log.Printf("%+v\n", row)
		} */

	// log.Printf("%+v\n", schema)
	return schema, nil
}

func (drv *mysqld) LoadTableIndexes() (schema []TableIndexesSchema, err error) {
	query := fmt.Sprintf(`SELECT
	s.INDEX_NAME AS name,
	s.COLUMN_NAME AS column_name,
	s.NON_UNIQUE ^ 1 AS index_is_unique,
	s.INDEX_NAME = 'PRIMARY' AS index_is_primary
  FROM
	information_schema.STATISTICS AS s
  WHERE s.TABLE_SCHEMA = COALESCE ('%s', DATABASE ())
	AND s.INDEX_SCHEMA = s.TABLE_SCHEMA
	AND s.TABLE_NAME = '%s'
  ORDER BY s.SEQ_IN_INDEX ASC`, drv.dbName, drv.tableName)

	err = drv.db.Raw(query).Scan(&schema).Error
	if err != nil {
		return nil, err
	}
	/* for _, row := range schema {
		fmt.Printf("Constraint Name: %s, Column: %s, IndexIsUnique: %s, IndexIsPrimary: %s\n", row.Name, row.ColumnName, row.IndexIsUnique, row.IndexIsPrimary)
		log.Printf("%+v\n", row)
	} */
	//	log.Printf("%+v\n", schema)
	return
}
