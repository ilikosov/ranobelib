// ranobelib — CLI-программа для скачивания книг с ranobelib.me в формате
// EPUB. Функциональный аналог https://github.com/ryadik/ranobelib-parser,
// написанный на Go и работающий напрямую через API сайта (без браузера).
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ilikosov/ranobelib/internal/api"
	"github.com/ilikosov/ranobelib/internal/book"
	"github.com/ilikosov/ranobelib/internal/cli"
	"github.com/ilikosov/ranobelib/internal/content"
	"github.com/ilikosov/ranobelib/internal/epub"
	"github.com/ilikosov/ranobelib/internal/model"
	"github.com/ilikosov/ranobelib/internal/progress"
)

const booksDir = "books"

// apiFetcher адаптирует api.Client к интерфейсу book.Fetcher.
type apiFetcher struct {
	client *api.Client
	slug   string
}

func (f *apiFetcher) FetchChapterHTML(ch model.Chapter) (string, error) {
	cc, err := f.client.GetChapterContent(f.slug, ch)
	if err != nil {
		return "", err
	}
	return content.ToHTML(cc.Content, cc.Attachments, api.SiteURL())
}

func (f *apiFetcher) IsRateLimited(err error) bool {
	return errors.Is(err, api.ErrRateLimited)
}

func main() {
	var (
		flagURL       = flag.String("url", "", "URL книги на ranobelib.me (неинтерактивный режим)")
		flagVolumes   = flag.String("volumes", "", "тома для загрузки: «1,3» или «1-3» (по умолчанию все)")
		flagFirst     = flag.Int("first", 0, "скачать только первые N глав (тестовый режим)")
		flagNoImages  = flag.Bool("no-images", false, "собирать EPUB без изображений")
		flagCoverOnly = flag.Bool("cover-only", false, "скачивать только обложку, без изображений глав")
		flagSplit     = flag.Bool("split", false, "сохранять каждый том отдельным EPUB-файлом")
	)
	flag.Parse()

	ui := cli.New(os.Stdin, os.Stdout)
	logf := ui.Printf
	client := api.NewClient()
	client.Logf = logf
	store := progress.NewStore("progress")
	interactive := *flagURL == ""

	ui.Welcome()

	var (
		bookURL     string
		allChapters []model.Chapter
		sel         book.Selection
	)

	// Предложение продолжить сохранённую сессию (как в оригинале).
	if interactive {
		if saved := ui.ChooseSavedProgress(store.Find()); saved != nil {
			logf("\n✅ Продолжение загрузки: %s", saved.BookID)
			logf("📚 Загружено глав: %d", saved.Data.CompletedCount)
			if saved.Data.URL != "" && len(saved.Data.AllChapters) > 0 {
				logf("🔄 Быстрое продолжение — используем сохранённые данные...")
				bookURL = saved.Data.URL
				allChapters = saved.Data.AllChapters
			} else if saved.Data.URL != "" {
				bookURL = saved.Data.URL
			} else {
				logf("⚠️ В сохранённом прогрессе нет URL книги.")
				bookURL = ui.PromptURL()
			}
		}
	}

	if bookURL == "" {
		if interactive {
			bookURL = ui.PromptURL()
		} else {
			bookURL = *flagURL
		}
	}

	slug, err := api.ExtractSlug(bookURL)
	if err != nil {
		fatal(err)
	}
	bookID := slug

	logf("\nПолучение информации о книге...")
	info, err := client.GetBookInfo(slug)
	if err != nil {
		fatal(err)
	}
	logf("📗 %s — %s", info.Title, info.Author)

	if len(allChapters) == 0 {
		logf("\nПолучение списка глав...")
		allChapters, err = client.GetChapters(slug)
		if err != nil {
			fatal(err)
		}
	}
	logf("\n📚 Найдено %d глав", len(allChapters))

	// Выбор томов и режимов: интерактивно или из флагов.
	if interactive {
		sel = ui.SelectVolumes(allChapters)
	} else {
		sel = book.Selection{FirstN: *flagFirst, VolumeByVolume: *flagSplit}
		switch {
		case *flagNoImages:
			sel.Images = model.ImagesNone
		case *flagCoverOnly:
			sel.Images = model.ImagesCoverOnly
		}
		if *flagVolumes != "" {
			sel.Volumes, err = parseVolumesFlag(*flagVolumes)
			if err != nil {
				fatal(err)
			}
		}
	}

	logf("\n🔧 === ВЫБРАННЫЕ НАСТРОЙКИ ===")
	if sel.VolumeByVolume {
		logf("📚 Режим: тома по отдельности")
	} else {
		logf("📚 Режим: один файл")
	}
	switch sel.Images {
	case model.ImagesNone:
		logf("🖼️ Изображения: БЕЗ изображений (стабильно)")
	case model.ImagesCoverOnly:
		logf("🖼️ Изображения: только обложка")
	default:
		logf("🖼️ Изображения: с изображениями (fallback при ошибках)")
	}
	logf("==============================")

	toDownload := book.FilterChapters(allChapters, sel)
	if len(toDownload) == 0 {
		fatal(errors.New("не найдено глав для загрузки: проверьте выбранные тома"))
	}
	logf("\n📖 К загрузке: %d глав", len(toDownload))
	logf("   Первая глава: «%s»", toDownload[0].Title())
	if len(toDownload) > 1 {
		logf("   Последняя глава: «%s»", toDownload[len(toDownload)-1].Title())
	}
	logf("\n⏱️ Примерное время загрузки: %d мин.", (len(toDownload)+1)/2)
	logf("💾 Прогресс автоматически сохраняется каждые 5 глав")
	logf("🔄 При прерывании загрузку можно будет продолжить с того же места")

	downloader := &book.Downloader{
		Fetcher:    &apiFetcher{client: client, slug: slug},
		Store:      store,
		Logf:       logf,
		DelayMin:   3 * time.Second,
		DelayMax:   7500 * time.Millisecond,
		SaveEvery:  5,
		RetryPause: 10 * time.Second,
	}
	generator := epub.NewGenerator(api.SiteURL() + "/")
	generator.Logf = logf

	if sel.VolumeByVolume {
		runVolumeByVolume(ui, downloader, generator, store, info, toDownload, allChapters, bookID, bookURL, sel)
	} else {
		runSingleFile(ui, downloader, generator, store, info, toDownload, allChapters, bookID, bookURL, sel, interactive)
	}
	logf("🔚 Завершение работы...")
}

// runSingleFile — обычный режим: все выбранные главы в один EPUB.
func runSingleFile(ui *cli.CLI, d *book.Downloader, gen *epub.Generator, store *progress.Store,
	info model.BookInfo, toDownload, allChapters []model.Chapter,
	bookID, bookURL string, sel book.Selection, interactive bool) {

	ui.Printf("\nЗагрузка глав книги...")
	res := d.DownloadAll(bookID, bookURL, toDownload, allChapters)

	if res.RateLimited > 0 {
		ui.Printf("\n⚠️ === ОБНАРУЖЕНЫ ОШИБКИ 429 (Too Many Requests) ===")
		ui.Printf("❌ Не удалось загрузить %d глав из-за ограничения запросов сервером.", res.RateLimited)
		ui.Printf("💾 Прогресс сохранён. Загружено глав: %d из %d", len(res.Content), len(toDownload))
		ui.Printf("\n💡 Запустите программу снова через некоторое время —")
		ui.Printf("   загрузка автоматически продолжится с незагруженных глав.")
		return
	}

	if len(res.Content) < len(toDownload) {
		missing := len(toDownload) - len(res.Content)
		ui.Printf("\n⚠️ ВНИМАНИЕ: загружено только %d из %d глав (не хватает %d).",
			len(res.Content), len(toDownload), missing)
		ui.Printf("💡 Запустите программу снова, чтобы догрузить остальные — прогресс сохранён.")
		if interactive && !ui.ConfirmIncomplete() {
			ui.Printf("\n🔚 Завершение работы без создания книги...")
			return
		}
	}
	if len(res.Content) == 0 {
		ui.Printf("\n❌ Не удалось загрузить ни одной главы.")
		return
	}

	outputName := book.OutputFileName(bookID, sel)
	outputPath := filepath.Join(booksDir, outputName+".epub")
	if !generateWithFallback(ui, gen, info, res.Content, outputPath, sel.Images) {
		return
	}

	if len(res.Content) == len(toDownload) && len(toDownload) == len(allChapters) {
		if err := store.Delete(bookID); err == nil {
			ui.Printf("🗑️ Файл прогресса очищен")
		}
	} else {
		ui.Printf("\n💾 Файл прогресса сохранён (загружено %d глав).", len(res.Content))
	}
}

// runVolumeByVolume — режим «том за томом»: каждый том скачивается и
// сохраняется отдельным EPUB, ошибка одного тома не губит остальные.
func runVolumeByVolume(ui *cli.CLI, d *book.Downloader, gen *epub.Generator, store *progress.Store,
	info model.BookInfo, toDownload, allChapters []model.Chapter,
	bookID, bookURL string, sel book.Selection) {

	ui.Printf("\n🔥 === РЕЖИМ ОБРАБОТКИ ПО ТОМАМ ===")
	groups, volumes := book.GroupByVolumes(toDownload)
	ui.Printf("📖 К загрузке: %d глав в %d томах", len(toDownload), len(volumes))

	var created []string
	for i, vol := range volumes {
		chapters := groups[vol]
		ui.Printf("\n📚 === ТОМ %d (%d глав) ===", vol, len(chapters))

		volBookID := fmt.Sprintf("%s_том_%d", bookID, vol)
		res := d.DownloadAll(volBookID, bookURL, chapters, allChapters)

		if res.RateLimited > 0 || len(res.Content) < len(chapters) {
			ui.Printf("⚠️ Том %d загружен не полностью (%d из %d глав) — пропускаем создание EPUB.",
				vol, len(res.Content), len(chapters))
			ui.Printf("💾 Прогресс тома сохранён, при следующем запуске загрузка продолжится.")
			continue
		}

		volInfo := info
		volInfo.Title = fmt.Sprintf("%s. Том %d", info.Title, vol)
		outputPath := filepath.Join(booksDir, fmt.Sprintf("%s_том_%d.epub", bookID, vol))
		if !generateWithFallback(ui, gen, volInfo, res.Content, outputPath, sel.Images) {
			continue
		}
		created = append(created, outputPath)
		if err := store.Delete(volBookID); err == nil {
			ui.Printf("🗑️ Прогресс тома %d очищен", vol)
		}

		if i < len(volumes)-1 {
			time.Sleep(3 * time.Second)
		}
	}

	ui.Printf("\n🎉 === ИТОГОВЫЙ РЕЗУЛЬТАТ ===")
	if len(created) == 0 {
		ui.Printf("❌ Не удалось создать ни одного файла EPUB")
		return
	}
	ui.Printf("✅ Успешно создано файлов EPUB: %d", len(created))
	for i, f := range created {
		ui.Printf("   %d. %s", i+1, filepath.Base(f))
	}
}

// generateWithFallback собирает EPUB; при сетевой ошибке пробует ещё раз
// без изображений (как оригинальный парсер).
func generateWithFallback(ui *cli.CLI, gen *epub.Generator, info model.BookInfo,
	chapters []model.DownloadedChapter, outputPath string, mode model.ImageMode) bool {

	ui.Printf("\nГенерация книги %s...", filepath.Base(outputPath))
	switch mode {
	case model.ImagesNone:
		ui.Printf("🚫 Создаём EPUB БЕЗ изображений (выбрано пользователем)")
	case model.ImagesCoverOnly:
		ui.Printf("🖼️ Режим «только обложка»: изображения глав не скачиваются")
	}
	err := gen.Generate(info, chapters, outputPath, mode)
	if err == nil {
		ui.Printf("\n✅ Книга успешно создана!")
		ui.Printf("📂 Путь к файлу: %s", outputPath)
		ui.Printf("📊 Глав в книге: %d", len(chapters))
		return true
	}

	if mode != model.ImagesNone && isNetworkError(err) {
		ui.Printf("⚠️ Ошибка сети при генерации EPUB: %v", err)
		ui.Printf("🔄 Пробуем создать EPUB без изображений...")
		fallbackPath := strings.TrimSuffix(outputPath, ".epub") + "_без_изображений.epub"
		if err := gen.Generate(info, chapters, fallbackPath, model.ImagesNone); err == nil {
			ui.Printf("\n✅ Книга успешно создана БЕЗ ИЗОБРАЖЕНИЙ!")
			ui.Printf("📂 Путь к файлу: %s", fallbackPath)
			return true
		}
	}
	ui.Printf("❌ Не удалось создать EPUB: %v", err)
	return false
}

func isNetworkError(err error) bool {
	msg := strings.ToLower(err.Error())
	for _, kw := range []string{"econnreset", "network", "timeout", "connection", "aborted", "socket", "refused"} {
		if strings.Contains(msg, kw) {
			return true
		}
	}
	return false
}

// parseVolumesFlag разбирает значение флага -volumes: «1,3,5» или «1-3».
func parseVolumesFlag(value string) ([]int, error) {
	value = strings.TrimSpace(value)
	if from, to, ok := strings.Cut(value, "-"); ok {
		a, err1 := strconv.Atoi(strings.TrimSpace(from))
		b, err2 := strconv.Atoi(strings.TrimSpace(to))
		if err1 != nil || err2 != nil || a > b {
			return nil, fmt.Errorf("некорректный диапазон томов: %q", value)
		}
		var out []int
		for v := a; v <= b; v++ {
			out = append(out, v)
		}
		return out, nil
	}
	var out []int
	for _, part := range strings.Split(value, ",") {
		n, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil {
			return nil, fmt.Errorf("некорректный список томов: %q", value)
		}
		out = append(out, n)
	}
	return out, nil
}

func fatal(err error) {
	fmt.Fprintf(os.Stderr, "\n❌ Критическая ошибка: %v\n", err)
	os.Exit(1)
}
