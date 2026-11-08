# WebUI 嵌入目录

本目录供 `webui.go` 的 `//go:embed all:webui_dist` 使用，必须至少存在一个被跟踪文件，
以保证干净 checkout 下 `go build` / `go test` / `go vet` 均可正常嵌入。

真实前端产物由 `web/scripts/embed-dist.mjs` 在构建期写入（含 `index.html` 与 `assets/`），
全部产物均被 `.gitignore` 忽略，因此 `make build` 不会再弄脏工作区，
也不需要 `git checkout --` 还原占位页。

未构建前端时，管理界面会自动显示 `webui_fallback.html` 中的构建提示页。
