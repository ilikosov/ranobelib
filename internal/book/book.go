// Package book содержит логику работы с главами книги: выбор томов,
// фильтрацию, группировку и загрузку с сохранением прогресса.
package book

import (
	"fmt"
	"math/rand"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ilikosov/ranobelib/internal/model"
)

// Selection — выбор пользователя: какие главы качать и в каком режиме.
type Selection struct {
	Volumes        []int // конкретные тома; пусто — все тома
	FirstN         int   // >0 — только первые N глав (тестовый режим)
	VolumeByVolume bool  // сохранять каждый том отдельным EPUB
	NoImages       bool  // собирать EPUB без изображений
}

// VolumeNumber возвращает номер тома главы (0, если распарсить не удалось).
func VolumeNumber(ch model.Chapter) int {
	n, err := strconv.Atoi(strings.TrimSpace(ch.Volume))
	if err != nil {
		return 0
	}
	return n
}

// AllVolumes возвращает отсортированный список номеров томов в книге.
func AllVolumes(chapters []model.Chapter) []int {
	seen := map[int]bool{}
	for _, ch := range chapters {
		seen[VolumeNumber(ch)] = true
	}
	volumes := make([]int, 0, len(seen))
	for v := range seen {
		volumes = append(volumes, v)
	}
	sort.Ints(volumes)
	return volumes
}

// FilterChapters возвращает главы, подходящие под выбор пользователя.
func FilterChapters(chapters []model.Chapter, sel Selection) []model.Chapter {
	if sel.FirstN > 0 {
		if sel.FirstN >= len(chapters) {
			return chapters
		}
		return chapters[:sel.FirstN]
	}
	if len(sel.Volumes) == 0 {
		return chapters
	}
	wanted := map[int]bool{}
	for _, v := range sel.Volumes {
		wanted[v] = true
	}
	var out []model.Chapter
	for _, ch := range chapters {
		if wanted[VolumeNumber(ch)] {
			out = append(out, ch)
		}
	}
	return out
}

// GroupByVolumes группирует главы по томам, сохраняя порядок глав.
// Возвращает отображение том→главы и отсортированные номера томов.
func GroupByVolumes(chapters []model.Chapter) (map[int][]model.Chapter, []int) {
	groups := map[int][]model.Chapter{}
	for _, ch := range chapters {
		v := VolumeNumber(ch)
		groups[v] = append(groups[v], ch)
	}
	volumes := make([]int, 0, len(groups))
	for v := range groups {
		volumes = append(volumes, v)
	}
	sort.Ints(volumes)
	return groups, volumes
}

// OutputFileName формирует имя выходного файла (без расширения) по
// правилам оригинального парсера: суффиксы _том_N, _тома_…, _первые_N_глав.
func OutputFileName(bookName string, sel Selection) string {
	name := bookName
	switch {
	case sel.FirstN > 0:
		name += fmt.Sprintf("_первые_%d_глав", sel.FirstN)
	case len(sel.Volumes) == 1:
		name += fmt.Sprintf("_том_%d", sel.Volumes[0])
	case len(sel.Volumes) >= 2 && len(sel.Volumes) <= 3:
		parts := make([]string, len(sel.Volumes))
		for i, v := range sel.Volumes {
			parts[i] = strconv.Itoa(v)
		}
		name += "_тома_" + strings.Join(parts, "_")
	case len(sel.Volumes) > 3:
		name += fmt.Sprintf("_тома_%d-%d", sel.Volumes[0], sel.Volumes[len(sel.Volumes)-1])
	}
	return name
}

// Fetcher загружает контент главы и возвращает готовый HTML.
// Реализуется поверх api.Client в main; выделен в интерфейс, чтобы
// логику загрузки можно было тестировать без сети.
type Fetcher interface {
	FetchChapterHTML(ch model.Chapter) (string, error)
	IsRateLimited(err error) bool
}

// Downloader качает главы с паузами, повторами и сохранением прогресса.
type Downloader struct {
	Fetcher    Fetcher
	Store      ProgressStore
	Logf       func(format string, args ...any)
	Sleep      func(d time.Duration)
	DelayMin   time.Duration // минимальная пауза между главами
	DelayMax   time.Duration // максимальная пауза между главами
	SaveEvery  int           // сохранять прогресс каждые N глав
	RetryPause time.Duration // пауза перед повторной попыткой глав с 429
}

// ProgressStore — подмножество progress.Store, нужное загрузчику.
type ProgressStore interface {
	Save(bookID string, chapters []model.DownloadedChapter, bookURL string, allChapters []model.Chapter) error
	Load(bookID string) model.Progress
}

// Result — итог загрузки.
type Result struct {
	Content     []model.DownloadedChapter
	RateLimited int // сколько глав не удалось скачать из-за 429
	Failed      int // сколько глав не удалось скачать по другим причинам
}

func (d *Downloader) logf(format string, args ...any) {
	if d.Logf != nil {
		d.Logf(format, args...)
	}
}

func (d *Downloader) sleep(t time.Duration) {
	if d.Sleep != nil {
		d.Sleep(t)
	} else {
		time.Sleep(t)
	}
}

func (d *Downloader) interChapterDelay() {
	min, max := d.DelayMin, d.DelayMax
	if max <= min {
		if min > 0 {
			d.sleep(min)
		}
		return
	}
	d.sleep(min + time.Duration(rand.Int63n(int64(max-min))))
}

// DownloadAll качает выбранные главы. Уже скачанные (по сохранённому
// прогрессу) пропускаются. Идентификатор главы в прогрессе — её порядковый
// номер в полном списке глав, поэтому он стабилен между сессиями.
func (d *Downloader) DownloadAll(bookID, bookURL string, toDownload, all []model.Chapter) Result {
	order := make(map[int64]int, len(all))
	for i, ch := range all {
		order[ch.ID] = i
	}

	wanted := make(map[int64]bool, len(toDownload))
	for _, ch := range toDownload {
		wanted[chapterID(ch, order)] = true
	}

	// downloaded хранит все скачанные главы книги (в том числе из прошлых
	// сессий вне текущего выбора), чтобы прогресс не терялся при смене
	// выбранных томов. В результат попадают только выбранные главы.
	saved := d.Store.Load(bookID)
	downloaded := make(map[int64]model.DownloadedChapter, len(saved.Chapters))
	for _, ch := range saved.Chapters {
		downloaded[ch.ID] = ch
	}
	if len(downloaded) > 0 {
		d.logf("💾 Найден сохранённый прогресс: %d глав уже загружено", len(downloaded))
	}

	saveProgress := func() {
		chapters := make([]model.DownloadedChapter, 0, len(downloaded))
		for _, ch := range downloaded {
			chapters = append(chapters, ch)
		}
		sort.Slice(chapters, func(i, j int) bool { return chapters[i].ID < chapters[j].ID })
		if err := d.Store.Save(bookID, chapters, bookURL, all); err != nil {
			d.logf("⚠️ Не удалось сохранить прогресс: %v", err)
		}
	}

	var res Result
	var rateLimited []model.Chapter
	sinceLastSave := 0
	for i, ch := range toDownload {
		id := chapterID(ch, order)
		if _, ok := downloaded[id]; ok {
			continue
		}

		d.logf("📖 [%d/%d] Загрузка: %s", i+1, len(toDownload), ch.Title())
		htmlData, err := d.Fetcher.FetchChapterHTML(ch)
		switch {
		case err == nil:
			downloaded[id] = model.DownloadedChapter{ID: id, Title: ch.Title(), Data: htmlData}
			sinceLastSave++
		case d.Fetcher.IsRateLimited(err):
			d.logf("⚠️ Глава «%s» отложена из-за ограничения запросов (429)", ch.Title())
			rateLimited = append(rateLimited, ch)
		default:
			d.logf("❌ Не удалось загрузить главу «%s»: %v", ch.Title(), err)
			res.Failed++
		}

		if d.SaveEvery > 0 && sinceLastSave >= d.SaveEvery {
			saveProgress()
			sinceLastSave = 0
			d.logf("💾 Прогресс сохранён (%d глав)", len(downloaded))
		}
		if i < len(toDownload)-1 {
			d.interChapterDelay()
		}
	}

	// Повторная попытка для глав, упавших с 429.
	if len(rateLimited) > 0 {
		d.logf("\n🔄 Повторная попытка для %d глав с ошибкой 429 через %s...", len(rateLimited), d.RetryPause)
		d.sleep(d.RetryPause)
		for _, ch := range rateLimited {
			id := chapterID(ch, order)
			d.logf("📖 Повтор: %s", ch.Title())
			htmlData, err := d.Fetcher.FetchChapterHTML(ch)
			if err != nil {
				d.logf("❌ Снова не удалось: %v", err)
				res.RateLimited++
				continue
			}
			downloaded[id] = model.DownloadedChapter{ID: id, Title: ch.Title(), Data: htmlData}
			d.interChapterDelay()
		}
	}

	saveProgress()

	content := make([]model.DownloadedChapter, 0, len(toDownload))
	for id, ch := range downloaded {
		if wanted[id] {
			content = append(content, ch)
		}
	}
	sort.Slice(content, func(i, j int) bool { return content[i].ID < content[j].ID })
	res.Content = content
	return res
}

func chapterID(ch model.Chapter, order map[int64]int) int64 {
	if idx, ok := order[ch.ID]; ok {
		return int64(idx)
	}
	return ch.ID
}
