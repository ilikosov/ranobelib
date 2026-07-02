// Package progress сохраняет и восстанавливает состояние загрузки,
// чтобы прерванную сессию можно было продолжить с того же места.
// Формат совместим по структуре с оригинальным ranobelib-parser:
// progress/{bookId}_progress.json.
package progress

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ilikosov/ranobelib/internal/model"
)

const fileSuffix = "_progress.json"

// Store — каталог с файлами прогресса.
type Store struct {
	Dir string
}

// NewStore создаёт хранилище в каталоге dir (по умолчанию «progress»).
func NewStore(dir string) *Store {
	if dir == "" {
		dir = "progress"
	}
	return &Store{Dir: dir}
}

func (s *Store) filePath(bookID string) string {
	return filepath.Join(s.Dir, bookID+fileSuffix)
}

// Save записывает прогресс загрузки книги на диск.
func (s *Store) Save(bookID string, chapters []model.DownloadedChapter, bookURL string, allChapters []model.Chapter) error {
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		return fmt.Errorf("создание каталога прогресса: %w", err)
	}
	p := model.Progress{
		Timestamp:      time.Now().Format(time.RFC3339),
		URL:            bookURL,
		CompletedCount: len(chapters),
		Chapters:       chapters,
		AllChapters:    allChapters,
	}
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.filePath(bookID), data, 0o644)
}

// Load восстанавливает прогресс книги. Если файла нет или он повреждён,
// возвращает пустой прогресс без ошибки — как оригинал.
func (s *Store) Load(bookID string) model.Progress {
	data, err := os.ReadFile(s.filePath(bookID))
	if err != nil {
		return model.Progress{}
	}
	var p model.Progress
	if err := json.Unmarshal(data, &p); err != nil {
		return model.Progress{}
	}
	return p
}

// Delete удаляет файл прогресса (после успешной полной загрузки).
func (s *Store) Delete(bookID string) error {
	err := os.Remove(s.filePath(bookID))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// Saved — найденная сохранённая сессия.
type Saved struct {
	BookID string
	Data   model.Progress
}

// Find возвращает все сохранённые сессии загрузки.
func (s *Store) Find() []Saved {
	entries, err := os.ReadDir(s.Dir)
	if err != nil {
		return nil
	}
	var found []Saved
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, fileSuffix) {
			continue
		}
		bookID := strings.TrimSuffix(name, fileSuffix)
		// Файлы прогресса отдельных томов (режим «том за томом»)
		// не предлагаются как сессии для продолжения — как в оригинале.
		if strings.Contains(bookID, "_том_") {
			continue
		}
		p := s.Load(bookID)
		if p.Timestamp == "" && len(p.Chapters) == 0 {
			continue // повреждённый файл
		}
		found = append(found, Saved{BookID: bookID, Data: p})
	}
	return found
}
