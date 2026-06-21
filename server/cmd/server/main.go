package main

import (
	"log"
	"net/http"

	"github.com/deepsea-ops/server/internal/api"
	"github.com/deepsea-ops/server/internal/store"
)

func main() {
	// 创建 Raft 存储层。单节点: 数据目录 raft-data, 通信地址 127.0.0.1:7000
	// 注意 raft-data 是相对工作目录的路径, 生产环境应从配置读取。
	storeInstance, err := store.NewStore("raft-data", "127.0.0.1:7000")
	if err != nil {
		log.Fatalf("启动 Raft 失败: %v", err)
	}

	// HTTP 路由, 注入 store
	handler := api.New(storeInstance)

	log.Println("控制面监听 :8080")
	log.Fatal(http.ListenAndServe(":8080", handler))
}