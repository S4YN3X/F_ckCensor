// Package ym находит, запускает и закрывает десктопную Яндекс Музыку (macOS).
//
// На Mac «папка установки» — само приложение: /Applications/Яндекс Музыка.app.
package ym

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// ExePath — исполняемый файл клиента внутри приложения.
func ExePath(app string) string {
	return filepath.Join(app, "Contents", "MacOS", strings.TrimSuffix(filepath.Base(app), ".app"))
}

// ASARPath — архив интерфейса клиента.
func ASARPath(app string) string { return filepath.Join(app, "Contents", "Resources", "app.asar") }

// IntegrityPath — где клиент хранит контрольную сумму app.asar: в Info.plist
// (ключ ElectronAsarIntegrity). Сумма — строка той же длины, её меняют на месте.
func IntegrityPath(app string) string { return filepath.Join(app, "Contents", "Info.plist") }

// Valid проверяет, что это приложение Яндекс Музыки.
func Valid(app string) bool {
	if app == "" {
		return false
	}
	if _, err := os.Stat(ASARPath(app)); err != nil {
		return false
	}
	_, err := os.Stat(IntegrityPath(app))
	return err == nil
}

// FindDir ищет приложение в «Программах» системы и пользователя.
func FindDir() (string, error) {
	home, _ := os.UserHomeDir()
	dirs := []string{"/Applications", filepath.Join(home, "Applications")}
	for _, dir := range dirs {
		for _, name := range []string{"Яндекс Музыка.app", "Yandex Music.app"} {
			if app := filepath.Join(dir, name); Valid(app) {
				return app, nil
			}
		}
	}
	for _, dir := range dirs {
		entries, _ := os.ReadDir(dir)
		for _, e := range entries {
			n := strings.ToLower(e.Name())
			isYandex := strings.Contains(n, "yandex") || strings.Contains(n, "яндекс")
			isMusic := strings.Contains(n, "муз") || strings.Contains(n, "music")
			if strings.HasSuffix(n, ".app") && isYandex && isMusic {
				if app := filepath.Join(dir, e.Name()); Valid(app) {
					return app, nil
				}
			}
		}
	}
	return "", fmt.Errorf("яндекс музыка не найдена в «Программах»")
}

// процессы клиента: главный и помощники — все внутри …app/Contents/
const procPattern = ".app/Contents/MacOS/"

func clientProcs() bool {
	out, err := exec.Command("pgrep", "-fl", procPattern).Output()
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(out), "\n") {
		l := strings.ToLower(line)
		if strings.Contains(l, "яндекс музыка") || strings.Contains(l, "yandex music") {
			return true
		}
	}
	return false
}

// IsRunning проверяет, запущен ли клиент.
func IsRunning() bool { return clientProcs() }

// Kill закрывает клиент (сначала по-хорошему) и ждёт, пока он завершится.
func Kill() {
	for _, name := range []string{"Яндекс Музыка", "Yandex Music"} {
		_ = exec.Command("osascript", "-e", fmt.Sprintf("quit app %q", name)).Run()
	}
	for i := 0; i < 20 && IsRunning(); i++ {
		time.Sleep(250 * time.Millisecond)
	}
	if IsRunning() {
		_ = exec.Command("pkill", "-f", "Яндекс Музыка.app/Contents/").Run()
		_ = exec.Command("pkill", "-f", "Yandex Music.app/Contents/").Run()
		for i := 0; i < 20 && IsRunning(); i++ {
			time.Sleep(250 * time.Millisecond)
		}
	}
	time.Sleep(500 * time.Millisecond)
}

// Launch запускает клиент.
func Launch(app string) error {
	if err := exec.Command("open", app).Run(); err != nil {
		return fmt.Errorf("запуск яндекс музыки: %w", err)
	}
	return nil
}

// AfterPatch переподписывает приложение после изменения app.asar и Info.plist: подпись Яндекса
// с ними больше не сходится, и macOS не запустит клиент. Подпись — локальная (без сертификата),
// с теми же правами приложения (entitlements), что у оригинала: без них подмена не работает.
// Переподписывается только само приложение, вложенные помощники остаются с подписью Яндекса.
func AfterPatch(app string) error {
	ent := filepath.Join(os.TempDir(), "fkc-entitlements.plist")
	defer os.Remove(ent)
	out, err := exec.Command("codesign", "-d", "--entitlements", ":-", app).Output()
	args := []string{"--force", "--sign", "-"}
	if err == nil && len(strings.TrimSpace(string(out))) > 0 {
		if err := os.WriteFile(ent, out, 0o600); err != nil {
			return err
		}
		args = append(args, "--entitlements", ent)
	}
	if out, err := exec.Command("codesign", append(args, app)...).CombinedOutput(); err != nil {
		return explain(fmt.Errorf("переподпись: %v: %s", err, strings.TrimSpace(string(out))))
	}
	// локальная подпись не заверена Apple: с пометкой «скачано из интернета» Gatekeeper не запустит
	_ = exec.Command("xattr", "-dr", "com.apple.quarantine", app).Run()
	return nil
}

// ErrNoAppManagement — macOS не даёт менять другое приложение без разрешения.
var ErrNoAppManagement = errors.New("macOS не даёт изменить Яндекс Музыку: разрешите F_ckCensor " +
	"«Управление приложениями» — Системные настройки → Конфиденциальность и безопасность → " +
	"Управление приложениями, затем повторите")

// Explain превращает ошибку доступа в понятную подсказку про «Управление приложениями».
func Explain(err error) error { return explain(err) }

func explain(err error) error {
	if err == nil {
		return nil
	}
	s := strings.ToLower(err.Error())
	if errors.Is(err, os.ErrPermission) || strings.Contains(s, "operation not permitted") || strings.Contains(s, "permission denied") {
		// открываем нужный раздел настроек, чтобы не искать
		_ = exec.Command("open", "x-apple.systempreferences:com.apple.preference.security?Privacy_AppBundles").Run()
		return fmt.Errorf("%w (%v)", ErrNoAppManagement, err)
	}
	return err
}
