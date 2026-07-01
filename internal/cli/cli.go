// Package cli реализует интерактивный диалог с пользователем:
// приветствие, ввод URL, выбор томов и режимов — повторяя меню
// оригинального ranobelib-parser.
package cli

import (
	"bufio"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

	"github.com/ilikosov/ranobelib/internal/book"
	"github.com/ilikosov/ranobelib/internal/model"
	"github.com/ilikosov/ranobelib/internal/progress"
)

// CLI читает ответы пользователя и печатает подсказки.
type CLI struct {
	in  *bufio.Scanner
	out io.Writer
}

// New создаёт CLI поверх произвольных потоков (в программе — stdin/stdout,
// в тестах — буферы).
func New(in io.Reader, out io.Writer) *CLI {
	return &CLI{in: bufio.NewScanner(in), out: out}
}

func (c *CLI) printf(format string, args ...any) {
	fmt.Fprintf(c.out, format+"\n", args...)
}

// Printf выводит строку в поток CLI (для сообщений остального кода).
func (c *CLI) Printf(format string, args ...any) {
	c.printf(format, args...)
}

func (c *CLI) prompt(label string) string {
	fmt.Fprint(c.out, label)
	if !c.in.Scan() {
		return ""
	}
	return strings.TrimSpace(c.in.Text())
}

// Welcome печатает приветствие программы.
func (c *CLI) Welcome() {
	c.printf("📚 === RANOBELIB PARSER (Go) ===")
	c.printf("Программа скачивает книги с сайта ranobelib.me и сохраняет их в формате EPUB.")
	c.printf("Готовые книги появятся в папке books/.")
	c.printf("")
}

// PromptURL запрашивает URL книги, пока пользователь не введёт непустую строку.
func (c *CLI) PromptURL() string {
	c.printf("Введите ссылку на книгу с сайта ranobelib.me")
	c.printf("Пример: https://ranobelib.me/ru/book/165329--kusuriya-no-hitorigoto-ln-novel")
	for {
		url := c.prompt("URL: ")
		if url != "" {
			c.printf("\nНачинаем загрузку...")
			return url
		}
		c.printf("❌ URL не может быть пустым. Попробуйте ещё раз.")
	}
}

// ChooseSavedProgress показывает найденные сохранённые сессии и спрашивает,
// продолжить ли одну из них. Возвращает выбранную сессию или nil.
func (c *CLI) ChooseSavedProgress(found []progress.Saved) *progress.Saved {
	if len(found) == 0 {
		return nil
	}
	c.printf("\n💾 === НАЙДЕНЫ СОХРАНЕННЫЕ ПРОГРЕССЫ ===")
	c.printf("Найдено %d сохранённых сессий:\n", len(found))
	for i, p := range found {
		c.printf("   %d. %s", i+1, p.BookID)
		c.printf("      Загружено глав: %d", p.Data.CompletedCount)
		c.printf("      Дата: %s", p.Data.Timestamp)
		if p.Data.URL != "" {
			c.printf("      URL: %s", p.Data.URL)
		}
		c.printf("")
	}
	c.printf("Выберите действие:")
	c.printf("1. Продолжить загрузку (быстрое продолжение)")
	c.printf("2. Начать новую загрузку")

	choice := c.prompt("Ваш выбор (1-2): ")
	if choice != "1" {
		return nil
	}
	if len(found) == 1 {
		return &found[0]
	}
	for {
		numStr := c.prompt(fmt.Sprintf("Номер сессии (1-%d): ", len(found)))
		n, err := strconv.Atoi(numStr)
		if err == nil && n >= 1 && n <= len(found) {
			return &found[n-1]
		}
		c.printf("❌ Некорректный номер. Попробуйте ещё раз.")
	}
}

// SelectVolumes показывает меню выбора томов и режимов загрузки.
func (c *CLI) SelectVolumes(chapters []model.Chapter) book.Selection {
	volumes := book.AllVolumes(chapters)
	c.printf("\n📚 В книге найдено томов: %d (%s), всего глав: %d",
		len(volumes), joinInts(volumes), len(chapters))

	var sel book.Selection
	for {
		c.printf("\nЧто скачать?")
		c.printf("1. Все тома")
		c.printf("2. Конкретные тома (через запятую, например: 1,3,5)")
		c.printf("3. Диапазон томов (например: 1-3)")
		c.printf("4. Первые N глав (тестовый режим)")

		switch c.prompt("Ваш выбор (1-4): ") {
		case "1":
			sel.Volumes = nil
		case "2":
			picked, ok := parseVolumeList(c.prompt("Номера томов: "), volumes)
			if !ok {
				c.printf("❌ Некорректный ввод или таких томов нет в книге.")
				continue
			}
			sel.Volumes = picked
		case "3":
			picked, ok := parseVolumeRange(c.prompt("Диапазон (например 1-3): "), volumes)
			if !ok {
				c.printf("❌ Некорректный диапазон или таких томов нет в книге.")
				continue
			}
			sel.Volumes = picked
		case "4":
			n, err := strconv.Atoi(c.prompt("Сколько глав скачать: "))
			if err != nil || n <= 0 {
				c.printf("❌ Введите положительное число.")
				continue
			}
			sel.FirstN = n
		default:
			c.printf("❌ Введите число от 1 до 4.")
			continue
		}
		break
	}

	// Для загрузки нескольких томов — выбор режима сохранения.
	multiVolume := sel.FirstN == 0 && (len(sel.Volumes) > 1 || (len(sel.Volumes) == 0 && len(volumes) > 1))
	if multiVolume {
		c.printf("\nКак сохранить книгу?")
		c.printf("1. Один файл EPUB со всеми томами")
		c.printf("2. Каждый том отдельным файлом (надёжнее: при ошибке потеряется только текущий том)")
		if c.prompt("Ваш выбор (1-2): ") == "2" {
			sel.VolumeByVolume = true
		}
	}

	c.printf("\nВключить изображения?")
	c.printf("1. С изображениями (при ошибках сети возможен fallback без изображений)")
	c.printf("2. Без изображений (стабильно и быстро)")
	if c.prompt("Ваш выбор (1-2): ") == "2" {
		sel.NoImages = true
	}

	return sel
}

// ConfirmIncomplete спрашивает, создавать ли книгу с неполным содержимым.
func (c *CLI) ConfirmIncomplete() bool {
	answer := strings.ToLower(c.prompt("Создать книгу с неполным содержимым? (y/n): "))
	return answer == "y" || answer == "yes" || answer == "да"
}

func joinInts(nums []int) string {
	parts := make([]string, len(nums))
	for i, n := range nums {
		parts[i] = strconv.Itoa(n)
	}
	return strings.Join(parts, ", ")
}

// parseVolumeList разбирает список «1,3,5», допуская только тома,
// существующие в книге.
func parseVolumeList(input string, available []int) ([]int, bool) {
	exists := map[int]bool{}
	for _, v := range available {
		exists[v] = true
	}
	seen := map[int]bool{}
	var out []int
	for _, part := range strings.Split(input, ",") {
		n, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil || !exists[n] {
			return nil, false
		}
		if !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	if len(out) == 0 {
		return nil, false
	}
	sort.Ints(out)
	return out, true
}

// parseVolumeRange разбирает диапазон «1-3», оставляя только тома,
// существующие в книге.
func parseVolumeRange(input string, available []int) ([]int, bool) {
	parts := strings.SplitN(input, "-", 2)
	if len(parts) != 2 {
		return nil, false
	}
	from, err1 := strconv.Atoi(strings.TrimSpace(parts[0]))
	to, err2 := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err1 != nil || err2 != nil || from > to {
		return nil, false
	}
	exists := map[int]bool{}
	for _, v := range available {
		exists[v] = true
	}
	var out []int
	for v := from; v <= to; v++ {
		if exists[v] {
			out = append(out, v)
		}
	}
	if len(out) == 0 {
		return nil, false
	}
	return out, true
}
