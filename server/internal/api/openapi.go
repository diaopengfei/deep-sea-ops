package api

import (
	"encoding/json"
	"net/http"
)

// handleOpenAPISpec GET /api/openapi.json — 返回 OpenAPI 3.0 规范文档(白名单, 无需登录)。
// 文档描述所有公开 API 的路径、方法、请求/响应结构, 供 Swagger UI 渲染和外部系统集成参考。
func handleOpenAPISpec(w http.ResponseWriter, r *http.Request) {
	spec := buildOpenAPISpec()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(spec)
}

// handleSwaggerUI GET /api/docs — 返回 Swagger UI HTML 页面(白名单, 无需登录)。
// 使用官方 Swagger UI CDN, 加载 /api/openapi.json 渲染交互式 API 文档。
func handleSwaggerUI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(swaggerUIHTML))
}

const swaggerUIHTML = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
  <meta charset="UTF-8">
  <title>deepsea-ops API 文档</title>
  <link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5.11.0/swagger-ui.css">
  <style>body { margin: 0; }</style>
</head>
<body>
  <div id="swagger-ui"></div>
  <script src="https://unpkg.com/swagger-ui-dist@5.11.0/swagger-ui-bundle.js"></script>
  <script>
    window.onload = function() {
      SwaggerUIBundle({
        url: '/api/openapi.json',
        dom_id: '#swagger-ui',
        deepLinking: true,
        presets: [SwaggerUIBundle.presets.apis],
        layout: 'BaseLayout'
      });
    };
  </script>
</body>
</html>`

// buildOpenAPISpec 构造 OpenAPI 3.0 规范文档。
// 手写 spec 而不引入 swagger 生成库, 避免依赖膨胀; 只描述核心 API 路径。
func buildOpenAPISpec() map[string]interface{} {
	info := map[string]interface{}{
		"title":       "deepsea-ops API",
		"description": "分布式服务器运维平台 API。支持 JWT(前端会话) 和 API Token(系统集成) 两种认证方式。",
		"version":     "1.0.0",
		"contact": map[string]string{
			"name": "deepsea-ops",
			"url":  "https://github.com/diaopengfei/deep-sea-ops",
		},
	}
	servers := []map[string]string{
		{"url": "/api", "description": "当前实例"},
	}
	securitySchemes := map[string]interface{}{
		"jwt": map[string]interface{}{
			"type":        "http",
			"scheme":      "bearer",
			"bearerFormat": "JWT",
			"description": "前端登录后获取的 AccessToken, 30 分钟有效。Authorization: Bearer <jwt>",
		},
		"apiToken": map[string]interface{}{
			"type":        "apiKey",
			"in":          "header",
			"name":        "X-API-Token",
			"description": "长期有效的 API Token, 适合 CI/CD、脚本、外部系统集成。格式: dst_xxx。也可放 Authorization: Bearer dst_xxx",
		},
	}
	components := map[string]interface{}{
		"securitySchemes": securitySchemes,
		"schemas": map[string]interface{}{
			"Error": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"error": map[string]interface{}{"type": "string"},
				},
			},
			"Server": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"id":         map[string]interface{}{"type": "integer", "format": "int64"},
					"name":       map[string]interface{}{"type": "string"},
					"ip":         map[string]interface{}{"type": "string"},
					"port":       map[string]interface{}{"type": "integer"},
					"os":         map[string]interface{}{"type": "string", "enum": []string{"linux", "windows"}},
					"username":   map[string]interface{}{"type": "string"},
					"status":     map[string]interface{}{"type": "string"},
					"createdAt":  map[string]interface{}{"type": "integer", "format": "int64"},
				},
			},
			"AgentInfo": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"id":         map[string]interface{}{"type": "string"},
					"hostname":   map[string]interface{}{"type": "string"},
					"ip":         map[string]interface{}{"type": "string"},
					"lastSeen":   map[string]interface{}{"type": "string", "format": "date-time"},
					"cpuPercent": map[string]interface{}{"type": "number"},
					"memPercent": map[string]interface{}{"type": "number"},
					"version":    map[string]interface{}{"type": "string"},
				},
			},
			"APIToken": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"id":          map[string]interface{}{"type": "string"},
					"name":        map[string]interface{}{"type": "string"},
					"tokenPrefix": map[string]interface{}{"type": "string"},
					"role":        map[string]interface{}{"type": "string", "enum": []string{"admin", "operator", "viewer"}},
					"createdBy":   map[string]interface{}{"type": "string"},
					"createdAt":   map[string]interface{}{"type": "integer", "format": "int64"},
					"lastUsedAt":  map[string]interface{}{"type": "integer", "format": "int64"},
					"expiresAt":   map[string]interface{}{"type": "integer", "format": "int64"},
				},
			},
			"Webhook": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"id":        map[string]interface{}{"type": "string"},
					"name":      map[string]interface{}{"type": "string"},
					"url":       map[string]interface{}{"type": "string"},
					"events":    map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
					"active":    map[string]interface{}{"type": "boolean"},
					"createdBy": map[string]interface{}{"type": "string"},
					"createdAt": map[string]interface{}{"type": "integer", "format": "int64"},
					"hasSecret": map[string]interface{}{"type": "boolean"},
				},
			},
		},
	}
	return map[string]interface{}{
		"openapi":    "3.0.3",
		"info":       info,
		"servers":    servers,
		"components": components,
		"security":   []map[string][]string{{"jwt": {}}, {"apiToken": {}}},
		"tags": []map[string]string{
			{"name": "auth", "description": "认证与会话"},
			{"name": "servers", "description": "服务器管理"},
			{"name": "agents", "description": "Agent 管理"},
			{"name": "projects", "description": "项目记录与配置治理"},
			{"name": "deploy", "description": "部署任务"},
			{"name": "credentials", "description": "SSH 凭据"},
			{"name": "cluster", "description": "集群管理"},
			{"name": "users", "description": "用户管理(admin)"},
			{"name": "tokens", "description": "API Token(admin)"},
			{"name": "webhooks", "description": "Webhook 事件订阅"},
			{"name": "monitor", "description": "监控与告警"},
			{"name": "audit", "description": "操作审计日志"},
		},
		"paths": buildOpenAPIPaths(),
	}
}

// buildOpenAPIPaths 构造 OpenAPI paths 部分。
// 覆盖核心 API, 完整性以代码为准, 文档作为参考。
func buildOpenAPIPaths() map[string]interface{} {
	jsonResp := func(ref string) map[string]interface{} {
		return map[string]interface{}{
			"200": map[string]interface{}{
				"description": "成功",
				"content": map[string]interface{}{
					"application/json": map[string]interface{}{
						"schema": map[string]interface{}{"$ref": ref},
					},
				},
			},
		}
	}
	errResp := map[string]interface{}{
		"401": map[string]interface{}{
			"description": "未授权",
			"content": map[string]interface{}{
				"application/json": map[string]interface{}{
					"schema": map[string]interface{}{"$ref": "#/components/schemas/Error"},
				},
			},
		},
	}

	paths := map[string]interface{}{}

	// /api/login
	paths["/login"] = map[string]interface{}{
		"post": map[string]interface{}{
			"tags":    []string{"auth"},
			"summary": "登录获取 JWT",
			"security": []map[string][]string{},
			"requestBody": map[string]interface{}{
				"required": true,
				"content": map[string]interface{}{
					"application/json": map[string]interface{}{
						"schema": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"username": map[string]interface{}{"type": "string"},
								"password": map[string]interface{}{"type": "string"},
							},
						},
					},
				},
			},
			"responses": jsonResp("#/components/schemas/Server"),
		},
	}

	// /api/servers
	paths["/servers"] = map[string]interface{}{
		"get": map[string]interface{}{
			"tags":     []string{"servers"},
			"summary":  "列出所有服务器",
			"security": []map[string][]string{{"jwt": {}}, {"apiToken": {}}},
			"parameters": []map[string]interface{}{
				{"name": "keyword", "in": "query", "schema": map[string]string{"type": "string"}},
				{"name": "sort", "in": "query", "schema": map[string]string{"type": "string"}},
				{"name": "order", "in": "query", "schema": map[string]interface{}{"type": "string", "enum": []string{"asc", "desc"}}},
			},
			"responses": map[string]interface{}{
				"200": map[string]interface{}{
					"description": "服务器列表",
					"content": map[string]interface{}{
						"application/json": map[string]interface{}{
							"schema": map[string]interface{}{"type": "array", "items": map[string]interface{}{"$ref": "#/components/schemas/Server"}},
						},
					},
				},
			},
		},
		"post": map[string]interface{}{
			"tags":     []string{"servers"},
			"summary":  "新增服务器",
			"security": []map[string][]string{{"jwt": {}}, {"apiToken": {}}},
			"requestBody": map[string]interface{}{
				"required": true,
				"content": map[string]interface{}{
					"application/json": map[string]interface{}{
						"schema": map[string]interface{}{"$ref": "#/components/schemas/Server"},
					},
				},
			},
			"responses": jsonResp("#/components/schemas/Server"),
		},
	}

	// /api/agents
	paths["/agents"] = map[string]interface{}{
		"get": map[string]interface{}{
			"tags":     []string{"agents"},
			"summary":  "列出在线 Agent",
			"security": []map[string][]string{{"jwt": {}}, {"apiToken": {}}},
			"responses": map[string]interface{}{
				"200": map[string]interface{}{
					"description": "Agent 列表",
					"content": map[string]interface{}{
						"application/json": map[string]interface{}{
							"schema": map[string]interface{}{"type": "array", "items": map[string]interface{}{"$ref": "#/components/schemas/AgentInfo"}},
						},
					},
				},
			},
		},
	}

	// /api/projects
	paths["/projects"] = map[string]interface{}{
		"get": map[string]interface{}{
			"tags":     []string{"projects"},
			"summary":  "列出扫描到的项目(含中间件)",
			"security": []map[string][]string{{"jwt": {}}, {"apiToken": {}}},
			"parameters": []map[string]interface{}{
				{"name": "agentId", "in": "query", "schema": map[string]string{"type": "string"}},
			},
			"responses": errResp,
		},
	}

	// /api/deploy-tasks
	paths["/deploy-tasks"] = map[string]interface{}{
		"get": map[string]interface{}{
			"tags":     []string{"deploy"},
			"summary":  "列出部署任务",
			"security": []map[string][]string{{"jwt": {}}, {"apiToken": {}}},
			"responses": errResp,
		},
		"post": map[string]interface{}{
			"tags":     []string{"deploy"},
			"summary":  "创建部署任务(扩容/迁移)",
			"security": []map[string][]string{{"jwt": {}}, {"apiToken": {}}},
			"responses": errResp,
		},
	}

	// /api/users
	paths["/users"] = map[string]interface{}{
		"get": map[string]interface{}{
			"tags":     []string{"users"},
			"summary":  "列出所有用户(admin 专用)",
			"security": []map[string][]string{{"jwt": {}}, {"apiToken": {}}},
			"responses": errResp,
		},
		"post": map[string]interface{}{
			"tags":     []string{"users"},
			"summary":  "创建用户(admin 专用)",
			"security": []map[string][]string{{"jwt": {}}, {"apiToken": {}}},
			"responses": errResp,
		},
	}

	// /api/tokens
	paths["/tokens"] = map[string]interface{}{
		"get": map[string]interface{}{
			"tags":     []string{"tokens"},
			"summary":  "列出所有 API Token(admin 专用)",
			"security": []map[string][]string{{"jwt": {}}, {"apiToken": {}}},
			"responses": map[string]interface{}{
				"200": map[string]interface{}{
					"description": "Token 列表(不含明文)",
					"content": map[string]interface{}{
						"application/json": map[string]interface{}{
							"schema": map[string]interface{}{"type": "array", "items": map[string]interface{}{"$ref": "#/components/schemas/APIToken"}},
						},
					},
				},
			},
		},
		"post": map[string]interface{}{
			"tags":     []string{"tokens"},
			"summary":  "创建 API Token(明文只返回一次)",
			"security": []map[string][]string{{"jwt": {}}, {"apiToken": {}}},
			"requestBody": map[string]interface{}{
				"required": true,
				"content": map[string]interface{}{
					"application/json": map[string]interface{}{
						"schema": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"name":      map[string]interface{}{"type": "string"},
								"role":      map[string]interface{}{"type": "string", "enum": []string{"admin", "operator", "viewer"}},
								"expiresAt": map[string]interface{}{"type": "integer", "format": "int64", "description": "0 表示永不过期"},
							},
						},
					},
				},
			},
			"responses": map[string]interface{}{
				"201": map[string]interface{}{
					"description": "创建成功, 返回明文 token(只此一次)",
					"content": map[string]interface{}{
						"application/json": map[string]interface{}{
							"schema": map[string]interface{}{
								"type": "object",
								"properties": map[string]interface{}{
									"token": map[string]interface{}{"type": "string", "description": "明文 token, 格式 dst_xxx, 只返回一次"},
									"info":  map[string]interface{}{"$ref": "#/components/schemas/APIToken"},
								},
							},
						},
					},
				},
			},
		},
	}
	paths["/tokens/{id}"] = map[string]interface{}{
		"delete": map[string]interface{}{
			"tags":     []string{"tokens"},
			"summary":  "删除 API Token(admin 专用)",
			"security": []map[string][]string{{"jwt": {}}, {"apiToken": {}}},
			"parameters": []map[string]interface{}{
				{"name": "id", "in": "path", "required": true, "schema": map[string]string{"type": "string"}},
			},
			"responses": errResp,
		},
	}

	// /api/webhooks
	paths["/webhooks"] = map[string]interface{}{
		"get": map[string]interface{}{
			"tags":     []string{"webhooks"},
			"summary":  "列出所有 Webhook",
			"security": []map[string][]string{{"jwt": {}}, {"apiToken": {}}},
			"responses": map[string]interface{}{
				"200": map[string]interface{}{
					"description": "Webhook 列表",
					"content": map[string]interface{}{
						"application/json": map[string]interface{}{
							"schema": map[string]interface{}{"type": "array", "items": map[string]interface{}{"$ref": "#/components/schemas/Webhook"}},
						},
					},
				},
			},
		},
		"post": map[string]interface{}{
			"tags":     []string{"webhooks"},
			"summary":  "新增 Webhook",
			"security": []map[string][]string{{"jwt": {}}, {"apiToken": {}}},
			"requestBody": map[string]interface{}{
				"required": true,
				"content": map[string]interface{}{
					"application/json": map[string]interface{}{
						"schema": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"name":   map[string]interface{}{"type": "string"},
								"url":    map[string]interface{}{"type": "string"},
								"events": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "空数组表示订阅全部事件"},
								"secret": map[string]interface{}{"type": "string", "description": "HMAC 签名密钥, 接收方用同 secret 验签"},
								"active": map[string]interface{}{"type": "boolean"},
							},
						},
					},
				},
			},
			"responses": jsonResp("#/components/schemas/Webhook"),
		},
	}
	paths["/webhooks/{id}"] = map[string]interface{}{
		"put": map[string]interface{}{
			"tags":     []string{"webhooks"},
			"summary":  "更新 Webhook",
			"security": []map[string][]string{{"jwt": {}}, {"apiToken": {}}},
			"parameters": []map[string]interface{}{
				{"name": "id", "in": "path", "required": true, "schema": map[string]string{"type": "string"}},
			},
			"responses": errResp,
		},
		"delete": map[string]interface{}{
			"tags":     []string{"webhooks"},
			"summary":  "删除 Webhook",
			"security": []map[string][]string{{"jwt": {}}, {"apiToken": {}}},
			"parameters": []map[string]interface{}{
				{"name": "id", "in": "path", "required": true, "schema": map[string]string{"type": "string"}},
			},
			"responses": errResp,
		},
	}
	paths["/webhooks/{id}/test"] = map[string]interface{}{
		"post": map[string]interface{}{
			"tags":     []string{"webhooks"},
			"summary":  "测试 Webhook 推送",
			"security": []map[string][]string{{"jwt": {}}, {"apiToken": {}}},
			"parameters": []map[string]interface{}{
				{"name": "id", "in": "path", "required": true, "schema": map[string]string{"type": "string"}},
			},
			"responses": errResp,
		},
	}

	// /api/alerts
	paths["/alerts"] = map[string]interface{}{
		"get": map[string]interface{}{
			"tags":     []string{"monitor"},
			"summary":  "列出当前 firing 告警",
			"security": []map[string][]string{{"jwt": {}}, {"apiToken": {}}},
			"responses": errResp,
		},
	}

	// /api/audit-logs
	paths["/audit-logs"] = map[string]interface{}{
		"get": map[string]interface{}{
			"tags":     []string{"audit"},
			"summary":  "查询操作审计日志",
			"security": []map[string][]string{{"jwt": {}}, {"apiToken": {}}},
			"responses": errResp,
		},
	}

	// /api/cluster/info
	paths["/cluster/info"] = map[string]interface{}{
		"get": map[string]interface{}{
			"tags":     []string{"cluster"},
			"summary":  "集群状态信息",
			"security": []map[string][]string{{"jwt": {}}, {"apiToken": {}}},
			"responses": errResp,
		},
	}

	return paths
}
