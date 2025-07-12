package postgres

import (
	"context"
	"testing"

	pgxmock "github.com/pashagolub/pgxmock/v3"
)

func TestStreamRows_HandlerCalled(t *testing.T) {
	ctx := context.Background()
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("mock init: %v", err)
	}
	defer mock.Close()

	rows := pgxmock.NewRows([]string{"id"}).AddRow(1).AddRow(2).AddRow(3)
	mock.ExpectQuery("SELECT id FROM test").WillReturnRows(rows)

	var count int
	h := func(_ []any) error { count++; return nil }

	if err := StreamRows(ctx, mock, "SELECT id FROM test", nil, 1, h); err != nil {
		t.Fatalf("StreamRows: %v", err)
	}
	if count != 3 {
		t.Fatalf("expected 3 rows, got %d", count)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}
