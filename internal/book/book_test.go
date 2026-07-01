package book

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/ilikosov/ranobelib/internal/model"
)

func makeChapters() []model.Chapter {
	return []model.Chapter{
		{ID: 100, Volume: "1", Number: "1", Name: "a"},
		{ID: 101, Volume: "1", Number: "2", Name: "b"},
		{ID: 102, Volume: "2", Number: "1", Name: "c"},
		{ID: 103, Volume: "2", Number: "2", Name: "d"},
		{ID: 104, Volume: "3", Number: "1", Name: "e"},
	}
}

func TestFilterChapters(t *testing.T) {
	chapters := makeChapters()

	if got := FilterChapters(chapters, Selection{}); len(got) != 5 {
		t.Errorf("все тома: len = %d", len(got))
	}
	if got := FilterChapters(chapters, Selection{Volumes: []int{2}}); len(got) != 2 || got[0].ID != 102 {
		t.Errorf("том 2: %+v", got)
	}
	if got := FilterChapters(chapters, Selection{Volumes: []int{1, 3}}); len(got) != 3 {
		t.Errorf("тома 1 и 3: len = %d", len(got))
	}
	if got := FilterChapters(chapters, Selection{FirstN: 3}); len(got) != 3 || got[2].ID != 102 {
		t.Errorf("первые 3: %+v", got)
	}
	if got := FilterChapters(chapters, Selection{FirstN: 99}); len(got) != 5 {
		t.Errorf("FirstN больше книги: len = %d", len(got))
	}
}

func TestGroupByVolumes(t *testing.T) {
	groups, volumes := GroupByVolumes(makeChapters())
	if fmt.Sprint(volumes) != "[1 2 3]" {
		t.Fatalf("volumes = %v", volumes)
	}
	if len(groups[1]) != 2 || len(groups[2]) != 2 || len(groups[3]) != 1 {
		t.Errorf("groups = %v", groups)
	}
	if groups[2][0].ID != 102 {
		t.Errorf("порядок глав нарушен: %+v", groups[2])
	}
}

func TestAllVolumes(t *testing.T) {
	if got := AllVolumes(makeChapters()); fmt.Sprint(got) != "[1 2 3]" {
		t.Errorf("AllVolumes = %v", got)
	}
}

func TestOutputFileName(t *testing.T) {
	tests := []struct {
		sel  Selection
		want string
	}{
		{Selection{}, "book"},
		{Selection{Volumes: []int{2}}, "book_том_2"},
		{Selection{Volumes: []int{1, 2}}, "book_тома_1_2"},
		{Selection{Volumes: []int{1, 2, 3}}, "book_тома_1_2_3"},
		{Selection{Volumes: []int{1, 2, 3, 4, 5}}, "book_тома_1-5"},
		{Selection{FirstN: 10}, "book_первые_10_глав"},
	}
	for _, tt := range tests {
		if got := OutputFileName("book", tt.sel); got != tt.want {
			t.Errorf("OutputFileName(%+v) = %q, want %q", tt.sel, got, tt.want)
		}
	}
}

// fakeFetcher имитирует загрузку глав: часть отдаёт с ошибкой 429
// заданное число раз, часть — с постоянной ошибкой.
type fakeFetcher struct {
	rateLimitLeft map[int64]int // id → сколько раз ответить 429
	brokenIDs     map[int64]bool
	calls         int
}

var errRate = errors.New("429")

func (f *fakeFetcher) FetchChapterHTML(ch model.Chapter) (string, error) {
	f.calls++
	if f.brokenIDs[ch.ID] {
		return "", errors.New("boom")
	}
	if left := f.rateLimitLeft[ch.ID]; left > 0 {
		f.rateLimitLeft[ch.ID] = left - 1
		return "", errRate
	}
	return "<p>" + ch.Name + "</p>", nil
}

func (f *fakeFetcher) IsRateLimited(err error) bool { return errors.Is(err, errRate) }

// memStore — прогресс в памяти.
type memStore struct {
	saved map[string]model.Progress
}

func newMemStore() *memStore { return &memStore{saved: map[string]model.Progress{}} }

func (m *memStore) Save(bookID string, chapters []model.DownloadedChapter, url string, all []model.Chapter) error {
	m.saved[bookID] = model.Progress{
		URL: url, CompletedCount: len(chapters),
		Chapters: append([]model.DownloadedChapter(nil), chapters...), AllChapters: all,
	}
	return nil
}

func (m *memStore) Load(bookID string) model.Progress { return m.saved[bookID] }

func newTestDownloader(f Fetcher, s ProgressStore) *Downloader {
	return &Downloader{
		Fetcher: f, Store: s,
		Sleep:     func(time.Duration) {},
		SaveEvery: 2, RetryPause: time.Nanosecond,
	}
}

func TestDownloadAllHappyPath(t *testing.T) {
	all := makeChapters()
	store := newMemStore()
	d := newTestDownloader(&fakeFetcher{}, store)

	res := d.DownloadAll("book", "http://u", all, all)
	if res.Failed != 0 || res.RateLimited != 0 {
		t.Fatalf("res = %+v", res)
	}
	if len(res.Content) != 5 {
		t.Fatalf("len(Content) = %d", len(res.Content))
	}
	// Порядок — по позиции в общем списке.
	for i, ch := range res.Content {
		if ch.ID != int64(i) {
			t.Errorf("Content[%d].ID = %d", i, ch.ID)
		}
	}
	if store.saved["book"].CompletedCount != 5 {
		t.Errorf("прогресс не сохранён: %+v", store.saved["book"])
	}
}

func TestDownloadAllResumesFromProgress(t *testing.T) {
	all := makeChapters()
	store := newMemStore()
	store.saved["book"] = model.Progress{
		CompletedCount: 2,
		Chapters: []model.DownloadedChapter{
			{ID: 0, Title: "t0", Data: "<p>сохранено0</p>"},
			{ID: 1, Title: "t1", Data: "<p>сохранено1</p>"},
		},
	}
	f := &fakeFetcher{}
	d := newTestDownloader(f, store)

	res := d.DownloadAll("book", "http://u", all, all)
	if len(res.Content) != 5 {
		t.Fatalf("len(Content) = %d", len(res.Content))
	}
	if f.calls != 3 {
		t.Errorf("должно качаться только 3 недостающих главы, calls = %d", f.calls)
	}
	if res.Content[0].Data != "<p>сохранено0</p>" {
		t.Errorf("сохранённая глава перезаписана: %+v", res.Content[0])
	}
}

func TestDownloadAllRateLimitRetrySucceeds(t *testing.T) {
	all := makeChapters()
	store := newMemStore()
	// Глава 102 один раз ответит 429, потом отдастся.
	f := &fakeFetcher{rateLimitLeft: map[int64]int{102: 1}}
	d := newTestDownloader(f, store)

	res := d.DownloadAll("book", "http://u", all, all)
	if res.RateLimited != 0 {
		t.Fatalf("повтор должен был успеть: %+v", res)
	}
	if len(res.Content) != 5 {
		t.Fatalf("len(Content) = %d", len(res.Content))
	}
}

func TestDownloadAllRateLimitPersists(t *testing.T) {
	all := makeChapters()
	store := newMemStore()
	f := &fakeFetcher{rateLimitLeft: map[int64]int{102: 99}}
	d := newTestDownloader(f, store)

	res := d.DownloadAll("book", "http://u", all, all)
	if res.RateLimited != 1 {
		t.Fatalf("RateLimited = %d", res.RateLimited)
	}
	if len(res.Content) != 4 {
		t.Fatalf("len(Content) = %d", len(res.Content))
	}
	// Прогресс сохранён — при следующем запуске докачается только глава 102.
	if store.saved["book"].CompletedCount != 4 {
		t.Errorf("прогресс: %+v", store.saved["book"])
	}
}

func TestDownloadAllBrokenChapterCounted(t *testing.T) {
	all := makeChapters()
	f := &fakeFetcher{brokenIDs: map[int64]bool{104: true}}
	d := newTestDownloader(f, newMemStore())

	res := d.DownloadAll("book", "http://u", all, all)
	if res.Failed != 1 || len(res.Content) != 4 {
		t.Fatalf("res = %+v, len = %d", res, len(res.Content))
	}
}

func TestDownloadAllSubsetKeepsForeignProgress(t *testing.T) {
	all := makeChapters()
	store := newMemStore()
	// В прогрессе уже есть глава из тома 1 (индекс 0).
	store.saved["book"] = model.Progress{
		CompletedCount: 1,
		Chapters:       []model.DownloadedChapter{{ID: 0, Title: "t0", Data: "d0"}},
	}
	d := newTestDownloader(&fakeFetcher{}, store)

	// Качаем только том 2 (индексы 2 и 3).
	toDownload := FilterChapters(all, Selection{Volumes: []int{2}})
	res := d.DownloadAll("book", "http://u", toDownload, all)

	if len(res.Content) != 2 {
		t.Fatalf("в результат должны попасть только главы тома 2: %+v", res.Content)
	}
	// А в прогрессе должны остаться и старая глава, и новые.
	if store.saved["book"].CompletedCount != 3 {
		t.Errorf("чужой прогресс потерян: %+v", store.saved["book"])
	}
}
