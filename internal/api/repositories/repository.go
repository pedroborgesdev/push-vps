package repositories

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"

	"github.com/pedroborgesdev/papoql/internal/api/database"
)

type Repository struct {
	DB *database.Database
}

func NewRepository(db *database.Database) *Repository {
	return &Repository{DB: db}
}

func (r *Repository) SchemaPOST() error {
	outFile := "./.papoql/schema.txt"

	db := r.DB.DB

	defer db.Close()

	file, err := os.Create(outFile)
	if err != nil {
		return err
	}
	defer file.Close()

	rows, err := db.Query(`
		SELECT name FROM sqlite_master
		WHERE type='table' AND name NOT LIKE 'sqlite_%'
		ORDER BY name
	`)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var tableName string
		if err := rows.Scan(&tableName); err != nil {
			return err
		}

		fmt.Fprintf(file, "TABLE %s\n", tableName)

		colRows, err := db.Query(fmt.Sprintf("PRAGMA table_info('%s')", tableName))
		if err != nil {
			return err
		}

		for colRows.Next() {
			var (
				cid       int
				name      string
				colType   string
				notnull   int
				dfltValue sql.NullString
				pk        int
			)

			if err := colRows.Scan(&cid, &name, &colType, &notnull, &dfltValue, &pk); err != nil {
				return err
			}

			pkLabel := ""
			if pk == 1 {
				pkLabel = " PK"
			}

			fmt.Fprintf(file, "  COL %s %s%s\n", name, colType, pkLabel)
		}
		colRows.Close()

		fkRows, err := db.Query(fmt.Sprintf("PRAGMA foreign_key_list('%s')", tableName))
		if err != nil {
			return err
		}

		for fkRows.Next() {
			var (
				id       int
				seq      int
				table    string
				from     string
				to       string
				onUpdate string
				onDelete string
				match    string
			)

			if err := fkRows.Scan(&id, &seq, &table, &from, &to, &onUpdate, &onDelete, &match); err != nil {
				return err
			}

			fmt.Fprintf(file, "  FK %s -> %s.%s\n", from, table, to)
		}
		fkRows.Close()

		fmt.Fprintln(file)
	}

	return nil
}

func (r *Repository) ExecSQLCommand(sqlCmd string) ([]byte, error) {
	db := r.DB.DB
	rows, err := db.Query(sqlCmd)
	if err == nil {
		defer rows.Close()
		cols, _ := rows.Columns()
		results := make([]map[string]interface{}, 0)
		for rows.Next() {
			vals := make([]interface{}, len(cols))
			ptrs := make([]interface{}, len(cols))
			for i := range vals {
				ptrs[i] = &vals[i]
			}
			if err := rows.Scan(ptrs...); err != nil {
				return nil, err
			}
			row := make(map[string]interface{}, len(cols))
			for i, col := range cols {
				switch v := vals[i].(type) {
				case []byte:
					row[col] = string(v)
				default:
					row[col] = v
				}
			}
			results = append(results, row)
		}
		b, err := json.Marshal(results)
		if err != nil {
			return nil, err
		}
		return b, nil
	}
	res, err := db.Exec(sqlCmd)
	if err != nil {
		return nil, err
	}
	affected, _ := res.RowsAffected()
	lastID, _ := res.LastInsertId()
	m := map[string]interface{}{"rows_affected": affected, "last_insert_id": lastID}
	b, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	return b, nil
}
