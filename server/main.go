package main

import (
	"encoding/json"
	"log"
	"net/http"
)

// Server 表示一台被管理的服务器。
// Go 里结构体字段如果首字母大写，就是"公开的"（可被外部包访问），
// 这一点和 Java 的 public/private 不同，Go 是按首字母大小写决定可见性。
type Server struct {
	ID     string `json:"id"`     // 字符串标签告诉 json 包序列化时用这个 key
	Name   string `json:"name"`
	IP     string `json:"ip"`
	Status string `json:"status"` // online / offline
}

// servers 是我们的"假数据库"。
// 热身阶段先用内存里的硬编码数据，v0.1 会换成 Raft + bbolt。
var servers = []Server{
	{ID: "s1", Name: "app-node-1", IP: "192.168.1.10", Status: "online"},
	{ID: "s2", Name: "app-node-2", IP: "192.168.1.11", Status: "online"},
	{ID: "s3", Name: "db-node-1", IP: "192.168.1.20", Status: "offline"},
}

func main() {
	// 注册一个 HTTP 路由：访问 /api/servers 时，调用 serversHandler 函数
	http.HandleFunc("/api/servers", serversHandler)

	addr := ":8080"
	log.Printf("后端启动，监听 %s", addr)
	// ListenAndServe 是阻塞的，进程会一直跑在这里处理请求
	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatal(err)
	}
}

// serversHandler 处理 /api/servers 请求。
// http.ResponseWriter 用来写响应，*http.Request 是请求对象。
func serversHandler(w http.ResponseWriter, r *http.Request) {
	// 告诉浏览器这是 JSON
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	// 把 servers 切片编码成 JSON 写到响应里
	if err := json.NewEncoder(w).Encode(servers); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
