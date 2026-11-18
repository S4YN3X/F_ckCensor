package app

// Обновление на Mac: в релизе F_ckCensor-mac.zip с готовым F_ckCensor.app. Программа скачивает
// его рядом со своим приложением (обычно в «Программы»), распаковывает системной ditto
// и меняет приложение целиком: старое — в скрытое «.F_ckCensor.app.old» рядом, новое — на его место.
// Скачивает сама программа, а не браузер, — метки «из интернета» на файле нет, и macOS
// не спрашивает при запуске; менять приложение в «Программах» позволяет разрешение
// «Управление приложениями», которое программа уже получила для патча.

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

var releaseAssets = []string{"F_ckCensor-mac.zip"}

const (
	updateZipName = ".F_ckCensor-update.zip"
	updateDirName = ".F_ckCensor-update"
	oldAppName    = ".F_ckCensor.app.old"
)

// appBundle — путь к F_ckCensor.app, из которого запущена программа.
func appBundle(exe string) (string, error) {
	if p, err := filepath.EvalSymlinks(exe); err == nil {
		exe = p
	}
	i := strings.Index(exe, ".app/Contents/MacOS/")
	if i < 0 {
		return "", errors.New("программа запущена не из приложения F_ckCensor.app")
	}
	bundle := exe[:i+len(".app")]
	// из «Загрузок» без переноса macOS запускает копию на диске только для чтения,
	// из .dmg — прямо с образа: заменить такое приложение нельзя
	if strings.Contains(bundle, "/AppTranslocation/") || strings.HasPrefix(bundle, "/Volumes/") {
		return "", errors.New("перетащите F_ckCensor в «Программы» и запустите оттуда — так он сможет обновляться")
	}
	return bundle, nil
}

func updateDownloadPath(exe string) string {
	if bundle, err := appBundle(exe); err == nil {
		return filepath.Join(filepath.Dir(bundle), updateZipName)
	}
	return exe + ".update.zip"
}

func installUpdate(exe, zipPath string) error {
	defer os.Remove(zipPath)
	bundle, err := appBundle(exe)
	if err != nil {
		return err
	}
	dir := filepath.Dir(bundle)
	work := filepath.Join(dir, updateDirName)
	_ = os.RemoveAll(work)
	defer os.RemoveAll(work)
	if out, err := exec.Command("ditto", "-x", "-k", zipPath, work).CombinedOutput(); err != nil {
		return fmt.Errorf("не распаковался архив: %v %s", err, strings.TrimSpace(string(out)))
	}
	newApp := filepath.Join(work, "F_ckCensor.app")
	if _, err := os.Stat(filepath.Join(newApp, "Contents", "MacOS", "F_ckCensor")); err != nil {
		return errors.New("в архиве нет F_ckCensor.app")
	}
	_ = exec.Command("xattr", "-dr", "com.apple.quarantine", newApp).Run()

	old := filepath.Join(dir, oldAppName)
	_ = os.RemoveAll(old)
	if err := os.Rename(bundle, old); err != nil {
		return explainMacUpdate(err)
	}
	if err := os.Rename(newApp, bundle); err != nil {
		_ = os.Rename(old, bundle)
		return explainMacUpdate(err)
	}
	return nil
}

func explainMacUpdate(err error) error {
	if errors.Is(err, fs.ErrPermission) {
		return errors.New("macOS не дала заменить приложение — разрешите F_ckCensor «Управление приложениями»: " +
			"Системные настройки → Конфиденциальность и безопасность")
	}
	return err
}

// CleanupOldExe удаляет прежнюю версию приложения, оставшуюся от обновления.
func CleanupOldExe() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	bundle, err := appBundle(exe)
	if err != nil {
		return
	}
	go func() {
		// старая копия ещё закрывается — даём ей пару секунд
		time.Sleep(3 * time.Second)
		_ = os.RemoveAll(filepath.Join(filepath.Dir(bundle), oldAppName))
	}()
}
