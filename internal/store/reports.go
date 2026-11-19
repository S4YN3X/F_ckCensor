package store

import (
	"sort"
	"time"
)

// Сообщения о цензуре, которые отправил пользователь. Когда трек (или альбом) появляется в базе
// замен — или у замены, на которую жаловались, меняется файл, — программа говорит «спасибо,
// вы помогли». Проверяет скрипт в Яндекс Музыке: у него есть список замен.

// Report — сообщение, как его присылает скрипт.
type Report struct {
	Kind    string `json:"kind"` // track | album
	ID      string `json:"id"`
	Wrong   bool   `json:"wrong"` // «в замене цензура»: трек уже был заменён
	Title   string `json:"title,omitempty"`
	Artists string `json:"artists,omitempty"`
	Cover   string `json:"cover,omitempty"`
	AlbumID string `json:"albumId,omitempty"`
	// URL — файл замены на момент жалобы (только для Wrong): другой — значит, исправили
	URL string `json:"url,omitempty"`
	// Old — отправлено до версии с благодарностями (известен только id)
	Old bool `json:"old,omitempty"`
}

// ReportKey — какое сообщение проверить или какое уже исправлено.
type ReportKey struct {
	Kind  string `json:"kind"`
	ID    string `json:"id"`
	Wrong bool   `json:"wrong"`
	URL   string `json:"url,omitempty"`
	// у исправленного — название, исполнители и обложка из базы замен (удалённого трека Яндекс не отдаст)
	Title   string `json:"title,omitempty"`
	Artists string `json:"artists,omitempty"`
	Cover   string `json:"cover,omitempty"`
	AlbumID string `json:"albumId,omitempty"`
}

// ReportEntry — сообщение в настройках программы.
type ReportEntry struct {
	Report
	At     time.Time `json:"at"`
	DoneAt time.Time `json:"doneAt,omitempty"`
	// Seen — показано в окне «подробнее»; Hidden — карточку закрыли крестиком
	Seen   bool `json:"seen,omitempty"`
	Hidden bool `json:"hidden,omitempty"`
}

// ReportInfo — название, исполнители, обложка и альбом, найденные для сообщения.
type ReportInfo struct {
	Title, Artists, Cover, AlbumID string
}

func (r Report) key() string {
	k := r.Kind + ":" + r.ID
	if r.Wrong {
		k += ":wrong"
	}
	return k
}

func (k ReportKey) key() string { return Report{Kind: k.Kind, ID: k.ID, Wrong: k.Wrong}.key() }

const reportsLimit = 2000

// ValidReportID — id трека или альбома из Яндекс Музыки: только цифры (он попадает в адреса API).
func ValidReportID(id string) bool {
	if id == "" || len(id) > 20 {
		return false
	}
	for _, c := range id {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// fill дописывает пустые поля; вернёт, поменялось ли что-то.
func (e *ReportEntry) fill(title, artists, cover, albumID string) bool {
	changed := false
	set := func(dst *string, v string) {
		if *dst == "" && v != "" {
			*dst, changed = v, true
		}
	}
	if e.Title == "" && title != "" {
		e.Title, changed = title, true
		set(&e.Artists, artists)
	}
	set(&e.Cover, cover)
	set(&e.AlbumID, albumID)
	return changed
}

// AddReports запоминает присланные сообщения. Вернёт true, если появилось что-то новое для
// проверки: новое сообщение или повторное о том, что уже было исправлено (например, в новой
// замене опять цензура) — тогда его снова ждём.
func (s *Store) AddReports(list []Report) (added bool) {
	if len(list) == 0 {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	idx := map[string]int{}
	for i, e := range s.d.Reports {
		idx[e.key()] = i
	}
	dirty := false
	for _, r := range list {
		if !ValidReportID(r.ID) || (r.Kind != "track" && r.Kind != "album") {
			continue
		}
		if i, ok := idx[r.key()]; ok {
			e := &s.d.Reports[i]
			if e.fill(r.Title, r.Artists, r.Cover, r.AlbumID) {
				dirty = true
			}
			// повторное сообщение (не перенос старых) о том, что уже исправлено, — снова на проверку
			if !r.Old && !e.DoneAt.IsZero() {
				e.DoneAt, e.Seen, e.Hidden, e.At = time.Time{}, false, false, time.Now()
				// какой файл стоит сейчас, клиент сообщит при первой проверке (пусто — запомнит нынешний)
				e.URL = r.URL
				dirty, added = true, true
			} else if e.URL == "" && r.URL != "" {
				e.URL, dirty = r.URL, true
			}
			continue
		}
		if len(s.d.Reports) >= reportsLimit && !s.evictOneLocked() {
			continue // все записи ещё ждут исправления — места нет
		}
		idx[r.key()] = len(s.d.Reports)
		s.d.Reports = append(s.d.Reports, ReportEntry{Report: r, At: time.Now()})
		dirty, added = true, true
	}
	if dirty {
		s.save()
	}
	return added
}

// evictOneLocked освобождает место: убирает самое давнее исправленное сообщение
// (сначала просмотренные). Вернёт false, если исправленных нет.
func (s *Store) evictOneLocked() bool {
	best := -1
	for i, e := range s.d.Reports {
		if e.DoneAt.IsZero() {
			continue
		}
		if best < 0 || (e.Seen && !s.d.Reports[best].Seen) ||
			(e.Seen == s.d.Reports[best].Seen && e.DoneAt.Before(s.d.Reports[best].DoneAt)) {
			best = i
		}
	}
	if best < 0 {
		return false
	}
	s.d.Reports = append(s.d.Reports[:best], s.d.Reports[best+1:]...)
	return true
}

// ReportWatch — что ещё ждёт исправления: скрипт проверит это по списку замен.
func (s *Store) ReportWatch() []ReportKey {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []ReportKey
	for _, e := range s.d.Reports {
		if e.DoneAt.IsZero() {
			out = append(out, ReportKey{Kind: e.Kind, ID: e.ID, Wrong: e.Wrong, URL: e.URL})
		}
	}
	return out
}

// ReportsDone отмечает исправленные; base — нынешний файл замены для старых жалоб на замену.
// Вернёт, появилось ли что-то новое для карточки.
func (s *Store) ReportsDone(done, base []ReportKey) (changed bool) {
	if len(done) == 0 && len(base) == 0 {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	d := make(map[string]ReportKey, len(done))
	for _, k := range done {
		d[k.key()] = k
	}
	b := make(map[string]string, len(base))
	for _, k := range base {
		b[k.key()] = k.URL
	}
	now := time.Now()
	dirty := false
	for i := range s.d.Reports {
		e := &s.d.Reports[i]
		if !e.DoneAt.IsZero() {
			continue
		}
		if k, ok := d[e.key()]; ok {
			e.DoneAt, changed, dirty = now, true, true
			e.fill(k.Title, k.Artists, k.Cover, "")
			// альбом из базы главнее: трек открывается в нём (своего трека в Яндекс Музыке нет)
			if k.AlbumID != "" {
				e.AlbumID = k.AlbumID
			}
		} else if u, ok := b[e.key()]; ok && e.URL == "" && u != "" {
			e.URL, dirty = u, true
		}
	}
	if changed {
		// новое исправление — карточка снова видна со всеми исправленными
		for i := range s.d.Reports {
			s.d.Reports[i].Hidden = false
		}
	}
	if dirty {
		s.save()
	}
	return changed
}

// ReportsFixed — исправленные сообщения, новые сверху; visible — показывать ли карточку.
func (s *Store) ReportsFixed() (list []ReportEntry, visible bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, e := range s.d.Reports {
		if !e.DoneAt.IsZero() {
			list = append(list, e)
			if !e.Hidden {
				visible = true
			}
		}
	}
	sort.SliceStable(list, func(i, j int) bool { return list[i].DoneAt.After(list[j].DoneAt) })
	return list, visible
}

// ReportsSeen — список открыли и закрыли «понятно»: новых больше нет. Вернёт, поменялось ли что-то.
func (s *Store) ReportsSeen() bool {
	return s.updateFixed(func(e *ReportEntry) bool {
		if e.Seen {
			return false
		}
		e.Seen = true
		return true
	})
}

// ReportsHide — карточку закрыли крестиком: до следующего исправления. Вернёт, поменялось ли что-то.
func (s *Store) ReportsHide() bool {
	return s.updateFixed(func(e *ReportEntry) bool {
		if e.Seen && e.Hidden {
			return false
		}
		e.Seen, e.Hidden = true, true
		return true
	})
}

func (s *Store) updateFixed(f func(*ReportEntry) bool) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	changed := false
	for i := range s.d.Reports {
		if !s.d.Reports[i].DoneAt.IsZero() && f(&s.d.Reports[i]) {
			changed = true
		}
	}
	if changed {
		s.save()
	}
	return changed
}

// ReportsNoTitle — треки и альбомы без названия (старые сообщения): программа подтянет его
// из Яндекс Музыки. Каждый — один раз, даже если о нём есть и обычное сообщение, и жалоба на замену.
func (s *Store) ReportsNoTitle() []ReportKey {
	s.mu.Lock()
	defer s.mu.Unlock()
	seen := map[string]bool{}
	var out []ReportKey
	for _, e := range s.d.Reports {
		k := e.Kind + ":" + e.ID
		if e.Title == "" && !seen[k] {
			seen[k] = true
			out = append(out, ReportKey{Kind: e.Kind, ID: e.ID})
		}
	}
	return out
}

// SetReportsInfo дописывает найденные сведения разом (ключ — kind:id) — одна запись на диск.
func (s *Store) SetReportsInfo(info map[string]ReportInfo) {
	if len(info) == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	changed := false
	for i := range s.d.Reports {
		e := &s.d.Reports[i]
		if in, ok := info[e.Kind+":"+e.ID]; ok && e.fill(in.Title, in.Artists, in.Cover, in.AlbumID) {
			changed = true
		}
	}
	if changed {
		s.save()
	}
}
