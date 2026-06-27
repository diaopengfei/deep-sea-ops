package agentclient

import "github.com/deepsea-ops/server/internal/version"

// agentVersion 是 Agent 上报给控制面的版本号(v0.6.6)。
// 与控制面共享 internal/version.Version, 保证同源码构建版本一致。
var agentVersion = version.Version
