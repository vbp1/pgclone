package fs

import (
	"fmt"
	"os"
	"path/filepath"
)

// MkdirP создает путь рекурсивно с правами 0755 (как `mkdir -p`).
// Не генерирует ошибку, если директория уже существует.
func MkdirP(path string) error {
	if path == "" {
		return fmt.Errorf("path is empty")
	}
	return os.MkdirAll(path, 0o755)
}

// CleanupDir удаляет все содержимое директории.
// Сама директория остается.
func CleanupDir(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		p := filepath.Join(dir, e.Name())
		if err := os.RemoveAll(p); err != nil {
			return err
		}
	}
	return nil
}
