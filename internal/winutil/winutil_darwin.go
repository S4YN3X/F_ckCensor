// Системные мелочи macOS — то же, что winutil даёт программе в Windows: автозапуск,
// уведомления, тема, открытие ссылок, выбор приложения. Окно на Mac — в internal/ui (свой процесс).
package winutil

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// EnableDPIAwareness — в macOS масштаб экрана система учитывает сама.
func EnableDPIAwareness() {}

// Scale — размеры в macOS в точках, а не в пикселях: пересчёт не нужен.
func Scale(px int) int { return px }

const launchAgentLabel = "ru.s4yn3x.fckcensor"

func launchAgentPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "LaunchAgents", launchAgentLabel+".plist")
}

// SetAutostart включает или выключает запуск при входе в систему (LaunchAgent, в трей).
func SetAutostart(on bool) error {
	path := launchAgentPath()
	if !on {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if p, err := filepath.EvalSymlinks(exe); err == nil {
		exe = p
	}
	plist := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>` + launchAgentLabel + `</string>
	<key>ProgramArguments</key>
	<array>
		<string>` + xmlEscape(exe) + `</string>
		<string>--tray</string>
	</array>
	<key>RunAtLoad</key>
	<true/>
</dict>
</plist>
`
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(plist), 0o644)
}

func xmlEscape(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;")
	return r.Replace(s)
}

// RegisterNotifications — в macOS регистрировать ничего не нужно.
func RegisterNotifications(ico []byte) {}

// appleScriptString — строка для AppleScript в кавычках.
func appleScriptString(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

// Notify показывает системное уведомление.
func Notify(title, body string) {
	_ = exec.Command("osascript", "-e",
		fmt.Sprintf("display notification %s with title %s", appleScriptString(body), appleScriptString(title))).Run()
}

// OpenURL открывает ссылку в браузере по умолчанию.
func OpenURL(url string) { _ = OpenLink(url) }

// OpenLink — то же, с ошибкой: например, ссылку yandexmusic:// некому открыть.
func OpenLink(url string) error { return exec.Command("open", url).Run() }

// PickFolder — выбор приложения Яндекс Музыки (на Mac это не папка, а .app). owner не используется.
func PickFolder(owner uintptr, title string) string {
	script := fmt.Sprintf(`POSIX path of (choose file of type {"com.apple.application-bundle"} with prompt %s default location (path to applications folder))`,
		appleScriptString(title))
	out, err := exec.Command("osascript", "-e", script).Output()
	if err != nil {
		return "" // отменили выбор
	}
	return strings.TrimSuffix(strings.TrimSpace(string(out)), "/")
}

// AppsDark — тёмное оформление macOS.
func AppsDark() bool {
	out, err := exec.Command("defaults", "read", "-g", "AppleInterfaceStyle").Output()
	return err == nil && strings.Contains(strings.ToLower(string(out)), "dark")
}

// TaskbarDark — строка меню в macOS тёмная вместе с оформлением системы.
func TaskbarDark() bool { return AppsDark() }
