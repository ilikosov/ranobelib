// Package epub собирает EPUB-файл из скачанных глав.
//
// Изображения скачиваются собственным HTTP-клиентом (CDN ranobelib.me
// требует заголовок Referer), сохраняются во временный каталог и
// встраиваются в книгу; недоступные изображения заменяются текстовым
// плейсхолдером, не прерывая сборку.
package epub

import (
	"fmt"
	"html"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	goepub "github.com/go-shiori/go-epub"

	"github.com/ilikosov/ranobelib/internal/content"
	"github.com/ilikosov/ranobelib/internal/model"
)

const placeholder = "<p>📷 [изображение недоступно]</p>"

var (
	imgTagRe  = regexp.MustCompile(`(?is)<img[^>]*>`)
	srcAttrRe = regexp.MustCompile(`(?i)\ssrc="([^"]+)"`)
	brTagRe   = regexp.MustCompile(`(?i)<br\s*>`)
	hrTagRe   = regexp.MustCompile(`(?i)<hr\s*>`)
)

// Generator собирает EPUB-файлы.
type Generator struct {
	HTTP    *http.Client
	Referer string
	Logf    func(format string, args ...any)
}

// NewGenerator создаёт генератор с настройками по умолчанию.
func NewGenerator(referer string) *Generator {
	return &Generator{
		HTTP:    &http.Client{Timeout: 60 * time.Second},
		Referer: referer,
	}
}

func (g *Generator) logf(format string, args ...any) {
	if g.Logf != nil {
		g.Logf(format, args...)
	}
}

// Generate собирает EPUB из глав и записывает его в outputPath.
// При noImages все изображения (включая обложку) вырезаются.
func (g *Generator) Generate(info model.BookInfo, chapters []model.DownloadedChapter, outputPath string, noImages bool) error {
	e, err := goepub.NewEpub(info.Title)
	if err != nil {
		return fmt.Errorf("создание EPUB: %w", err)
	}
	e.SetAuthor(info.Author)
	e.SetLang("ru")
	if info.Description != "" {
		e.SetDescription(info.Description)
	}

	tmpDir, err := os.MkdirTemp("", "ranobelib-images-*")
	if err != nil {
		return fmt.Errorf("создание временного каталога: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	if !noImages && info.CoverURL != "" {
		if err := g.addCover(e, info.CoverURL, tmpDir); err != nil {
			g.logf("⚠️ Не удалось добавить обложку: %v", err)
		}
	}

	imgIndex := 0
	for i, ch := range chapters {
		body := ch.Data
		if noImages {
			body = content.RemoveImages(body)
		} else {
			body = g.embedImages(e, body, tmpDir, &imgIndex)
		}
		body = fixVoidTags(body)
		section := fmt.Sprintf("<h2>%s</h2>\n%s", html.EscapeString(ch.Title), body)
		filename := fmt.Sprintf("chapter_%04d.xhtml", i+1)
		if _, err := e.AddSection(section, ch.Title, filename, ""); err != nil {
			return fmt.Errorf("добавление главы «%s»: %w", ch.Title, err)
		}
	}

	if dir := filepath.Dir(outputPath); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("создание каталога %s: %w", dir, err)
		}
	}
	if err := e.Write(outputPath); err != nil {
		return fmt.Errorf("запись EPUB: %w", err)
	}
	return nil
}

func (g *Generator) addCover(e *goepub.Epub, coverURL, tmpDir string) error {
	local, err := g.downloadImage(coverURL, tmpDir, "cover")
	if err != nil {
		return err
	}
	internal, err := e.AddImage(local, "cover"+filepath.Ext(local))
	if err != nil {
		return err
	}
	return e.SetCover(internal, "")
}

// embedImages скачивает все изображения главы и переписывает src на
// внутренние пути EPUB. Недоступные изображения заменяются плейсхолдером.
func (g *Generator) embedImages(e *goepub.Epub, body, tmpDir string, imgIndex *int) string {
	return imgTagRe.ReplaceAllStringFunc(body, func(tag string) string {
		m := srcAttrRe.FindStringSubmatch(tag)
		if m == nil {
			return placeholder
		}
		src := m[1]
		*imgIndex++
		local, err := g.downloadImage(src, tmpDir, fmt.Sprintf("img_%04d", *imgIndex))
		if err != nil {
			g.logf("⚠️ Изображение %s недоступно: %v", src, err)
			return placeholder
		}
		internal, err := e.AddImage(local, filepath.Base(local))
		if err != nil {
			g.logf("⚠️ Не удалось встроить изображение %s: %v", src, err)
			return placeholder
		}
		return strings.Replace(tag, m[0], ` src="`+internal+`"`, 1)
	})
}

// downloadImage скачивает изображение во временный каталог и возвращает
// путь к локальному файлу.
func (g *Generator) downloadImage(rawURL, tmpDir, baseName string) (string, error) {
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64)")
	if g.Referer != "" {
		req.Header.Set("Referer", g.Referer)
	}
	resp, err := g.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	ext := extensionFor(rawURL, resp.Header.Get("Content-Type"))
	path := filepath.Join(tmpDir, baseName+ext)
	f, err := os.Create(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := io.Copy(f, resp.Body); err != nil {
		return "", err
	}
	return path, nil
}

func extensionFor(rawURL, contentType string) string {
	if i := strings.IndexAny(rawURL, "?#"); i >= 0 {
		rawURL = rawURL[:i]
	}
	switch ext := strings.ToLower(filepath.Ext(rawURL)); ext {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp":
		return ext
	}
	switch contentType {
	case "image/png":
		return ".png"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	default:
		return ".jpg"
	}
}

// fixVoidTags приводит одиночные теги к самозакрывающейся XHTML-форме.
func fixVoidTags(body string) string {
	body = brTagRe.ReplaceAllString(body, "<br/>")
	body = hrTagRe.ReplaceAllString(body, "<hr/>")
	return body
}
