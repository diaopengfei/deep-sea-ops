// agentclient 包单元测试: 验证 v0.6.7 中间件扫描的进程匹配 / 去重 / 端口提取逻辑。
// 不依赖运行环境, 全部基于构造的 RunningProcess 列表测试。
package agentclient

import (
	"testing"
)

func TestScanMiddlewares_RecognizeAllTypes(t *testing.T) {
	cases := []struct {
		name    string
		proc    RunningProcess
		wantType ProjectType
		wantPort int
	}{
		{
			name: "redis 默认端口",
			proc: RunningProcess{PID: 1, Name: "redis-server", CmdLine: "/usr/bin/redis-server /etc/redis/redis.conf"},
			wantType: MiddlewareRedis,
			wantPort: 6379, // 未在 cmdline 显式指定, 用默认
		},
		{
			name: "redis 显式端口 --port",
			proc: RunningProcess{PID: 2, Name: "redis-server", CmdLine: "/usr/bin/redis-server --port 6380"},
			wantType: MiddlewareRedis,
			wantPort: 6380,
		},
		{
			name: "postgresql 进程",
			proc: RunningProcess{PID: 3, Name: "postgres", CmdLine: "postgres: writer process /var/lib/postgresql/data"},
			wantType: MiddlewarePostgreSQL,
			wantPort: 5432,
		},
		{
			name: "mysql 进程",
			proc: RunningProcess{PID: 4, Name: "mysqld", CmdLine: "/usr/sbin/mysqld --port=3307"},
			wantType: MiddlewareMySQL,
			wantPort: 3307,
		},
		{
			name: "kafka 进程(主类名匹配)",
			proc: RunningProcess{PID: 5, Name: "java", CmdLine: "java -Xmx4g kafka.Kafka /opt/kafka/config/server.properties"},
			wantType: MiddlewareKafka,
			wantPort: 9092,
		},
		{
			name: "zookeeper 进程(主类名匹配)",
			proc: RunningProcess{PID: 6, Name: "java", CmdLine: "java -cp zookeeper.jar org.apache.zookeeper.server.quorum.QuorumPeerMain /opt/zk/conf/zoo.cfg"},
			wantType: MiddlewareZookeeper,
			wantPort: 2181,
		},
		{
			name: "elasticsearch 进程",
			proc: RunningProcess{PID: 7, Name: "java", CmdLine: "java -Xms4g org.elasticsearch.bootstrap.Elasticsearch -d"},
			wantType: MiddlewareElasticsearch,
			wantPort: 9200,
		},
		{
			name: "clickhouse 进程",
			proc: RunningProcess{PID: 8, Name: "clickhouse-server", CmdLine: "/usr/bin/clickhouse-server --config-file=/etc/clickhouse-server/config.xml"},
			wantType: MiddlewareClickHouse,
			wantPort: 8123,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out := ScanMiddlewares([]RunningProcess{c.proc})
			if len(out) != 1 {
				t.Fatalf("期望识别 1 个中间件, 实际 %d", len(out))
			}
			if out[0].Type != c.wantType {
				t.Errorf("Type = %q, want %q", out[0].Type, c.wantType)
			}
			if !out[0].Running {
				t.Errorf("Running 应为 true")
			}
			if out[0].PID != c.proc.PID {
				t.Errorf("PID = %d, want %d", out[0].PID, c.proc.PID)
			}
			// 端口写在 Name 里: 类型-端口
			wantName := string(c.wantType) + "-" + itoa(c.wantPort)
			if out[0].Name != wantName {
				t.Errorf("Name = %q, want %q", out[0].Name, wantName)
			}
		})
	}
}

func TestScanMiddlewares_DedupSameBinary(t *testing.T) {
	// 同一 redis 二进制启动多个进程(主进程 + 子进程), 应只保留一条
	procs := []RunningProcess{
		{PID: 100, Name: "redis-server", CmdLine: "/usr/bin/redis-server /etc/redis/redis.conf"},
		{PID: 101, Name: "redis-server", CmdLine: "/usr/bin/redis-server /etc/redis/redis.conf"}, // 同二进制同配置
	}
	out := ScanMiddlewares(procs)
	if len(out) != 1 {
		t.Errorf("同二进制多进程应去重为 1, 实际 %d", len(out))
	}
}

func TestScanMiddlewares_DifferentBinariesBothKept(t *testing.T) {
	// 不同二进制路径(如 redis5 和 redis7 各装一份)应都展示
	procs := []RunningProcess{
		{PID: 200, Name: "redis-server", CmdLine: "/opt/redis5/bin/redis-server /etc/redis5.conf"},
		{PID: 201, Name: "redis-server", CmdLine: "/opt/redis7/bin/redis-server /etc/redis7.conf"},
	}
	out := ScanMiddlewares(procs)
	if len(out) != 2 {
		t.Errorf("不同二进制应保留 2 条, 实际 %d", len(out))
	}
}

func TestScanMiddlewares_NonMiddlewareProcessIgnored(t *testing.T) {
	procs := []RunningProcess{
		{PID: 300, Name: "java", CmdLine: "java -jar /opt/myapp/app.jar"},     // Java 应用, 非中间件
		{PID: 301, Name: "nginx", CmdLine: "nginx: master process /usr/sbin/nginx"}, // nginx 暂未纳入中间件识别
		{PID: 302, Name: "python3", CmdLine: "python3 /opt/script.py"},
	}
	out := ScanMiddlewares(procs)
	if len(out) != 0 {
		t.Errorf("非中间件进程应被忽略, 实际识别到 %d 条: %+v", len(out), out)
	}
}

func TestIsMiddlewareType(t *testing.T) {
	if !IsMiddlewareType(MiddlewareRedis) {
		t.Error("redis 应为中间件类型")
	}
	if IsMiddlewareType(ProjectJavaSpring) {
		t.Error("java-spring 不是中间件类型")
	}
	if IsMiddlewareType(ProjectPython) {
		t.Error("python 不是中间件类型")
	}
}

func TestExtractPortFromCmdline(t *testing.T) {
	cases := []struct {
		cmdline     string
		defaultPort int
		want        int
	}{
		{"redis-server --port 6380", 6379, 6380},
		{"redis-server --port=6381", 6379, 6381},
		{"mysqld -P 3307", 3306, 3307},
		{"kafka.Kafka /opt/kafka/config/server.properties", 9092, 9092}, // 未指定, 用默认
		{"java -Dserver.port=9092 -jar app.jar", 8080, 8080},           // -Dserver.port 不识别(非标准 flag)
		{"", 6379, 6379},
	}
	for _, c := range cases {
		got := extractPortFromCmdline(c.cmdline, c.defaultPort)
		if got != c.want {
			t.Errorf("extractPortFromCmdline(%q, %d) = %d, want %d", c.cmdline, c.defaultPort, got, c.want)
		}
	}
}

func TestExtractBinaryPath(t *testing.T) {
	cases := []struct {
		cmdline string
		want    string
	}{
		{"/usr/bin/redis-server /etc/redis/redis.conf", "/usr/bin/redis-server"},
		{"/usr/bin/redis-server", "/usr/bin/redis-server"}, // 无空格
		{`"C:\Program Files\Redis\redis-server.exe" --service`, `C:\Program Files\Redis\redis-server.exe`}, // Windows 引号
		{"", ""},
	}
	for _, c := range cases {
		got := extractBinaryPath(c.cmdline)
		if got != c.want {
			t.Errorf("extractBinaryPath(%q) = %q, want %q", c.cmdline, got, c.want)
		}
	}
}

// itoa 简单整数转字符串(避免引入 strconv 仅用于测试断言消息)
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
