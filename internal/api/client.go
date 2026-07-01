// Package api реализует клиент публичного API ranobelib.me
// (api.cdnlibs.org), через которое сайт отдаёт метаданные книги,
// список глав и их содержимое.
package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ilikosov/ranobelib/internal/model"
)

const (
	// DefaultBaseURL — база API ranobelib.me.
	DefaultBaseURL = "https://api.cdnlibs.org/api/manga"

	siteURL   = "https://ranobelib.me"
	userAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 " +
		"(KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36"
)

// ErrRateLimited возвращается, когда сервер отвечает 429 даже после
// всех повторных попыток. Вызывающий код может отложить главу и
// попробовать снова позже.
var ErrRateLimited = errors.New("429 Too Many Requests: сервер ограничил запросы")

// Client — HTTP-клиент API с повторными попытками и паузами при 429.
type Client struct {
	BaseURL        string
	HTTP           *http.Client
	Logf           func(format string, args ...any) // вывод сообщений о повторах; nil — молча
	RetryDelays429 []time.Duration                  // паузы перед повторами при 429
	RetryDelaysNet []time.Duration                  // паузы перед повторами при сетевых ошибках
	Sleep          func(d time.Duration)
}

// NewClient создаёт клиент с настройками по умолчанию
// (паузы 30/60/120 с при 429, 2/4/6 с при сетевых ошибках).
func NewClient() *Client {
	return &Client{
		BaseURL:        DefaultBaseURL,
		HTTP:           &http.Client{Timeout: 60 * time.Second},
		RetryDelays429: []time.Duration{30 * time.Second, 60 * time.Second, 120 * time.Second},
		RetryDelaysNet: []time.Duration{2 * time.Second, 4 * time.Second, 6 * time.Second},
		Sleep:          time.Sleep,
	}
}

// ExtractSlug выделяет slug книги из её URL:
// https://ranobelib.me/ru/book/165329--kusuriya-no-hitorigoto-ln-novel?section=chapters
// → 165329--kusuriya-no-hitorigoto-ln-novel.
// Slug одновременно служит идентификатором книги для файла прогресса.
func ExtractSlug(bookURL string) (string, error) {
	trimmed := strings.TrimSpace(bookURL)
	if trimmed == "" {
		return "", errors.New("пустой URL")
	}
	u, err := url.Parse(trimmed)
	if err != nil {
		return "", fmt.Errorf("некорректный URL: %w", err)
	}
	path := strings.TrimSuffix(u.Path, "/")
	slug := path[strings.LastIndex(path, "/")+1:]
	if slug == "" {
		return "", fmt.Errorf("не удалось выделить название книги из URL %q", bookURL)
	}
	return slug, nil
}

func (c *Client) logf(format string, args ...any) {
	if c.Logf != nil {
		c.Logf(format, args...)
	}
}

// get выполняет GET-запрос с повторными попытками: при 429 ждёт по
// RetryDelays429, при сетевых ошибках и 5xx — по RetryDelaysNet.
func (c *Client) get(rawURL string) ([]byte, error) {
	attempts := len(c.RetryDelays429) + 1
	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		body, err := c.getOnce(rawURL)
		if err == nil {
			return body, nil
		}
		lastErr = err

		var delay time.Duration
		switch {
		case errors.Is(err, ErrRateLimited):
			if attempt >= len(c.RetryDelays429) {
				return nil, err
			}
			delay = c.RetryDelays429[attempt]
			c.logf("⚠️ Ошибка 429 (Too Many Requests). Ожидание %s перед повтором (попытка %d/%d)...",
				delay, attempt+1, attempts-1)
		default:
			if attempt >= len(c.RetryDelaysNet) {
				return nil, err
			}
			delay = c.RetryDelaysNet[attempt]
			c.logf("⚠️ Ошибка запроса: %v. Повтор через %s (попытка %d/%d)...",
				err, delay, attempt+1, attempts-1)
		}
		c.Sleep(delay)
	}
	return nil, lastErr
}

func (c *Client) getOnce(rawURL string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Referer", siteURL+"/")
	req.Header.Set("Site-Id", "3")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusTooManyRequests:
		return nil, ErrRateLimited
	case resp.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("HTTP %d для %s", resp.StatusCode, rawURL)
	}
	return io.ReadAll(resp.Body)
}

// GetBookInfo запрашивает метаданные книги (название, автор, обложку,
// описание).
func (c *Client) GetBookInfo(slug string) (model.BookInfo, error) {
	var payload struct {
		Data struct {
			Name    string `json:"name"`
			RusName string `json:"rus_name"`
			Cover   struct {
				Default string `json:"default"`
			} `json:"cover"`
			Authors []struct {
				Name string `json:"name"`
			} `json:"authors"`
			Summary string `json:"summary"`
		} `json:"data"`
	}

	rawURL := fmt.Sprintf("%s/%s?fields[]=authors&fields[]=summary", c.BaseURL, url.PathEscape(slug))
	body, err := c.get(rawURL)
	if err != nil {
		return model.BookInfo{}, fmt.Errorf("получение информации о книге: %w", err)
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return model.BookInfo{}, fmt.Errorf("разбор ответа с информацией о книге: %w", err)
	}

	info := model.BookInfo{
		Title:       payload.Data.RusName,
		CoverURL:    payload.Data.Cover.Default,
		Description: payload.Data.Summary,
	}
	if info.Title == "" {
		info.Title = payload.Data.Name
	}
	var authors []string
	for _, a := range payload.Data.Authors {
		if a.Name != "" {
			authors = append(authors, a.Name)
		}
	}
	info.Author = strings.Join(authors, ", ")
	if info.Author == "" {
		info.Author = "Неизвестный автор"
	}
	return info, nil
}

// GetChapters запрашивает полный список глав книги.
func (c *Client) GetChapters(slug string) ([]model.Chapter, error) {
	var payload struct {
		Data []model.Chapter `json:"data"`
	}

	rawURL := fmt.Sprintf("%s/%s/chapters", c.BaseURL, url.PathEscape(slug))
	body, err := c.get(rawURL)
	if err != nil {
		return nil, fmt.Errorf("получение списка глав: %w", err)
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("разбор списка глав: %w", err)
	}
	if len(payload.Data) == 0 {
		return nil, errors.New("список глав пуст: проверьте URL книги")
	}
	return payload.Data, nil
}

// GetChapterContent запрашивает содержимое одной главы.
func (c *Client) GetChapterContent(slug string, ch model.Chapter) (model.ChapterContent, error) {
	q := url.Values{}
	q.Set("volume", ch.Volume)
	q.Set("number", ch.Number)
	if len(ch.Branches) > 0 && ch.Branches[0].BranchID != 0 {
		q.Set("branch_id", fmt.Sprintf("%d", ch.Branches[0].BranchID))
	}

	var payload struct {
		Data model.ChapterContent `json:"data"`
	}

	rawURL := fmt.Sprintf("%s/%s/chapter?%s", c.BaseURL, url.PathEscape(slug), q.Encode())
	body, err := c.get(rawURL)
	if err != nil {
		return model.ChapterContent{}, err
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return model.ChapterContent{}, fmt.Errorf("разбор содержимого главы: %w", err)
	}
	return payload.Data, nil
}

// SiteURL возвращает базовый адрес сайта для абсолютизации
// относительных ссылок на изображения.
func SiteURL() string { return siteURL }
