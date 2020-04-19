package schema

import (
	"testing"

	"github.com/udovin/solve/db"
)

func TestColumnInt64(t *testing.T) {
	// PrimaryKey AutoIncrement Int64 column.
	c1 := Column{
		Name:          "test1",
		Type:          Int64,
		PrimaryKey:    true,
		AutoIncrement: true,
	}
	// Check for SQLite.
	// Note that SQLite does not support bigint as primary key.
	if sql, err := c1.BuildSQL(db.SQLite); err != nil {
		t.Fatal("Error:", err)
	} else if sql != `"test1" integer PRIMARY KEY AUTOINCREMENT` {
		t.Fatal("Wrong SQL:", sql)
	}
	// Check for Postgres.
	if sql, err := c1.BuildSQL(db.Postgres); err != nil {
		t.Fatal("Error:", err)
	} else if sql != `"test1" bigserial PRIMARY KEY` {
		t.Fatal("Wrong SQL:", sql)
	}
	// PrimaryKey Int64 column.
	c2 := Column{Name: "test2", Type: Int64, PrimaryKey: true}
	// Check for SQLite.
	if sql, err := c2.BuildSQL(db.SQLite); err != nil {
		t.Fatal("Error:", err)
	} else if sql != `"test2" integer PRIMARY KEY` {
		t.Fatal("Wrong SQL:", sql)
	}
	// Check for Postgres.
	if sql, err := c2.BuildSQL(db.Postgres); err != nil {
		t.Fatal("Error:", err)
	} else if sql != `"test2" bigint PRIMARY KEY` {
		t.Fatal("Wrong SQL:", sql)
	}
	// Int64 column.
	c3 := Column{Name: "test3", Type: Int64}
	// Check for SQLite.
	if sql, err := c3.BuildSQL(db.SQLite); err != nil {
		t.Fatal("Error:", err)
	} else if sql != `"test3" bigint NOT NULL` {
		t.Fatal("Wrong SQL:", sql)
	}
	// Check for Postgres.
	if sql, err := c3.BuildSQL(db.Postgres); err != nil {
		t.Fatal("Error:", err)
	} else if sql != `"test3" bigint NOT NULL` {
		t.Fatal("Wrong SQL:", sql)
	}
	// Int64 column.
	c4 := Column{Name: "test4", Type: Int64, Nullable: true}
	// Check for SQLite.
	if sql, err := c4.BuildSQL(db.SQLite); err != nil {
		t.Fatal("Error:", err)
	} else if sql != `"test4" bigint` {
		t.Fatal("Wrong SQL:", sql)
	}
	// Check for Postgres.
	if sql, err := c4.BuildSQL(db.Postgres); err != nil {
		t.Fatal("Error:", err)
	} else if sql != `"test4" bigint` {
		t.Fatal("Wrong SQL:", sql)
	}
}

func TestColumnString(t *testing.T) {
	c1 := Column{Name: "test1", Type: String}
	// Check for SQLite.
	if sql, err := c1.BuildSQL(db.SQLite); err != nil {
		t.Fatal("Error:", err)
	} else if sql != `"test1" text NOT NULL` {
		t.Fatal("Wrong SQL:", sql)
	}
	// Check for Postgres.
	if sql, err := c1.BuildSQL(db.Postgres); err != nil {
		t.Fatal("Error:", err)
	} else if sql != `"test1" text NOT NULL` {
		t.Fatal("Wrong SQL:", sql)
	}
	// Nullable column.
	c2 := Column{Name: "test2", Type: String, Nullable: true}
	// Check for SQLite.
	if sql, err := c2.BuildSQL(db.SQLite); err != nil {
		t.Fatal("Error:", err)
	} else if sql != `"test2" text` {
		t.Fatal("Wrong SQL:", sql)
	}
	// Check for Postgres.
	if sql, err := c2.BuildSQL(db.Postgres); err != nil {
		t.Fatal("Error:", err)
	} else if sql != `"test2" text` {
		t.Fatal("Wrong SQL:", sql)
	}
}

func TestColumnJSON(t *testing.T) {
	c1 := Column{Name: "test1", Type: JSON}
	// Check for SQLite.
	if sql, err := c1.BuildSQL(db.SQLite); err != nil {
		t.Fatal("Error:", err)
	} else if sql != `"test1" blob NOT NULL` {
		t.Fatal("Wrong SQL:", sql)
	}
	// Check for Postgres.
	if sql, err := c1.BuildSQL(db.Postgres); err != nil {
		t.Fatal("Error:", err)
	} else if sql != `"test1" jsonb NOT NULL` {
		t.Fatal("Wrong SQL:", sql)
	}
	// Nullable column.
	c2 := Column{Name: "test2", Type: JSON, Nullable: true}
	// Check for SQLite.
	if sql, err := c2.BuildSQL(db.SQLite); err != nil {
		t.Fatal("Error:", err)
	} else if sql != `"test2" blob` {
		t.Fatal("Wrong SQL:", sql)
	}
	// Check for Postgres.
	if sql, err := c2.BuildSQL(db.Postgres); err != nil {
		t.Fatal("Error:", err)
	} else if sql != `"test2" jsonb` {
		t.Fatal("Wrong SQL:", sql)
	}
}

func TestColumnInvalid(t *testing.T) {
	c1 := Column{Name: "test", Type: 228}
	if _, err := c1.BuildSQL(db.SQLite); err == nil {
		t.Fatal("Expected error")
	}
}

func TestTableSimple(t *testing.T) {
	t1 := Table{
		Name: "test_table",
		Columns: []Column{
			{Name: "id", Type: Int64, PrimaryKey: true, AutoIncrement: true},
			{Name: "name", Type: String},
		},
	}
	t1SQLite := `CREATE TABLE "test_table"("id" integer PRIMARY KEY AUTOINCREMENT,"name" text NOT NULL)`
	if sql, err := t1.BuildCreateSQL(db.SQLite); err != nil {
		t.Fatal("Error:", err)
	} else if sql != t1SQLite {
		t.Fatal("Wrong SQL:", sql)
	}
	t1Postgres := `CREATE TABLE "test_table"("id" bigserial PRIMARY KEY,"name" text NOT NULL)`
	if sql, err := t1.BuildCreateSQL(db.Postgres); err != nil {
		t.Fatal("Error:", err)
	} else if sql != t1Postgres {
		t.Fatal("Wrong SQL:", sql)
	}
}

func TestTableInvalidColumn(t *testing.T) {
	t1 := Table{
		Name: "test_table",
		Columns: []Column{
			{Name: "id", Type: 228},
		},
	}
	if _, err := t1.BuildCreateSQL(db.SQLite); err == nil {
		t.Fatal("Expected error")
	}
	if _, err := t1.BuildCreateSQL(db.Postgres); err == nil {
		t.Fatal("Expected error")
	}
}
