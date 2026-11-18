package app

// Анонимная статистика (настройка «анонимная статистика», по умолчанию включена).
// Раз в сутки программа отправляет одну пачку в Supabase (функция submit_metrics):
//   - установка была активна: случайный id установки, версии программы и Яндекс Музыки,
//     система, работает ли патч;
//   - сколько раз слушали треки из базы замен и какие из их файлов не загрузились.
//
// Прослушивания и ошибки на стороне базы складываются в общие суммы «трек + день» — без id
// установки, кто что слушал, узнать нельзя. Обычные треки Яндекс Музыки и подмены своим
// файлом не считаются. Схема и правила хранения — supabase/metrics.sql (30 дней).

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/http"
	"runtime"
	"time"

	"github.com/S4YN3X/F_ckCensor/internal/ipc"
)

// Публичный ключ: им можно только вызвать submit_metrics (и отправить сообщение о цензуре)
var (
	metricsURL = "https://mrqlfhiqjinueibsxnof.supabase.co/rest/v1/rpc/submit_metrics"
	metricsKey = "sb_publishable_6yhyZv93vTD26vz-F4VQSQ_pN12qEMn"
	// первая отправка — когда после запуска поднимется сеть, дальше — проверка раз в несколько часов
	metricsFirst = time.Minute
	metricsEvery = 3 * time.Hour
)

// userCustomTrack — свой трек пользователя (его файл, не из базы): диапазон как CUSTOM_BASE в inject.js.
func userCustomTrack(id string) bool {
	return isCustomTrackID(id) && !isBaseCustomTrackID(id)
}

func isBaseCustomTrackID(id string) bool {
	var n int64
	_, err := fmt.Sscan(id, &n)
	return err == nil && n >= 600000001000000 && n < 600000002000000
}

// countMetric прибавляет прослушивание трека из базы (или ошибку его файла).
func (a *App) countMetric(e ipc.Event) {
	if !a.store.Settings().Metrics || e.Local || e.TrackID == "" || userCustomTrack(e.TrackID) {
		return
	}
	a.store.CountMetric(e.TrackID, e.Type == "failed")
}

func newInstallID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = b[6]&0x0f | 0x40 // UUID v4
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// metricsLoop раз в сутки отправляет статистику.
func (a *App) metricsLoop() {
	time.Sleep(metricsFirst)
	for {
		a.sendMetrics()
		time.Sleep(metricsEvery)
	}
}

func (a *App) sendMetrics() {
	if !a.store.Settings().Metrics {
		return
	}
	today := time.Now().Format("2006-01-02")
	id, plays, failures, sent := a.store.MetricsSnapshot(newInstallID)
	if sent == today {
		return
	}
	a.mu.Lock()
	ym, patch := a.ymVersion, a.patch
	a.mu.Unlock()
	body, _ := json.Marshal(map[string]any{"p": map[string]any{
		"client": id, "app": a.Version, "ym": ym, "os": runtime.GOOS, "patch": patch == PatchOK,
		"plays": plays, "failures": failures,
	}})
	req, _ := http.NewRequest("POST", metricsURL, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("apikey", metricsKey)
	req.Header.Set("Authorization", "Bearer "+metricsKey)
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return // нет связи — в следующий раз
	}
	resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return
	}
	a.store.MetricsSent(plays, failures, today)
}
