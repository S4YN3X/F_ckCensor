package app

// «Спасибо, вы помогли»: пользователь сообщил о цензуре, модераторы добавили трек в базу
// (или поменяли файл замены, на которую жаловались) — на главной появляется карточка.
// Сообщения присылает скрипт в Яндекс Музыке, он же проверяет их по списку замен;
// программа хранит, что исправлено, и показывает это.

import (
	"errors"
	"strings"
	"sync/atomic"
	"time"

	"github.com/S4YN3X/F_ckCensor/internal/ipc"
	"github.com/S4YN3X/F_ckCensor/internal/store"
	"github.com/S4YN3X/F_ckCensor/internal/winutil"
)

// watchEvery — как часто напоминать клиенту список на проверку, даже если он не менялся
// (клиент мог перезапуститься и забыть его)
const watchEvery = time.Minute

// ThanksItem — исправленный трек или альбом в карточке.
type ThanksItem struct {
	Kind    string `json:"kind"`
	ID      string `json:"id"`
	Title   string `json:"title"`
	Artists string `json:"artists"`
	Cover   string `json:"cover"`
	Wrong   bool   `json:"wrong"` // исправили замену, на которую жаловались
	New     bool   `json:"new"`
}

// ThanksView — карточка «спасибо» на главной. Считаются треки и альбомы, а не сообщения:
// о треке могли и сообщить о цензуре, и пожаловаться на замену — в карточке он один.
type ThanksView struct {
	Visible bool         `json:"visible"`
	Total   int          `json:"total"`
	Tracks  int          `json:"tracks"`
	Albums  int          `json:"albums"`
	New     int          `json:"new"`
	Items   []ThanksItem `json:"items"`
}

// syncReports — часть обмена с клиентом: новые сообщения, исправленные, список на проверку.
func (a *App) syncReports(req ipc.SyncRequest, res *ipc.SyncResponse) (changed bool) {
	added := a.store.AddReports(req.Reports)
	if added {
		go a.fillReportInfo()
	}
	if a.store.ReportsDone(req.ReportsDone, req.ReportsBase) {
		changed = true
	}
	a.mu.Lock()
	send := added || changed || len(req.ReportsBase) > 0 || time.Since(a.watchSent) > watchEvery
	if send {
		a.watchSent = time.Now()
	}
	a.mu.Unlock()
	if send {
		res.Watch = a.store.ReportWatch()
		if res.Watch == nil {
			res.Watch = []store.ReportKey{} // пустой список — тоже ответ: проверять нечего
		}
	}
	return changed
}

// fillingInfo — подтягивание названий уже идёт: второе не запускаем
var fillingInfo atomic.Bool

// fillReportInfo подтягивает названия старых сообщений (до этой версии скрипт помнил только id).
// Одновременно идёт одно; найденное записывается пачками, не по файлу на трек.
func (a *App) fillReportInfo() {
	if !fillingInfo.CompareAndSwap(false, true) {
		return
	}
	defer fillingInfo.Store(false)
	found := map[string]store.ReportInfo{}
	flush := func() {
		if len(found) > 0 {
			a.store.SetReportsInfo(found)
			found = map[string]store.ReportInfo{}
			a.changed()
		}
	}
	for _, k := range a.store.ReportsNoTitle() {
		if k.Kind == "track" && isCustomTrackID(k.ID) {
			continue // своего трека в Яндекс Музыке нет — название придёт из базы, когда его добавят
		}
		var m trackMeta
		var err error
		if k.Kind == "album" {
			m, err = fetchAlbum(k.ID)
		} else {
			m, err = fetchTrack(k.ID)
		}
		if err == nil && m.Title != "" {
			found[k.Kind+":"+k.ID] = store.ReportInfo{Title: m.Title, Artists: m.Artist, Cover: m.Cover, AlbumID: m.AlbumID}
			if len(found) >= 25 {
				flush()
			}
		}
		// и после ошибки: если API отвечает ошибками, не засыпаем его запросами
		time.Sleep(200 * time.Millisecond)
	}
	flush()
}

func (a *App) thanksView() *ThanksView {
	list, visible := a.store.ReportsFixed()
	if len(list) == 0 {
		return nil
	}
	v := &ThanksView{Visible: visible}
	at := map[string]int{}        // трек или альбом → его строка (самое свежее исправление — первым)
	untitled := map[string]bool{} // у строки пока название-заглушка
	for _, e := range list {
		k := e.Kind + ":" + e.ID
		if i, ok := at[k]; ok {
			if untitled[k] && e.Title != "" {
				v.Items[i].Title, untitled[k] = e.Title, false
			}
			// тот же трек, исправленный раньше: новизна — если новое хоть одно из его сообщений
			if !e.Seen && !v.Items[i].New {
				v.Items[i].New = true
				v.New++
			}
			if v.Items[i].Artists == "" {
				v.Items[i].Artists = e.Artists
			}
			if v.Items[i].Cover == "" {
				v.Items[i].Cover = coverURL(e.Cover)
			}
			continue
		}
		title := e.Title
		if title == "" {
			untitled[k] = true
			if e.Kind == "album" {
				title = "альбом " + e.ID
			} else {
				title = "трек " + e.ID
			}
		}
		it := ThanksItem{Kind: e.Kind, ID: e.ID, Title: title, Artists: e.Artists, Cover: coverURL(e.Cover), Wrong: e.Wrong, New: !e.Seen}
		if it.New {
			v.New++
		}
		if e.Kind == "album" {
			v.Albums++
		} else {
			v.Tracks++
		}
		at[k] = len(v.Items)
		v.Items = append(v.Items, it)
	}
	v.Total = len(v.Items)
	return v
}

// ThanksSeen — список посмотрели: «новых» больше нет.
func (a *App) ThanksSeen() {
	if a.store.ReportsSeen() {
		a.changed()
	}
}

// ThanksHide — карточку закрыли: до следующего исправления.
func (a *App) ThanksHide() {
	if a.store.ReportsHide() {
		a.changed()
	}
}

// OpenReport открывает исправленный трек или альбом из карточки «спасибо» в Яндекс Музыке.
func (a *App) OpenReport(kind, id string) error {
	u := a.ReportLink(kind, id)
	if u == "" {
		return errors.New("не нашлось, где открыть этот трек — найдите его в яндекс музыке поиском")
	}
	if err := winutil.OpenLink(u); err != nil {
		return errors.New("яндекс музыка не открылась — найдите трек в ней поиском")
	}
	return nil
}

// ReportLink — где открыть исправленный трек или альбом: в приложении Яндекс Музыки
// (свой и удалённый трек — в его альбоме из базы: вне его в Яндекс Музыке такого трека нет).
func (a *App) ReportLink(kind, id string) string {
	if !store.ValidReportID(id) {
		return ""
	}
	if kind == "album" {
		return "yandexmusic://album/" + id
	}
	list, _ := a.store.ReportsFixed()
	for _, e := range list {
		if e.Kind == "track" && e.ID == id && e.AlbumID != "" && strings.Trim(e.AlbumID, "0123456789") == "" {
			return "yandexmusic://album/" + e.AlbumID + "/track/" + id
		}
	}
	if isCustomTrackID(id) {
		return "" // свой трек без альбома: в Яндекс Музыке его не найти
	}
	return "https://music.yandex.ru/track/" + id
}
