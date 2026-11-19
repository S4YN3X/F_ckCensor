package app

import (
	"strconv"
	"testing"

	"github.com/S4YN3X/F_ckCensor/internal/ipc"
	"github.com/S4YN3X/F_ckCensor/internal/store"
)

func TestThanksFlow(t *testing.T) {
	t.Setenv("LOCALAPPDATA", t.TempDir())
	t.Setenv("HOME", t.TempDir()) // на Mac настройки — в ~/Library/Application Support
	a := &App{Version: "v9.9.9", store: store.Open(), listeners: map[int]func(){}}

	// пользователь сообщил о треке и пожаловался на замену другого
	var res ipc.SyncResponse
	a.syncReports(ipc.SyncRequest{Reports: []store.Report{
		{Kind: "track", ID: "600000001000058", Title: "Атлант", Artists: "Miyagi & Andy Panda"},
		{Kind: "track", ID: "77", Wrong: true, Title: "Остров", Artists: "GUF", URL: "https://x/tracks/77"},
	}}, &res)
	if len(res.Watch) != 2 {
		t.Fatalf("на проверку: %+v", res.Watch)
	}
	if a.thanksView() != nil {
		t.Fatal("карточка до исправления")
	}

	// клиент: первый трек появился в базе — карточка с одним новым
	res = ipc.SyncResponse{}
	if !a.syncReports(ipc.SyncRequest{ReportsDone: []store.ReportKey{{Kind: "track", ID: "600000001000058", AlbumID: "2000000005"}}}, &res) {
		t.Fatal("исправление не заметили")
	}
	v := a.thanksView()
	if v == nil || !v.Visible || v.Total != 1 || v.New != 1 || v.Items[0].Title != "Атлант" {
		t.Fatalf("карточка: %+v", v)
	}
	if len(res.Watch) != 1 || res.Watch[0].ID != "77" {
		t.Fatalf("осталось на проверку: %+v", res.Watch)
	}

	// «понятно» — новых нет, карточка на месте; крестик — карточки нет
	a.ThanksSeen()
	if v = a.thanksView(); v.New != 0 || !v.Visible {
		t.Fatalf("после «понятно»: %+v", v)
	}
	a.ThanksHide()
	if v = a.thanksView(); v.Visible {
		t.Fatalf("после крестика: %+v", v)
	}

	// исправили замену — карточка снова видна, со всеми исправленными, новый один
	res = ipc.SyncResponse{}
	a.syncReports(ipc.SyncRequest{ReportsDone: []store.ReportKey{{Kind: "track", ID: "77", Wrong: true}}}, &res)
	v = a.thanksView()
	if !v.Visible || v.Total != 2 || v.New != 1 || !v.Items[0].Wrong || !v.Items[0].New {
		t.Fatalf("после второго исправления: %+v", v)
	}
	if res.Watch == nil || len(res.Watch) != 0 {
		t.Fatalf("проверять больше нечего, но список должен прийти пустым: %+v", res.Watch)
	}

	// повтор того же сообщения при переносе старых не дублирует и не сбрасывает исправленное
	a.syncReports(ipc.SyncRequest{Reports: []store.Report{{Kind: "track", ID: "77", Wrong: true, Old: true}}}, &ipc.SyncResponse{})
	if v = a.thanksView(); v.Total != 2 {
		t.Fatalf("повтор: %+v", v)
	}

	// старая жалоба на замену без файла: клиент отдаёт нынешний — это ещё не исправление
	a.syncReports(ipc.SyncRequest{Reports: []store.Report{{Kind: "track", ID: "88", Wrong: true, Old: true, Title: "x"}}}, &ipc.SyncResponse{})
	res = ipc.SyncResponse{}
	a.syncReports(ipc.SyncRequest{ReportsBase: []store.ReportKey{{Kind: "track", ID: "88", Wrong: true, URL: "https://x/tracks/88"}}}, &res)
	if v = a.thanksView(); v.Total != 2 {
		t.Fatalf("базовый файл засчитан как исправление: %+v", v)
	}
	if len(res.Watch) != 1 || res.Watch[0].URL != "https://x/tracks/88" {
		t.Fatalf("на проверку после базы: %+v", res.Watch)
	}

	// свой трек из базы — в его альбоме из базы, в приложении
	if u := a.ReportLink("track", "600000001000058"); u != "yandexmusic://album/2000000005/track/600000001000058" {
		t.Fatalf("ссылка на свой трек: %q", u)
	}
	if u := a.ReportLink("album", "2000000005"); u != "yandexmusic://album/2000000005" {
		t.Fatalf("ссылка на альбом: %q", u)
	}
	if u := a.ReportLink("track", "1;rm"); u != "" {
		t.Fatalf("плохой id: %q", u)
	}
}

func TestThanksEdgeCases(t *testing.T) {
	t.Setenv("LOCALAPPDATA", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	a := &App{Version: "v9.9.9", store: store.Open(), listeners: map[int]func(){}}

	// только числовые id
	if a.store.AddReports([]store.Report{{Kind: "track", ID: "1/../x"}, {Kind: "song", ID: "5"}}) {
		t.Fatal("принят плохой id или вид")
	}

	// жалоба на замену исправлена, потом снова жалоба с новым файлом — снова ждём
	a.store.AddReports([]store.Report{{Kind: "track", ID: "77", Wrong: true, URL: "u1", Title: "Остров"}})
	a.store.ReportsDone([]store.ReportKey{{Kind: "track", ID: "77", Wrong: true}}, nil)
	if !a.store.AddReports([]store.Report{{Kind: "track", ID: "77", Wrong: true, URL: "u2"}}) {
		t.Fatal("повторная жалоба не вернулась на проверку")
	}
	w := a.store.ReportWatch()
	if len(w) != 1 || w[0].URL != "u2" {
		t.Fatalf("на проверку: %+v", w)
	}
	// перенос старых сообщений исправленное не сбрасывает
	a.store.ReportsDone([]store.ReportKey{{Kind: "track", ID: "77", Wrong: true}}, nil)
	if a.store.AddReports([]store.Report{{Kind: "track", ID: "77", Wrong: true, Old: true}}) || len(a.store.ReportWatch()) != 0 {
		t.Fatal("старое сообщение сбросило исправленное")
	}

	// лимит: место освобождают исправленные, а не новые сообщения
	var many []store.Report
	for i := 0; i < 1999; i++ {
		many = append(many, store.Report{Kind: "track", ID: strconv.Itoa(1000 + i)})
	}
	a.store.AddReports(many)
	if !a.store.AddReports([]store.Report{{Kind: "track", ID: "5"}}) {
		t.Fatal("новое сообщение не принято при полном списке")
	}
	if list, _ := a.store.ReportsFixed(); len(list) != 0 {
		t.Fatalf("исправленное не вытеснено: %d", len(list))
	}
	if a.store.AddReports([]store.Report{{Kind: "track", ID: "6"}}) {
		t.Fatal("принято сверх лимита, хотя исправленных нет")
	}

	// свой трек без альбома — открыть негде
	if u := a.ReportLink("track", "600000001000099"); u != "" {
		t.Fatalf("ссылка на свой трек без альбома: %q", u)
	}
}

func TestThanksOneRowPerTrack(t *testing.T) {
	t.Setenv("LOCALAPPDATA", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	a := &App{Version: "v9.9.9", store: store.Open(), listeners: map[int]func(){}}
	a.store.AddReports([]store.Report{
		{Kind: "track", ID: "123", Title: "Трек"},
		{Kind: "track", ID: "123", Wrong: true},
		{Kind: "album", ID: "9", Title: "Альбом"},
	})
	a.store.ReportsDone([]store.ReportKey{{Kind: "track", ID: "123"}, {Kind: "album", ID: "9"}}, nil)
	a.store.ReportsDone([]store.ReportKey{{Kind: "track", ID: "123", Wrong: true}}, nil)
	v := a.thanksView()
	if v.Total != 2 || v.Tracks != 1 || v.Albums != 1 || len(v.Items) != 2 || v.New != 2 {
		t.Fatalf("карточка: %+v", v)
	}
	if v.Items[0].ID != "123" || !v.Items[0].Wrong || v.Items[0].Title != "Трек" {
		t.Fatalf("строка трека — по свежему исправлению, название — из сообщения: %+v", v.Items[0])
	}
}
