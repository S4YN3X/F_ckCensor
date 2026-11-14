// Процесс окна на macOS: тот же интерфейс (assets/ui) в WKWebView. Вызовы страницы
// (fkcState, fkcAction) уходят основному процессу программы, обновления (fkcPush) приходят
// от него же — см. window_darwin.go.
package ui

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework Cocoa
#import <Cocoa/Cocoa.h>

// Меню: без «Правки» в WKWebView не работают ⌘C, ⌘V, ⌘A. Пока окно открыто, у программы
// есть значок в Dock (основной процесс живёт только в строке меню).
static void fkcSetupApp(void) {
	[NSApp setActivationPolicy:NSApplicationActivationPolicyRegular];
	NSMenu *bar = [[NSMenu alloc] init];

	NSMenuItem *appItem = [[NSMenuItem alloc] init];
	NSMenu *appMenu = [[NSMenu alloc] initWithTitle:@"F_ckCensor"];
	[appMenu addItemWithTitle:@"Скрыть F_ckCensor" action:@selector(hide:) keyEquivalent:@"h"];
	[appMenu addItem:[NSMenuItem separatorItem]];
	[appMenu addItemWithTitle:@"Закрыть окно" action:@selector(performClose:) keyEquivalent:@"w"];
	[appItem setSubmenu:appMenu];
	[bar addItem:appItem];

	NSMenuItem *editItem = [[NSMenuItem alloc] init];
	NSMenu *edit = [[NSMenu alloc] initWithTitle:@"Правка"];
	[edit addItemWithTitle:@"Отменить" action:@selector(undo:) keyEquivalent:@"z"];
	[edit addItemWithTitle:@"Повторить" action:@selector(redo:) keyEquivalent:@"Z"];
	[edit addItem:[NSMenuItem separatorItem]];
	[edit addItemWithTitle:@"Вырезать" action:@selector(cut:) keyEquivalent:@"x"];
	[edit addItemWithTitle:@"Копировать" action:@selector(copy:) keyEquivalent:@"c"];
	[edit addItemWithTitle:@"Вставить" action:@selector(paste:) keyEquivalent:@"v"];
	[edit addItemWithTitle:@"Выделить всё" action:@selector(selectAll:) keyEquivalent:@"a"];
	[editItem setSubmenu:edit];
	[bar addItem:editItem];

	[NSApp setMainMenu:bar];
}

static void fkcActivate(void *win) {
	[NSApp activateIgnoringOtherApps:YES];
	if (win) {
		NSWindow *w = (__bridge NSWindow *)win;
		[w center];
		[w makeKeyAndOrderFront:nil];
	}
}
*/
import "C"

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"time"

	webview "github.com/webview/webview_go"

	"github.com/S4YN3X/F_ckCensor/assets"
)

const (
	width, height       = 980, 640
	minWidth, minHeight = 760, 560
)

// RunWindowProcess — точка входа процесса окна (флаг --window).
func RunWindowProcess(args []string) {
	runtime.LockOSThread()
	fs := flag.NewFlagSet("window", flag.ContinueOnError)
	port := fs.Int("ui-port", 0, "")
	token := fs.String("ui-token", "", "")
	zoom := fs.Float64("ui-zoom", 1, "")
	_ = fs.Parse(args)
	if *port == 0 || *token == "" {
		fmt.Fprintln(os.Stderr, "[ui] окно запущено без связи с программой")
		return
	}
	base := "http://127.0.0.1:" + strconv.Itoa(*port)
	client := &http.Client{Timeout: 30 * time.Second}
	call := func(path string, body any) ([]byte, error) {
		var rd io.Reader
		if body != nil {
			b, _ := json.Marshal(body)
			rd = bytes.NewReader(b)
		}
		req, _ := http.NewRequest("POST", base+path, rd)
		req.Header.Set("X-Token", *token)
		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			return nil, fmt.Errorf("программа ответила %d", resp.StatusCode)
		}
		return io.ReadAll(resp.Body)
	}

	w := webview.New(false)
	if w == nil {
		fmt.Fprintln(os.Stderr, "[ui] WKWebView недоступен")
		return
	}
	defer w.Destroy()
	C.fkcSetupApp()
	w.SetTitle("F_ckCensor")
	w.SetSize(int(float64(width)*(*zoom)), int(float64(height)*(*zoom)), webview.HintNone)
	w.SetSize(int(float64(minWidth)*(*zoom)), int(float64(minHeight)*(*zoom)), webview.HintMin)

	_ = w.Bind("fkcState", func() (json.RawMessage, error) {
		b, err := call("/state", nil)
		return json.RawMessage(b), err
	})
	_ = w.Bind("fkcAction", func(name string, arg json.RawMessage) error {
		b, err := call("/action", map[string]any{"name": name, "arg": arg})
		if err != nil {
			return err
		}
		var res struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(b, &res)
		if res.Error != "" {
			return fmt.Errorf("%s", res.Error)
		}
		return nil
	})
	// своей шапки окна на Mac нет: рамку и кнопки рисует система
	_ = w.Bind("fkcWindow", func(cmd string) bool { return false })

	w.Init(fmt.Sprintf(`window.fkcZoom = %g; window.fkcPlatform = 'mac';
document.addEventListener('DOMContentLoaded', function () {
  var s = document.createElement('style');
  s.textContent = ':root{--tb:0px !important} #titlebar{display:none !important}';
  document.head.appendChild(s);
  document.documentElement.classList.add('mac');
});`, *zoom))
	w.SetHtml(assets.UIPage())
	C.fkcActivate(w.Window())

	// обновления от программы: новое состояние, сообщения, «на передний план», «закрыться»
	go func() {
		seq, focus := -1, 0
		long := &http.Client{Timeout: 40 * time.Second}
		for {
			req, _ := http.NewRequest("GET", base+"/wait?seq="+strconv.Itoa(seq)+"&focus="+strconv.Itoa(focus), nil)
			req.Header.Set("X-Token", *token)
			resp, err := long.Do(req)
			if err != nil {
				// программа закрылась — закрываем и окно
				w.Dispatch(w.Terminate)
				return
			}
			var r struct {
				Seq    int             `json:"seq"`
				Focus  int             `json:"focus"`
				Close  bool            `json:"close"`
				Toasts []string        `json:"toasts"`
				State  json.RawMessage `json:"state"`
			}
			_ = json.NewDecoder(resp.Body).Decode(&r)
			resp.Body.Close()
			if r.Close {
				w.Dispatch(w.Terminate)
				return
			}
			raise := r.Focus != focus && focus != 0
			seq, focus = r.Seq, r.Focus
			state, toasts := r.State, r.Toasts
			w.Dispatch(func() {
				if len(state) > 0 {
					w.Eval("window.fkcPush && window.fkcPush(" + string(state) + ")")
				}
				for _, t := range toasts {
					b, _ := json.Marshal(t)
					w.Eval("window.fkcToast && window.fkcToast(" + string(b) + ")")
				}
				if raise {
					C.fkcActivate(w.Window())
				}
			})
		}
	}()

	w.Run()
}
