//go:build e2e

package main

import (
	"archive/zip"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ilikosov/ranobelib/internal/api"
	"github.com/ilikosov/ranobelib/internal/book"
	"github.com/ilikosov/ranobelib/internal/epub"
	"github.com/ilikosov/ranobelib/internal/progress"
)

func TestE2EDownloadOmniscientReader(t *testing.T) {
	bookURL := "https://ranobelib.me/ru/book/26690--omniscient-readers-viewpoint-novel"

	slug, err := api.ExtractSlug(bookURL)
	if err != nil {
		t.Fatalf("ExtractSlug: %v", err)
	}

	client := api.NewClient()
	client.Logf = t.Logf

	t.Logf("Получение информации о книге...")
	info, err := client.GetBookInfo(slug)
	if err != nil {
		t.Fatalf("GetBookInfo: %v", err)
	}
	t.Logf("Название: %s", info.Title)
	t.Logf("Автор: %s", info.Author)
	t.Logf("Обложка: %s", info.CoverURL)
	if info.Title == "" {
		t.Fatal("название книги пустое")
	}

	t.Logf("Получение списка глав...")
	allChapters, err := client.GetChapters(slug)
	if err != nil {
		t.Fatalf("GetChapters: %v", err)
	}
	if len(allChapters) == 0 {
		t.Fatal("список глав пуст")
	}
	t.Logf("Всего глав: %d", len(allChapters))
	t.Logf("Первая глава: %s", allChapters[0].Title())

	sel := book.Selection{FirstN: 1}
	toDownload := book.FilterChapters(allChapters, sel)
	if len(toDownload) == 0 {
		t.Fatal("нет глав для загрузки")
	}

	store := progress.NewStore(t.TempDir())
	d := &book.Downloader{
		Fetcher:    &apiFetcher{client: client, slug: slug},
		Store:      store,
		Logf:       t.Logf,
		DelayMin:   1 * time.Second,
		DelayMax:   2 * time.Second,
		SaveEvery:  5,
		RetryPause: 5 * time.Second,
	}

	t.Logf("Загрузка 1-й главы...")
	res := d.DownloadAll(slug, bookURL, toDownload, allChapters)
	if res.RateLimited > 0 {
		t.Fatalf("глава не скачалась из-за rate-limit (429)")
	}
	if res.Failed > 0 {
		t.Fatalf("глава не скачалась: %d ошибок", res.Failed)
	}
	if len(res.Content) != 1 {
		t.Fatalf("скачано глав: %d, ожидалось 1", len(res.Content))
	}
	t.Logf("Глава скачана: %s", res.Content[0].Title)

	gen := epub.NewGenerator(api.SiteURL() + "/")
	gen.Logf = t.Logf
	outputPath := filepath.Join(t.TempDir(), "omniscient-reader.epub")

	t.Logf("Генерация EPUB без изображений...")
	if err := gen.Generate(info, res.Content, outputPath, true); err != nil {
		t.Fatalf("Generate: %v", err)
	}

	zr, err := zip.OpenReader(outputPath)
	if err != nil {
		t.Fatalf("EPUB не открывается как zip: %v", err)
	}
	defer zr.Close()

	var hasXHTML, hasOPF bool
	var chapterText string
	for _, f := range zr.File {
		t.Logf("  %s (%d байт)", f.Name, f.UncompressedSize64)
		if strings.HasSuffix(f.Name, ".xhtml") {
			hasXHTML = true
			rc, err := f.Open()
			if err != nil {
				t.Fatal(err)
			}
			buf := make([]byte, f.UncompressedSize64)
			n, _ := rc.Read(buf)
			chapterText = string(buf[:n])
			rc.Close()
		}
		if strings.HasSuffix(f.Name, ".opf") {
			hasOPF = true
		}
	}

	if !hasXHTML {
		t.Error("EPUB не содержит xhtml-секций")
	}
	if !hasOPF {
		t.Error("EPUB не содержит файла метаданных (.opf)")
	}
	if !strings.Contains(chapterText, res.Content[0].Title) {
		t.Error("в EPUB не найден заголовок главы")
	}
	if !strings.Contains(chapterText, "Том") && !strings.Contains(chapterText, "Глава") {
		t.Error("в EPUB не найден номер тома/главы")
	}

	t.Logf("\n✅ E2E-тест пройден: книга «%s» успешно скачана в EPUB", info.Title)
}
