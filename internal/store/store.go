// Package store хранит настройки, историю замен и статистику в %LOCALAPPDATA%\F_ckCensor.
package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"
)

const historyLimit = 1000

type Settings struct {
	Enabled    bool `json:"enabled"`
	Autostart  bool `json:"autostart"`
	RestartYM  bool `json:"restartYM"`
	AutoUpdate bool `json:"autoUpdate"`
	HideToTray bool `json:"hideToTray"`
	// Theme — тема окна: system (как в Windows), light или dark
	Theme string `json:"theme"`
	// Animations — анимации в окне программы (по умолчанию включены)
	Animations bool `json:"animations"`
	// UIScale — масштаб интерфейса в процентах поверх масштаба Windows: 100, 125, 150, 175, 200
	UIScale int    `json:"uiScale"`
	YMDir   string `json:"ymDir"`
	// Metrics — анонимная статистика (по умолчанию включена; см. internal/app/metrics.go)
	Metrics bool `json:"metrics"`
}

// Zoom — масштаб интерфейса долей (1 = 100%).
func (s Settings) Zoom() float64 {
	if s.UIScale < 100 || s.UIScale > 200 {
		return 1
	}
	return float64(s.UIScale) / 100
}

// Entry — один трек в истории.
type Entry struct {
	TrackID string    `json:"trackId"`
	Title   string    `json:"title"`
	Artist  string    `json:"artist"`
	Cover   string    `json:"cover"`
	Time    time.Time `json:"time"`
	Failed  bool      `json:"failed"`
	// Original — трек сыграл как есть: замены для него нет. В статистику не идёт.
	Original bool `json:"original,omitempty"`
}

type Stats struct {
	Total     int    `json:"total"`
	Failed    int    `json:"failed"`
	Today     int    `json:"today"`
	TodayDate string `json:"todayDate"`
}

type data struct {
	Settings    Settings  `json:"settings"`
	Stats       Stats     `json:"stats"`
	History     []Entry   `json:"history"`
	LastUpdated time.Time `json:"lastUpdateCheck"`
	Metrics     Metrics   `json:"metrics"`
}

// Metrics — счётчики анонимной статистики до отправки: прослушивания и ошибки треков из базы.
type Metrics struct {
	// ID — случайный id установки: только чтобы считать активные установки
	ID       string         `json:"id"`
	Plays    map[string]int `json:"plays,omitempty"`
	Failures map[string]int `json:"failures,omitempty"`
	// SentDay — день последней отправки (ГГГГ-ММ-ДД)
	SentDay string `json:"sentDay,omitempty"`
}

type Store struct {
	mu   sync.Mutex
	path string
	d    data
}

// Dir — папка данных программы: %LOCALAPPDATA%\F_ckCensor в Windows,
// ~/Library/Application Support/F_ckCensor в macOS (там же её ищет скрипт в клиенте).
func Dir() string { return filepath.Join(baseDir(), "F_ckCensor") }

// LegacyDir — папка данных под старым названием программы (FkCensor).
func LegacyDir() string { return filepath.Join(baseDir(), "FkCensor") }

func baseDir() string {
	if runtime.GOOS == "darwin" {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, "Library", "Application Support")
	}
	return os.Getenv("LOCALAPPDATA")
}

// RemoveLegacy удаляет папку старого названия, если в ней не осталось настроек
// (их уже перенесли): там лишь копия ipc.json и журнал старого патча.
func RemoveLegacy() {
	old := LegacyDir()
	if _, err := os.Stat(filepath.Join(old, "state.json")); err == nil {
		return
	}
	_ = os.RemoveAll(old)
}

// MigrateLegacy переносит данные из папки старого названия в новую: настройки, историю,
// кэш окна. Новая папка уже есть — ничего не делает. Если папку целиком не переименовать
// (файл занят), копирует главное: настройки и историю.
func MigrateLegacy() {
	newDir, oldDir := Dir(), LegacyDir()
	if _, err := os.Stat(newDir); err == nil {
		return
	}
	if _, err := os.Stat(oldDir); err != nil {
		return
	}
	if os.Rename(oldDir, newDir) == nil {
		return
	}
	_ = os.MkdirAll(newDir, 0755)
	for _, name := range []string{"state.json", "original_hash.txt"} {
		if data, err := os.ReadFile(filepath.Join(oldDir, name)); err == nil {
			_ = os.WriteFile(filepath.Join(newDir, name), data, 0644)
		}
	}
}

func Open() *Store {
	s := &Store{path: filepath.Join(Dir(), "state.json")}
	s.d.Settings = Settings{
		Enabled:    true,
		RestartYM:  true,
		AutoUpdate: true,
		HideToTray: true,
		Animations: true,
		UIScale:    100,
		Metrics:    true,
	}
	if raw, err := os.ReadFile(s.path); err == nil {
		_ = json.Unmarshal(raw, &s.d)
	}
	return s
}

// save пишет файл через временный, чтобы сбой посреди записи не стёр данные.
func (s *Store) save() {
	raw, err := json.MarshalIndent(s.d, "", "  ")
	if err != nil {
		return
	}
	_ = os.MkdirAll(filepath.Dir(s.path), 0755)
	tmp := s.path + ".tmp"
	if os.WriteFile(tmp, raw, 0644) == nil {
		_ = os.Rename(tmp, s.path)
	}
}

func (s *Store) Settings() Settings {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.d.Settings
}

func (s *Store) UpdateSettings(f func(*Settings)) Settings {
	s.mu.Lock()
	defer s.mu.Unlock()
	f(&s.d.Settings)
	s.save()
	return s.d.Settings
}

func (s *Store) rollDay(now time.Time) {
	if day := now.Format("2006-01-02"); s.d.Stats.TodayDate != day {
		s.d.Stats.TodayDate = day
		s.d.Stats.Today = 0
	}
}

func (s *Store) Stats() Stats {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rollDay(time.Now())
	return s.d.Stats
}

func (s *Store) History() []Entry {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Entry(nil), s.d.History...)
}

// RecentlyRecorded — был ли трек уже записан за последние window.
// Клиент может запросить файл повторно (предзагрузка, перемотка), это не новая замена.
// withOriginal — учитывать и записи «без замены».
func (s *Store) RecentlyRecorded(trackID string, window time.Duration, withOriginal bool) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, e := range s.d.History {
		if time.Since(e.Time) > window {
			break
		}
		if e.TrackID == trackID && (withOriginal || !e.Original) {
			return true
		}
	}
	return false
}

// Record добавляет запись в начало истории и обновляет статистику.
func (s *Store) Record(e Entry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rollDay(e.Time)
	switch {
	case e.Original:
	case e.Failed:
		s.d.Stats.Failed++
	default:
		s.d.Stats.Total++
		s.d.Stats.Today++
	}
	s.d.History = append([]Entry{e}, s.d.History...)
	if len(s.d.History) > historyLimit {
		s.d.History = s.d.History[:historyLimit]
	}
	s.save()
}

// UpdateEntry дополняет запись данными (например, метаданными трека после загрузки).
func (s *Store) UpdateEntry(trackID string, at time.Time, f func(*Entry)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.d.History {
		if s.d.History[i].TrackID == trackID && s.d.History[i].Time.Equal(at) {
			f(&s.d.History[i])
			s.save()
			return
		}
	}
}

func (s *Store) ClearHistory() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.d.History = nil
	s.save()
}

func (s *Store) LastUpdateCheck() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.d.LastUpdated
}

func (s *Store) SetLastUpdateCheck(t time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.d.LastUpdated = t
	s.save()
}

// metricsLimit — сколько разных треков копится до отправки (больше за сутки не бывает)
const metricsLimit = 300

// CountMetric прибавляет прослушивание (или ошибку) трека к счётчикам статистики.
func (s *Store) CountMetric(trackID string, failed bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m := &s.d.Metrics.Plays
	if failed {
		m = &s.d.Metrics.Failures
	}
	if *m == nil {
		*m = map[string]int{}
	}
	if _, ok := (*m)[trackID]; !ok && len(*m) >= metricsLimit {
		return
	}
	(*m)[trackID]++
	s.save()
}

// MetricsSnapshot — id установки (создаётся при первом вызове), копии счётчиков и день прошлой отправки.
func (s *Store) MetricsSnapshot(newID func() string) (id string, plays, failures map[string]int, sentDay string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.d.Metrics.ID == "" {
		s.d.Metrics.ID = newID()
		s.save()
	}
	return s.d.Metrics.ID, copyCounts(s.d.Metrics.Plays), copyCounts(s.d.Metrics.Failures), s.d.Metrics.SentDay
}

// MetricsSent вычитает отправленное (пока шла отправка, могло прибавиться) и запоминает день.
func (s *Store) MetricsSent(plays, failures map[string]int, day string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sub := func(m map[string]int, sent map[string]int) {
		if m == nil {
			return
		}
		for k, n := range sent {
			if m[k] -= n; m[k] <= 0 {
				delete(m, k)
			}
		}
	}
	sub(s.d.Metrics.Plays, plays)
	sub(s.d.Metrics.Failures, failures)
	s.d.Metrics.SentDay = day
	s.save()
}

func copyCounts(m map[string]int) map[string]int {
	out := make(map[string]int, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
