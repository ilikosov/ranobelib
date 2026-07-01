package api

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ilikosov/ranobelib/internal/model"
)

func TestExtractSlug(t *testing.T) {
	tests := []struct {
		url     string
		want    string
		wantErr bool
	}{
		{"https://ranobelib.me/ru/book/165329--kusuriya-no-hitorigoto-ln-novel", "165329--kusuriya-no-hitorigoto-ln-novel", false},
		{"https://ranobelib.me/ru/book/165329--kusuriya-no-hitorigoto-ln-novel?section=chapters", "165329--kusuriya-no-hitorigoto-ln-novel", false},
		{"https://ranobelib.me/ru/book/165329--slug/", "165329--slug", false},
		{"  https://ranobelib.me/sakurasou-no-pet-na-kanojo-novel  ", "sakurasou-no-pet-na-kanojo-novel", false},
		{"", "", true},
		{"https://ranobelib.me/", "", true},
	}
	for _, tt := range tests {
		got, err := ExtractSlug(tt.url)
		if (err != nil) != tt.wantErr {
			t.Errorf("ExtractSlug(%q) error = %v, wantErr %v", tt.url, err, tt.wantErr)
			continue
		}
		if got != tt.want {
			t.Errorf("ExtractSlug(%q) = %q, want %q", tt.url, got, tt.want)
		}
	}
}

// testClient создаёт клиент, направленный на тестовый сервер, с нулевыми
// паузами между повторами.
func testClient(srv *httptest.Server) *Client {
	c := NewClient()
	c.BaseURL = srv.URL + "/api/manga"
	c.HTTP = srv.Client()
	c.RetryDelays429 = []time.Duration{0, 0, 0}
	c.RetryDelaysNet = []time.Duration{0, 0, 0}
	c.Sleep = func(time.Duration) {}
	return c
}

func TestGetBookInfo(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/manga/42--test-book" {
			t.Errorf("неожиданный путь: %s", r.URL.Path)
		}
		if got := r.Header.Get("Site-Id"); got != "3" {
			t.Errorf("Site-Id = %q, want 3", got)
		}
		fmt.Fprint(w, `{"data":{"name":"Test Book","rus_name":"Тестовая книга",
			"cover":{"default":"https://cover.example/c.jpg"},
			"authors":[{"name":"Автор Один"},{"name":"Автор Два"}],
			"summary":"Описание"}}`)
	}))
	defer srv.Close()

	info, err := testClient(srv).GetBookInfo("42--test-book")
	if err != nil {
		t.Fatal(err)
	}
	if info.Title != "Тестовая книга" {
		t.Errorf("Title = %q", info.Title)
	}
	if info.Author != "Автор Один, Автор Два" {
		t.Errorf("Author = %q", info.Author)
	}
	if info.CoverURL != "https://cover.example/c.jpg" {
		t.Errorf("CoverURL = %q", info.CoverURL)
	}
}

func TestGetChapters(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"data":[
			{"id":100,"volume":"1","number":"1","name":"Начало","branches":[{"id":7,"branch_id":3}]},
			{"id":101,"volume":"1","number":"2","name":"","branches":[]}
		]}`)
	}))
	defer srv.Close()

	chapters, err := testClient(srv).GetChapters("42--test-book")
	if err != nil {
		t.Fatal(err)
	}
	if len(chapters) != 2 {
		t.Fatalf("len = %d, want 2", len(chapters))
	}
	if chapters[0].Title() != "Том 1 Глава 1 — Начало" {
		t.Errorf("Title() = %q", chapters[0].Title())
	}
	if chapters[1].Title() != "Том 1 Глава 2" {
		t.Errorf("Title() = %q", chapters[1].Title())
	}
}

func TestGetChapterContentQuery(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		fmt.Fprint(w, `{"data":{"content":"<p>текст</p>","attachments":[]}}`)
	}))
	defer srv.Close()

	ch := model.Chapter{Volume: "2", Number: "5", Branches: []model.Branch{{ID: 9, BranchID: 4}}}
	cc, err := testClient(srv).GetChapterContent("42--test-book", ch)
	if err != nil {
		t.Fatal(err)
	}
	if string(cc.Content) != `"<p>текст</p>"` {
		t.Errorf("Content = %s", cc.Content)
	}
	for _, want := range []string{"volume=2", "number=5", "branch_id=4"} {
		if !strings.Contains(gotQuery, want) {
			t.Errorf("в запросе %q нет %q", gotQuery, want)
		}
	}
}

func TestRetryOn429ThenSuccess(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls <= 2 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		fmt.Fprint(w, `{"data":[{"id":1,"volume":"1","number":"1","name":"x","branches":[]}]}`)
	}))
	defer srv.Close()

	if _, err := testClient(srv).GetChapters("42--test-book"); err != nil {
		t.Fatalf("после повторов ожидался успех, получено: %v", err)
	}
	if calls != 3 {
		t.Errorf("calls = %d, want 3", calls)
	}
}

func TestRateLimitedAfterAllRetries(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	_, err := testClient(srv).GetChapters("42--test-book")
	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("ожидался ErrRateLimited, получено: %v", err)
	}
}
