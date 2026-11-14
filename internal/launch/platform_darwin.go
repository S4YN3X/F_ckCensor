package launch

import (
	"os"
	"slices"

	"github.com/S4YN3X/F_ckCensor/internal/ui"
)

// windowProcess — на Mac окно живёт в отдельном процессе той же программы (флаг --window):
// трей и окно не уживаются в одном процессе. Если это он — показываем окно и выходим.
func windowProcess() bool {
	if !slices.Contains(os.Args[1:], "--window") {
		return false
	}
	var rest []string
	for _, a := range os.Args[1:] {
		if a != "--window" {
			rest = append(rest, a)
		}
	}
	ui.RunWindowProcess(rest)
	return true
}

// platformOptions — особенности Mac: обновления из релизов на GitHub пока только для Windows.
func platformOptions(o *Options) { o.NoUpdates = true }
