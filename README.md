# deepsea-ops

鍒嗗竷寮忔湇鍔″櫒杩愮淮骞冲彴 鈥斺€?鐢ㄤ竴濂楀伐鍏风鐞?20+ 鍙版湇鍔″櫒涓婄殑 Java 寰湇鍔°€丣ava/Python 绋嬪簭銆丷edis銆丳ostgreSQL銆並afka銆丒lasticsearch銆丆lickHouse銆丯acos銆?
> 鐘舵€? 鏃╂湡寮€鍙戜腑(v0.1)銆傞鏈熻仛鐒?Java 寰湇鍔＄殑閰嶇疆绠＄悊涓庢墿瀹硅縼绉汇€?
## 涓轰粈涔堥€犲畠

绠＄悊 20+ 鍙版湇鍔″櫒涓婄殑澶氱被涓棿浠跺拰鏈嶅姟, 浼犵粺鏂瑰紡闈?SSH + 鑴氭湰 + Excel, 閰嶇疆婕傜Щ銆佹墿瀹圭箒鐞愩€佽縼绉婚闄╅珮銆俤eepsea-ops 鐢ㄤ竴濂楀垎甯冨紡鎺у埗闈㈢粺涓€绠＄悊: 閰嶇疆涓€澶勭淮鎶ゃ€佹瘮瀵逛竴鐩簡鐒躲€佹墿瀹硅縼绉讳竴閿紪鎺? 涓斾换鎰忚妭鐐瑰彲璁块棶銆佹晠闅滆嚜鍔ㄥ垏鎹€?
## 鐗规€?
- **鍒嗗竷寮忔帶鍒堕潰**: 3 鑺傜偣 Raft 寮轰竴鑷撮泦缇? 瀹瑰繊 1 鑺傜偣鏁呴殰, 绉掔骇 Leader 鍒囨崲
- **Agent 鏋舵瀯**: 姣忓彴琚鏈哄櫒璺戣交閲?Agent, gRPC 闀胯繛鎺? 蹇冭烦 + 鎸囦护涓嬪彂
- **閰嶇疆娌荤悊**: 杩炴帴 Nacos / 鏈湴閰嶇疆 / jar 鍐呴厤缃? 涓夋柟姣斿, 鍩哄噯鐗堟湰璧?Raft
- **鎵╁杩佺Щ**: Leader 缂栨帓, 涓嬪彂閮ㄧ讲鎸囦护鍒扮洰鏍?Agent, 鐘舵€佸疄鏃跺洖浼?- **鍙鍖?*: 鏈嶅姟鍣?鏈嶅姟鎷撴墤鍥?AntV G6), 璧勬簮鐩戞帶(ECharts), 閰嶇疆 diff(Monaco)
- **鍗曚簩杩涘埗閮ㄧ讲**: Go 浜ゅ弶缂栬瘧, Agent 鎺ㄩ€佸嵆璺? 鎺у埗闈㈣嚜甯﹀墠绔?embed)

## 鏋舵瀯

```
                鈹屸攢鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹?   娴忚鍣?鈹€鈹€鈹€鈹€鈹€鈻垛攤  鎺у埗闈?(3 鑺傜偣 Raft)          鈹?                鈹? HTTP:8080  gRPC:9090  Raft:7000 鈹?                鈹斺攢鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹攢鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹?                  gRPC 闀胯繛 鈹?蹇冭烦 + 鎸囦护
            鈹屸攢鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹尖攢鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹?            鈻?             鈻?             鈻?       Agent@宸ヤ綔鏈?   Agent@宸ヤ綔鏈?  ... Agent@宸ヤ綔鏈篘
```

鎺у埗闈㈢敤 Raft 淇濊瘉寮轰竴鑷?閰嶇疆銆侀儴缃茶鍒?, Agent 鍦ㄧ嚎鐘舵€佽蛋鍐呭瓨(楂橀鐬椂鏁版嵁涓嶄粯涓€鑷存€ф垚鏈?銆傝瑙佹灦鏋勮璁℃枃妗ｃ€?
## 鎶€鏈爤

| 灞?| 鎶€鏈?|
|----|------|
| 鍚庣 | Go 1.22+, hashicorp/raft, bbolt, gRPC |
| 鍓嶇 | Vue 3, TypeScript, Vite, Element Plus, ECharts, AntV G6 |
| 閰嶇疆缂栬緫 | Monaco Editor |

## 蹇€熷紑濮?
### 鐜瑕佹眰

- Go 1.22+
- Node.js 18+
- Git

### 鏋勫缓

```bash
# 鍚庣(鎺у埗闈?+ Agent)
cd server
go build -o ../dist/deepsea-server ./cmd/server
go build -o ../dist/deepsea-agent ./cmd/agent

# 鍓嶇
cd ../web
npm install
npm run build      # 浜х墿 web/dist/
```

鎴栫敤鏍圭洰褰?Makefile:

```bash
make build         # 鏋勫缓鍏ㄩ儴鍒?dist/
```

### 鏈湴杩愯(寮€鍙戞ā寮?

```bash
# 缁堢 1: 鎺у埗闈?cd server
go run ./cmd/server

# 缁堢 2: Agent
cd server
go run ./cmd/agent -id agent-1 -server 127.0.0.1:9090

# 缁堢 3: 鍓嶇
cd web
npm run dev        # http://localhost:5173
```

鎵撳紑 `http://localhost:5173`, 宸︿晶"鏈嶅姟鍣ㄧ鐞?鏂板鏈嶅姟鍣? "Agent 鑺傜偣"鏌ョ湅鍦ㄧ嚎 Agent銆?
## 椤圭洰缁撴瀯

```
deep-sea-ops/
鈹溾攢鈹€ server/                  Go 鍚庣
鈹?  鈹溾攢鈹€ cmd/
鈹?  鈹?  鈹溾攢鈹€ server/          鎺у埗闈㈠叆鍙?鈹?  鈹?  鈹斺攢鈹€ agent/           Agent 鍏ュ彛
鈹?  鈹溾攢鈹€ internal/            绉佹湁鍖?澶栭儴涓嶅彲 import)
鈹?  鈹?  鈹溾攢鈹€ model/           棰嗗煙妯″瀷
鈹?  鈹?  鈹溾攢鈹€ store/           Raft 瀛樺偍灞?FSM/Store)
鈹?  鈹?  鈹溾攢鈹€ api/             HTTP 璺敱
鈹?  鈹?  鈹溾攢鈹€ grpcserver/      Agent 杩炴帴绠＄悊
鈹?  鈹?  鈹溾攢鈹€ agentclient/     Agent 绔繛鎺ラ€昏緫
鈹?  鈹?  鈹斺攢鈹€ proto/agent/     protoc 鐢熸垚浠ｇ爜
鈹?  鈹斺攢鈹€ proto/agent.proto    gRPC 濂戠害
鈹溾攢鈹€ web/                     Vue 鍓嶇
鈹?  鈹斺攢鈹€ src/{api,views,styles}/
鈹溾攢鈹€ Makefile                 鏋勫缓鑴氭湰
鈹斺攢鈹€ dist/                    鏋勫缓浜х墿(gitignore)
```

## 閮ㄧ讲

鐢熶骇閮ㄧ讲鍒?Linux 闆嗙兢瑙侀儴缃叉寚鍗楁枃妗? 浜ゅ弶缂栬瘧銆乻ystemd銆乶ginx銆丄gent 鎵归噺閮ㄧ讲銆佹粴鍔ㄥ崌绾с€?
## 璺嚎鍥?
- **v0.1** 鍗曡妭鐐规帶鍒堕潰 + Agent 楠ㄦ灦 鉁?(M1-M3 瀹屾垚, M4 杩涜涓?
  - Raft 鍗曡妭鐐瑰瓨鍌ㄣ€乥bolt 鎸佷箙鍖栥€乬RPC 鍙屽悜娴併€丄gent 蹇冭烦
- **v0.2** 3 鑺傜偣瀹归敊闆嗙兢
- **v0.3** Java 杩愮淮 MVP(閰嶇疆姣斿銆佹墿瀹硅縼绉汇€佹嫇鎵戝彲瑙嗗寲)
- **v0.4** 鍒嗗竷寮忛儴缃茶兘鍔?bootstrap 鑷姩鍖栥€佸叆鍙ｄ唬鐞嗐€佸畨鍏ㄥ姞鍥?

## 寮€鍙?
```bash
git clone <repo>
cd deepsea-ops
make dev         # 鍚姩鍚庣 + 鍓嶇寮€鍙戞湇鍔?```

浠ｇ爜瑙勮寖: Go 鐢?`gofmt`/`go vet`; 鍓嶇鐢?TypeScript strict銆傛彁浜ゅ墠 `make check`銆?
## 璁稿彲璇?
MIT