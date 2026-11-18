package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/S4YN3X/F_ckCensor/internal/ipc"
	"github.com/S4YN3X/F_ckCensor/internal/store"
)

func TestMetricsCountAndSend(t *testing.T) {
	t.Setenv("LOCALAPPDATA", t.TempDir())
	a := &App{Version: "v9.9.9", store: store.Open(), listeners: map[int]func(){}, patch: PatchOK, ymVersion: "5.124.0"}

	a.countMetric(ipc.Event{Type: "replaced", TrackID: "50941920"})
	a.countMetric(ipc.Event{Type: "replaced", TrackID: "50941920"})
	a.countMetric(ipc.Event{Type: "replaced", TrackID: "600000001000047"})  // свой трек из базы — считается
	a.countMetric(ipc.Event{Type: "replaced", TrackID: "600000000000005"})  // свой трек пользователя — нет
	a.countMetric(ipc.Event{Type: "replaced", TrackID: "777", Local: true}) // свой файл — нет
	a.countMetric(ipc.Event{Type: "failed", TrackID: "130011719"})

	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			P map[string]any `json:"p"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		got = body.P
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	old := metricsURL
	metricsURL = srv.URL
	defer func() { metricsURL = old }()

	a.sendMetrics()
	if got == nil {
		t.Fatal("ничего не отправлено")
	}
	plays, _ := got["plays"].(map[string]any)
	fails, _ := got["failures"].(map[string]any)
	if len(plays) != 2 || plays["50941920"] != float64(2) || plays["600000001000047"] != float64(1) {
		t.Fatalf("plays %v", plays)
	}
	if len(fails) != 1 || fails["130011719"] != float64(1) {
		t.Fatalf("failures %v", fails)
	}
	if got["patch"] != true || got["ym"] != "5.124.0" || got["app"] != "v9.9.9" || got["client"] == "" {
		t.Fatalf("install %v", got)
	}

	// в тот же день второй раз не отправляет, счётчики обнулены
	got = nil
	a.sendMetrics()
	if got != nil {
		t.Fatal("отправлено дважды за день")
	}
	_, p, f, day := a.store.MetricsSnapshot(newInstallID)
	if len(p) != 0 || len(f) != 0 || day != time.Now().Format("2006-01-02") {
		t.Fatalf("после отправки: %v %v %q", p, f, day)
	}

	// выключено — не считает
	_ = a.SetSetting("metrics", false)
	a.countMetric(ipc.Event{Type: "replaced", TrackID: "1"})
	if _, p, _, _ := a.store.MetricsSnapshot(newInstallID); len(p) != 0 {
		t.Fatalf("посчитано при выключенной статистике: %v", p)
	}
}
