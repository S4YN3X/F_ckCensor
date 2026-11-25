package main

import "github.com/S4YN3X/F_ckCensor/internal/launch"

// version — переменная, чтобы тестовую сборку можно было пометить другой версией через -ldflags -X
var version = "v2.3.7"

func main() {
	launch.Run(launch.Options{Version: version})
}
