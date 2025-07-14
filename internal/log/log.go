package log

import (
	"log/slog"
	"os"
)

// Setup инициализирует глобальный slog.Logger.
// Если debug=true — уровень Debug; если verbose=true — Info; иначе — Warn.
// Функция также делает этот логгер логгером по-умолчанию (slog.SetDefault).
func Setup(debug bool, verbose bool) *slog.Logger {
	level := slog.LevelWarn
	if verbose {
		level = slog.LevelInfo
	}
	if debug {
		level = slog.LevelDebug
	}

	h := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})
	l := slog.New(h)
	slog.SetDefault(l)
	return l
}
