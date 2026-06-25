// Package webassets 将前端构建产物 (web/dist/) 嵌入控制面二进制,
// 实现"单二进制部署": 控制面只需一个 deepsea-server + 一个 server.yaml。
//
// 构建流程 (见 Makefile): make build / make build-linux 会先执行 npm run build
// 产出 web/dist/, 再把 web/dist/ 拷贝到本包目录下的 dist/, 最后 go build。
// 开发环境下 dist/ 可能不存在, 此时返回空的文件系统, 前端走 vite dev server。
package webassets

import (
	"embed"
	"io/fs"
	"log"
	"os"
)

//go:embed all:dist
var embeddedFS embed.FS

// distFS 缓存子树文件系统, 避免每次请求重新 Sub。
var distFS fs.FS

func init() {
	sub, err := fs.Sub(embeddedFS, "dist")
	if err != nil {
		// 理论上不会发生(dist 目录在构建时由 Makefile 拷贝生成)
		log.Printf("[webassets] 嵌入 dist 子树失败: %v (前端不可用, 请检查构建流程)", err)
		distFS = emptyFS{}
		return
	}
	distFS = sub
}

// FS 返回前端静态文件的根文件系统 (内容为 web/dist/ 下的文件)。
// 开发环境若未嵌入前端, 返回空文件系统, 调用方应回退到 vite dev server。
func FS() fs.FS {
	return distFS
}

// emptyFS 是一个始终返回 not found 的空文件系统, 用于开发环境未构建前端时。
type emptyFS struct{}

func (emptyFS) Open(name string) (fs.File, error) {
	return nil, &os.PathError{Op: "open", Path: name, Err: os.ErrNotExist}
}
