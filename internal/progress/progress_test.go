package progress

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ilikosov/ranobelib/internal/model"
)

func TestSaveLoadFindDelete(t *testing.T) {
	store := NewStore(t.TempDir())

	chapters := []model.DownloadedChapter{
		{ID: 0, Title: "Том 1 Глава 1", Data: "<p>текст</p>"},
		{ID: 1, Title: "Том 1 Глава 2", Data: "<p>ещё</p>"},
	}
	all := []model.Chapter{{ID: 100, Volume: "1", Number: "1"}, {ID: 101, Volume: "1", Number: "2"}}

	if err := store.Save("42--book", chapters, "https://ranobelib.me/ru/book/42--book", all); err != nil {
		t.Fatal(err)
	}

	loaded := store.Load("42--book")
	if loaded.CompletedCount != 2 || len(loaded.Chapters) != 2 {
		t.Fatalf("Load: %+v", loaded)
	}
	if loaded.Chapters[1].Data != "<p>ещё</p>" {
		t.Errorf("Data = %q", loaded.Chapters[1].Data)
	}
	if len(loaded.AllChapters) != 2 || loaded.URL == "" {
		t.Errorf("нет данных для быстрого продолжения: %+v", loaded)
	}

	found := store.Find()
	if len(found) != 1 || found[0].BookID != "42--book" {
		t.Fatalf("Find: %+v", found)
	}

	if err := store.Delete("42--book"); err != nil {
		t.Fatal(err)
	}
	if got := store.Find(); len(got) != 0 {
		t.Fatalf("после Delete: %+v", got)
	}
	// Повторное удаление не должно быть ошибкой.
	if err := store.Delete("42--book"); err != nil {
		t.Fatal(err)
	}
}

func TestLoadMissingAndCorrupted(t *testing.T) {
	store := NewStore(t.TempDir())

	if p := store.Load("nope"); p.CompletedCount != 0 || len(p.Chapters) != 0 {
		t.Errorf("Load отсутствующего файла: %+v", p)
	}

	path := filepath.Join(store.Dir, "bad_progress.json")
	if err := os.WriteFile(path, []byte("{broken"), 0o644); err != nil {
		t.Fatal(err)
	}
	if p := store.Load("bad"); p.CompletedCount != 0 {
		t.Errorf("Load повреждённого файла: %+v", p)
	}
	if found := store.Find(); len(found) != 0 {
		t.Errorf("Find не должен возвращать повреждённые файлы: %+v", found)
	}
}

func TestFindSkipsPerVolumeProgress(t *testing.T) {
	store := NewStore(t.TempDir())
	ch := []model.DownloadedChapter{{ID: 0, Title: "t", Data: "d"}}
	if err := store.Save("42--book", ch, "", nil); err != nil {
		t.Fatal(err)
	}
	if err := store.Save("42--book_том_2", ch, "", nil); err != nil {
		t.Fatal(err)
	}
	found := store.Find()
	if len(found) != 1 || found[0].BookID != "42--book" {
		t.Fatalf("Find должен пропускать прогрессы томов: %+v", found)
	}
}
