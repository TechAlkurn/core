package query

import (
	"context"
	"fmt"
	"strings"

	"github.com/TechAlkurn/core/config"

	"github.com/TechAlkurn/core/lib"
	"github.com/gin-gonic/gin"
)

type dBQuery struct {
	query string
}
type DBQuery interface {
	Query() (any, error)
}

func NewDBQuery(q any) DBQuery {
	query := strings.TrimSpace(lib.ToString(q))
	return dBQuery{
		query: query,
	}
}

func (q dBQuery) Query() (any, error) {
	sqlQuery := strings.TrimSpace(strings.ToLower(lib.ToString(q.query)))
	ctx := context.Background()

	// Use SELECT for QueryContext
	if strings.HasPrefix(sqlQuery, "select") {
		if !strings.Contains(sqlQuery, "limit") {
			sqlQuery = strings.TrimSuffix(sqlQuery, ";") // remove any trailing semicolon
			sqlQuery += " LIMIT 50"
		}
		rows, err := config.DB.ConnPool.QueryContext(ctx, sqlQuery)
		if err != nil {
			return nil, fmt.Errorf("error executing SELECT query: %v", err)
		}
		defer rows.Close()
		var results []map[string]any
		columns, err := rows.Columns()
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			values := make([]any, len(columns))
			valuePtrs := make([]any, len(columns))
			for i := range values {
				valuePtrs[i] = &values[i]
			}

			if err := rows.Scan(valuePtrs...); err != nil {
				return nil, err
			}

			row := make(map[string]any)
			for i, col := range columns {
				val := values[i]
				// Convert []uint8 (aka []byte) to string if needed
				if b, ok := val.([]byte); ok {
					row[col] = string(b)
				} else {
					row[col] = val
				}
			}
			results = append(results, row)
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}
		if lib.IsEmpty(results) {
			return gin.H{"message": "No rows found"}, nil
		}
		return results, nil
	}

	// For UPDATE, DELETE, INSERT: use ExecContext
	result, err := config.DB.ConnPool.ExecContext(ctx, sqlQuery)
	if err != nil {
		return nil, fmt.Errorf("error executing query: %v", err)
	}
	affected, _ := result.RowsAffected()
	return gin.H{
		"query":   sqlQuery,
		"message": fmt.Sprintf("%d row(s) affected", affected),
	}, nil
}
