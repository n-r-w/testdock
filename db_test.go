package pglive

import (
	"context"
	"testing"
)

func Test_InitDB(t *testing.T) {
	db := GetPool(t, "./migrations", WithForceDockerMode())

	rows, err := db.Query(context.Background(), "SELECT name FROM test_table")
	if err != nil {
		t.Fatalf("error: %s", err)
	}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("error: %s", err)
		}
		if name != "test" {
			t.Fatalf("expected 'test', got '%s'", name)
		}
	}
}
