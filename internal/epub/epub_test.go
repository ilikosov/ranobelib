package epub

import (
	"archive/zip"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ilikosov/ranobelib/internal/model"
)

// pngPixel — минимальный валидный PNG 1×1.
var pngPixel = []byte{
	0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x00, 0x00, 0x0D,
	0x49, 0x48, 0x44, 0x52, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
	0x08, 0x06, 0x00, 0x00, 0x00, 0x1F, 0x15, 0xC4, 0x89, 0x00, 0x00, 0x00,
	0x0D, 0x49, 0x44, 0x41, 0x54, 0x78, 0x9C, 0x62, 0x00, 0x01, 0x00, 0x00,
	0x05, 0x00, 0x01, 0x0D, 0x0A, 0x2D, 0xB4, 0x00, 0x00, 0x00, 0x00, 0x49,
	0x45, 0x4E, 0x44, 0xAE, 0x42, 0x60, 0x82,
}

func imageServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "missing") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.Write(pngPixel)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func testBook(srv *httptest.Server) (model.BookInfo, []model.DownloadedChapter) {
	info := model.BookInfo{
		Title:       "Тестовая книга",
		Author:      "Автор",
		Description: "Описание",
		CoverURL:    srv.URL + "/cover.png",
	}
	chapters := []model.DownloadedChapter{
		{ID: 0, Title: "Том 1 Глава 1", Data: `<p>Первая</p><img src="` + srv.URL + `/pic1.png" alt="x"/>`},
		{ID: 1, Title: "Том 1 Глава 2", Data: `<p>Вторая</p><img src="` + srv.URL + `/missing.png"/>`},
	}
	return info, chapters
}

// readEpub возвращает имена файлов внутри EPUB и содержимое глав
// (только chapter_*.xhtml — служебную страницу обложки не учитываем).
func readEpub(t *testing.T, path string) (names []string, chaptersText string) {
	t.Helper()
	zr, err := zip.OpenReader(path)
	if err != nil {
		t.Fatalf("EPUB не открывается как zip: %v", err)
	}
	defer zr.Close()
	var sb strings.Builder
	for _, f := range zr.File {
		names = append(names, f.Name)
		if strings.HasPrefix(filepath.Base(f.Name), "chapter_") && strings.HasSuffix(f.Name, ".xhtml") {
			rc, err := f.Open()
			if err != nil {
				t.Fatal(err)
			}
			buf := make([]byte, f.UncompressedSize64)
			n, _ := rc.Read(buf)
			sb.Write(buf[:n])
			rc.Close()
		}
	}
	return names, sb.String()
}

func TestGenerateWithImages(t *testing.T) {
	srv := imageServer(t)
	info, chapters := testBook(srv)
	gen := NewGenerator("")
	gen.HTTP = srv.Client()

	out := filepath.Join(t.TempDir(), "book.epub")
	if err := gen.Generate(info, chapters, out, model.ImagesAll); err != nil {
		t.Fatal(err)
	}

	names, text := readEpub(t, out)
	joined := strings.Join(names, "\n")
	if !strings.Contains(joined, "images/") {
		t.Errorf("в EPUB нет изображений:\n%s", joined)
	}
	if !strings.Contains(text, "Первая") || !strings.Contains(text, "Вторая") {
		t.Errorf("текст глав потерян:\n%s", text)
	}
	// Живая картинка встроена, недоступная заменена плейсхолдером.
	if !strings.Contains(text, "images/") {
		t.Errorf("src не переписан на внутренний путь:\n%s", text)
	}
	if !strings.Contains(text, "изображение недоступно") {
		t.Errorf("нет плейсхолдера для недоступной картинки:\n%s", text)
	}
}

func TestGenerateNoImages(t *testing.T) {
	srv := imageServer(t)
	info, chapters := testBook(srv)
	gen := NewGenerator("")
	gen.HTTP = srv.Client()

	out := filepath.Join(t.TempDir(), "book.epub")
	if err := gen.Generate(info, chapters, out, model.ImagesNone); err != nil {
		t.Fatal(err)
	}

	names, text := readEpub(t, out)
	if strings.Contains(text, "<img") {
		t.Errorf("в режиме без изображений остались <img>:\n%s", text)
	}
	if !strings.Contains(text, "Первая") {
		t.Errorf("текст потерян:\n%s", text)
	}
	if strings.Contains(strings.Join(names, "\n"), "cover") {
		t.Errorf("в режиме без изображений не должно быть обложки:\n%s", strings.Join(names, "\n"))
	}
}

func TestGenerateCoverOnly(t *testing.T) {
	srv := imageServer(t)
	info, chapters := testBook(srv)
	gen := NewGenerator("")
	gen.HTTP = srv.Client()

	out := filepath.Join(t.TempDir(), "book.epub")
	if err := gen.Generate(info, chapters, out, model.ImagesCoverOnly); err != nil {
		t.Fatal(err)
	}

	names, text := readEpub(t, out)
	if !strings.Contains(strings.Join(names, "\n"), "cover") {
		t.Errorf("обложка не встроена:\n%s", strings.Join(names, "\n"))
	}
	if strings.Contains(text, "<img") {
		t.Errorf("изображения глав должны быть вырезаны:\n%s", text)
	}
	if !strings.Contains(text, "изображение удалено") {
		t.Errorf("нет плейсхолдера вместо изображений глав:\n%s", text)
	}
	if !strings.Contains(text, "Первая") || !strings.Contains(text, "Вторая") {
		t.Errorf("текст глав потерян:\n%s", text)
	}
}

func TestGenerateCoverFailureIsNotFatal(t *testing.T) {
	srv := imageServer(t)
	info, chapters := testBook(srv)
	info.CoverURL = srv.URL + "/missing-cover.png"
	gen := NewGenerator("")
	gen.HTTP = srv.Client()

	out := filepath.Join(t.TempDir(), "book.epub")
	if err := gen.Generate(info, chapters, out, model.ImagesAll); err != nil {
		t.Fatalf("недоступная обложка не должна прерывать сборку: %v", err)
	}
}
