// Окно программы на macOS. Трей (systray) и окно (WKWebView) на Mac не уживаются в одном
// процессе: обоим нужен главный цикл Cocoa. Поэтому окно — отдельный процесс той же программы
// (флаг --window, см. RunWindowProcess), а этот процесс отдаёт ему состояние и принимает
// действия по локальной связи: 127.0.0.1, случайный порт, токен в аргументах.
package ui

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"time"

	"github.com/S4YN3X/F_ckCensor/internal/app"
	"github.com/S4YN3X/F_ckCensor/internal/winutil"
)

// Window — окно программы: процесс окна запускается при открытии и завершается при закрытии;
// программа при этом остаётся в строке меню.
type Window struct {
	app *app.App
	// OnClosed вызывается после закрытия окна пользователем.
	OnClosed func()

	mu      sync.Mutex
	cond    *sync.Cond
	proc    *os.Process
	port    int
	token   string
	seq     int      // растёт при каждом изменении состояния
	focus   int      // растёт, когда окно надо вывести на передний план
	closing bool     // окно просят закрыть
	toasts  []string // сообщения для окна, ещё не отданные ему
}

func New(a *app.App) *Window {
	w := &Window{app: a}
	w.cond = sync.NewCond(&w.mu)
	return w
}

// Show открывает окно или выводит уже открытое на передний план.
func (w *Window) Show() {
	if err := w.serve(); err != nil {
		fmt.Fprintln(os.Stderr, "[ui] связь с окном:", err)
		return
	}
	w.mu.Lock()
	if w.proc != nil {
		w.focus++
		w.cond.Broadcast()
		w.mu.Unlock()
		return
	}
	w.closing = false
	w.mu.Unlock()

	exe, err := os.Executable()
	if err != nil {
		return
	}
	args := []string{"--window",
		"--ui-port", strconv.Itoa(w.port), "--ui-token", w.token,
		"--ui-zoom", strconv.FormatFloat(w.app.Settings().Zoom(), 'f', 2, 64)}
	// первое окно после запуска открывается заставкой: на ней проверяется обновление, как в Windows
	if w.app.TakeStartupUpdate() {
		args = append(args, "--ui-update")
	}
	cmd := exec.Command(exe, args...)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Start(); err != nil {
		fmt.Fprintln(os.Stderr, "[ui] окно не запустилось:", err)
		return
	}
	w.mu.Lock()
	w.proc = cmd.Process
	w.mu.Unlock()
	go func() {
		_ = cmd.Wait()
		w.mu.Lock()
		w.proc = nil
		w.mu.Unlock()
		if w.OnClosed != nil {
			w.OnClosed()
		}
	}()
}

// ShowMinimized — на Mac окно просто открывается (свёрнутого «кнопкой на панели задач» тут нет).
func (w *Window) ShowMinimized() { w.Show() }

func (w *Window) IsOpen() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.proc != nil
}

// Close закрывает окно (при выходе из программы).
func (w *Window) Close() {
	w.mu.Lock()
	p := w.proc
	w.closing = true
	w.cond.Broadcast()
	w.mu.Unlock()
	if p != nil {
		// процесс окна сам закроется, получив closing; на всякий случай — через секунду
		time.AfterFunc(time.Second, func() { _ = p.Kill() })
	}
}

func (w *Window) toast(msg string) {
	w.mu.Lock()
	w.toasts = append(w.toasts, msg)
	w.cond.Broadcast()
	w.mu.Unlock()
}

// serve запускает локальную связь для окна (один раз).
func (w *Window) serve() error {
	w.mu.Lock()
	if w.port != 0 {
		w.mu.Unlock()
		return nil
	}
	w.mu.Unlock()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	buf := make([]byte, 16)
	_, _ = rand.Read(buf)
	w.mu.Lock()
	w.port, w.token = ln.Addr().(*net.TCPAddr).Port, hex.EncodeToString(buf)
	w.mu.Unlock()
	w.app.Subscribe(func() {
		w.mu.Lock()
		w.seq++
		w.cond.Broadcast()
		w.mu.Unlock()
	})
	// ждущие /wait просыпаются хотя бы раз в 20 секунд — чтобы ответить «ничего нового»
	go func() {
		for range time.Tick(20 * time.Second) {
			w.cond.Broadcast()
		}
	}()

	mux := http.NewServeMux()
	auth := func(h http.HandlerFunc) http.HandlerFunc {
		return func(rw http.ResponseWriter, r *http.Request) {
			if r.Header.Get("X-Token") != w.token {
				http.Error(rw, "forbidden", http.StatusForbidden)
				return
			}
			h(rw, r)
		}
	}
	mux.HandleFunc("/state", auth(func(rw http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(rw).Encode(w.app.Snapshot())
	}))
	mux.HandleFunc("/action", auth(func(rw http.ResponseWriter, r *http.Request) {
		var in struct {
			Name string          `json:"name"`
			Arg  json.RawMessage `json:"arg"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		res := map[string]string{}
		if err := w.action(in.Name, in.Arg); err != nil {
			res["error"] = err.Error()
		}
		_ = json.NewEncoder(rw).Encode(res)
	}))
	mux.HandleFunc("/wait", auth(func(rw http.ResponseWriter, r *http.Request) {
		seen, _ := strconv.Atoi(r.URL.Query().Get("seq"))
		focus, _ := strconv.Atoi(r.URL.Query().Get("focus"))
		deadline := time.Now().Add(20 * time.Second)
		w.mu.Lock()
		for w.seq == seen && w.focus == focus && !w.closing && len(w.toasts) == 0 && time.Now().Before(deadline) {
			w.cond.Wait()
		}
		resp := map[string]any{"seq": w.seq, "focus": w.focus, "close": w.closing, "toasts": w.toasts}
		w.toasts = nil
		changed := w.seq != seen
		w.mu.Unlock()
		if changed {
			resp["state"] = w.app.Snapshot()
		}
		_ = json.NewEncoder(rw).Encode(resp)
	}))
	go func() { _ = http.Serve(ln, mux) }()
	return nil
}

// action выполняет команду из интерфейса — как в окне Windows, без вещей своей рамки окна.
func (w *Window) action(name string, arg json.RawMessage) error {
	var v any
	if len(arg) > 0 {
		_ = json.Unmarshal(arg, &v)
	}
	switch name {
	case "toggle":
		w.app.Toggle()
	case "install":
		w.app.InstallPatch()
	case "remove":
		w.app.RemovePatch()
	case "restartYM":
		w.app.RestartYM()
	case "findYM":
		return w.app.SetYMDir("")
	case "browseYM":
		go func() {
			if dir := winutil.PickFolder(0, "Выберите приложение «Яндекс Музыка»"); dir != "" {
				if err := w.app.SetYMDir(dir); err != nil {
					w.toast(err.Error())
				}
			}
		}()
	case "setting":
		m, _ := v.(map[string]any)
		key, _ := m["key"].(string)
		return w.app.SetSetting(key, m["value"])
	case "reloadList":
		return w.app.ReloadList()
	case "clearHistory":
		w.app.ClearHistory()
	case "checkUpdate":
		w.app.CheckUpdate()
	case "dismissError":
		w.app.DismissError()
	case "startupUpdate":
		w.app.RunStartupUpdate()
	case "skipUpdate":
		w.app.SkipStartupUpdate()
	case "expand", "titlebar":
		// заставку разворачивает сам процесс окна, шапку окна на Mac рисует система
	case "open":
		if url, ok := v.(string); ok && (url == app.RepoURL || len(url) > len(app.RepoURL) && url[:len(app.RepoURL)+1] == app.RepoURL+"/") {
			winutil.OpenURL(url)
		}
	default:
		return fmt.Errorf("неизвестное действие %q", name)
	}
	return nil
}
