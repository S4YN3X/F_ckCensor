package app

import (
	"os"
	"time"
)

// Файл программы в релизе. Под старым названием (FkCensor.exe) его ищут прежние версии,
// поэтому какое-то время в релиз кладутся оба файла; новая версия берёт любой
var releaseAssets = []string{"F_ckCensor.exe", "FkCensor.exe"}

func updateDownloadPath(exe string) string { return exe + ".new" }

// installUpdate ставит новый exe на место запущенного. Запущенный файл Windows не даёт
// удалить, но даёт переименовать — он уходит в «F_ckCensor.exe~».
func installUpdate(exe, newPath string) error {
	old := exe + "~"
	_ = os.Remove(old)
	if err := os.Rename(exe, old); err != nil {
		return err
	}
	if err := os.Rename(newPath, exe); err != nil {
		_ = os.Rename(old, exe)
		return err
	}
	return nil
}

// CleanupOldExe удаляет «F_ckCensor.exe~», оставшийся от обновления, когда старая копия
// программы уже закрылась.
func CleanupOldExe() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	go func() {
		for i := 0; i < 30; i++ {
			if err := os.Remove(exe + "~"); err == nil || os.IsNotExist(err) {
				return
			}
			time.Sleep(time.Second)
		}
	}()
}
