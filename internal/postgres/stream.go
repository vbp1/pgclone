package postgres

import (
	"context"
	"log/slog"

	"github.com/jackc/pgx/v5"
)

// RowHandler вызывается для каждой строки; data содержит значения колонок в виде []any.
// Если handler возвращает ошибку – чтение прекращается и она пробрасывается выше.
type RowHandler func(data []any) error

// Queryer minimal subset of pgxpool.Pool needed for streaming.
type Queryer interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// StreamRows выполняет запрос и построчно обрабатывает результат через handler.
// Она не загружает весь набор данных в память.
// colsExpected – количество ожидаемых колонок; если 0 – не проверяется.
func StreamRows(ctx context.Context, q Queryer, sql string, args []any, colsExpected int, handler RowHandler) error {
	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		vals, err := rows.Values()
		if err != nil {
			return err
		}
		if colsExpected > 0 && len(vals) != colsExpected {
			slog.Warn("stream: columns mismatch", "have", len(vals), "want", colsExpected)
		}
		if err := handler(vals); err != nil {
			return err
		}
	}
	return rows.Err()
}
