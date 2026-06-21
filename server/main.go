package main

import (
	"log"
	"net/http"
)

func main() {
	// 创建 Raft 存储层。单节点: 数据目录 raft-data, 通信地址 127.0.0.1:7000
	store, err := NewStore("raft-data", "127.0.0.1:7000")
	if err != nil {
		log.Fatalf("启动 Raft 失败: %v", err)
	}

	// HTTP 路由, 注入 store
	handler := newAPI(store)

	// 启动 HTTP 服务
	log.Println("控制面监听 :8080")
	log.Fatal(http.ListenAndServe(":8080", handler))
}