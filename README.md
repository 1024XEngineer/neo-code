# NeoCode

> 鍩轰簬 Go + Bubble Tea 鐨勬湰鍦?Coding Agent

## NeoCode 鏄粈涔?
NeoCode 鏄竴涓湪缁堢涓繍琛岀殑 AI 缂栫爜鍔╂墜锛岄噰鐢?ReAct锛圧eason-Act-Observe锛夊惊鐜ā寮忥紝鍥寸粫浠ヤ笅涓婚摼璺伐浣滐細

`鐢ㄦ埛杈撳叆 -> Agent 鎺ㄧ悊 -> 璋冪敤宸ュ叿 -> 鑾峰彇缁撴灉 -> 缁х画鎺ㄧ悊 -> UI 灞曠ず`

瀹冮€傚悎甯屾湜鍦ㄦ湰鍦板伐浣滄祦涓畬鎴愪唬鐮佺悊瑙ｃ€佷慨鏀广€佽皟璇曚笌鑷姩鍖栨搷浣滅殑寮€鍙戣€呫€?
## 鏈変粈涔堣兘鍔?
- 缁堢鍘熺敓 TUI 浜や簰浣撻獙锛圔ubble Tea锛?- Agent 鍙皟鐢ㄥ唴缃伐鍏峰畬鎴愭枃浠朵笌鍛戒护鐩稿叧浠诲姟
- 鏀寔 Provider/Model 鍒囨崲锛堝唴寤?`openai`銆乣gemini`銆乣openll`銆乣qiniu`锛?- 鏀寔涓婁笅鏂囧帇缂╋紙`/compact`锛夛紝甯姪闀夸細璇濅繚鎸佸彲鐢?- 鏀寔宸ヤ綔鍖洪殧绂伙紙`--workdir`銆乣/cwd`锛?- 浼氳瘽鎸佷箙鍖栦笌鎭㈠锛岄檷浣庨噸澶嶆矡閫氭垚鏈?- 鏀寔鎸佷箙璁板繂鏌ョ湅銆佹樉寮忓啓鍏ヤ笌鍚庡彴鑷姩鎻愬彇锛屼繚鐣欒法浼氳瘽鍋忓ソ涓庨」鐩簨瀹?
## 鎬庝箞鐢紙蹇€熷紑濮嬶級

### 1) 鐜瑕佹眰

- Go `1.25+`
- 鍙敤鐨?API Key锛堝 OpenAI銆丟emini銆丱penLL銆丵iniu锛?
### 2) 涓€閿畨瑁?
macOS / Linux锛?
```bash
curl -fsSL https://raw.githubusercontent.com/1024XEngineer/neo-code/main/scripts/install.sh | bash
```

Windows PowerShell锛?
```powershell
irm https://raw.githubusercontent.com/1024XEngineer/neo-code/main/scripts/install.ps1 | iex
```

### 3) 浠庢簮鐮佽繍琛?
```bash
git clone https://github.com/1024XEngineer/neo-code.git
cd neo-code
go run ./cmd/neocode
```

Gateway 瀛愬懡浠わ紙Step 1 楠ㄦ灦锛夛細

```bash
go run ./cmd/neocode gateway
```

鎸囧畾缃戠粶璁块棶闈㈢洃鍚湴鍧€锛堥粯璁?`127.0.0.1:8080`锛屼粎鍏佽 Loopback锛夛細

```bash
go run ./cmd/neocode gateway --http-listen 127.0.0.1:8080
```

缃戠粶璁块棶闈㈤鏋剁鐐癸紙EPIC-GW-04锛夛細

- `POST /rpc`锛氬崟娆?JSON-RPC 璇锋眰鍏ュ彛
- `GET /ws`锛歐ebSocket 娴佸紡鍏ュ彛锛堝惈蹇冭烦锛?- `GET /sse`锛歋SE 娴佸紡鍏ュ彛锛圡VP 榛樿瑙﹀彂 `gateway.ping`锛屽惈蹇冭烦锛?
瀹夊叏闄愬埗锛氫负闃叉璺ㄧ珯鏀诲嚮锛岀綉鍏崇綉缁滈潰榛樿寮€鍚弗鏍肩殑 Origin 鏍￠獙銆傚綋鍓嶄粎鍏佽
`http://localhost`銆乣http://127.0.0.1`銆乣http://[::1]` 浠ュ強 `app://` 鍓嶇紑鏉ユ簮杩炲叆锛?闈炲厑璁告潵婧愮殑璺ㄥ煙璋冪敤浼氳鎷︽埅骞惰繑鍥?`403`銆?娉細涓婅堪鐧藉悕鍗曟満鍒朵粎閽堝鎼哄甫 `Origin` 澶寸殑娴忚鍣ㄨ法绔欒姹傜敓鏁堛€傝嫢璇锋眰涓嶆惡甯?`Origin` 澶?锛堜緥濡?`cURL`銆丳ostman 鎴栨湰鍦板悗绔剼鏈洿杩烇級锛岀綉鍏抽粯璁ゆ斁琛屻€?
URL Scheme 娲惧彂楠ㄦ灦鍛戒护锛圗PIC-GW-02A锛夛細

```bash
go run ./cmd/neocode url-dispatch --url "neocode://review?path=README.md"
```

> `url-dispatch` 浼氬皢 `neocode://` URL 杞彂鍒版湰鍦?Gateway锛屽苟杈撳嚭缁撴瀯鍖栧搷搴斻€?>
> 娉ㄦ剰锛氬綋鍓?MVP 鐗堟湰浠呮敮鎸?`review` 鍔ㄤ綔锛屼笖蹇呴』鎼哄甫 `path` 鍙傛暟锛堝 `neocode://review?path=README.md`锛夛紱鍏朵綑鍔ㄤ綔浼氬湪缃戝叧渚ц鎷︽埅鎷掔粷銆?
璁剧疆 API Key 绀轰緥锛堟寜浣犱娇鐢ㄧ殑 provider 閫夋嫨锛夛細

```bash
export OPENAI_API_KEY="your_key_here"
export GEMINI_API_KEY="your_key_here"
export AI_API_KEY="your_key_here"
export QINIU_API_KEY="your_key_here"
```

Windows PowerShell锛?
```powershell
$env:OPENAI_API_KEY = "your_key_here"
$env:GEMINI_API_KEY = "your_key_here"
$env:AI_API_KEY = "your_key_here"
$env:QINIU_API_KEY = "your_key_here"
```

鎸夊伐浣滃尯鍚姩锛堜粎褰撳墠杩涚▼鐢熸晥锛夛細

```bash
go run ./cmd/neocode --workdir /path/to/workspace
```

### 4) 棣栨浣跨敤涓庡父鐢ㄥ懡浠?
- `/help`锛氭煡鐪嬪懡浠ゅ府鍔?- `/provider`锛氭墦寮€ provider 閫夋嫨鍣?- `/model`锛氭墦寮€ model 閫夋嫨鍣?- `/compact`锛氬帇缂╁綋鍓嶄細璇濅笂涓嬫枃
- `/status`锛氭煡鐪嬪綋鍓嶄細璇濅笌杩愯鐘舵€?- `/cwd [path]`锛氭煡鐪嬫垨璁剧疆褰撳墠浼氳瘽宸ヤ綔鍖?- `/memo`锛氭煡鐪嬭蹇嗙储寮?- `/remember <text>`锛氫繚瀛樿蹇?- `/forget <keyword>`锛氭寜鍏抽敭璇嶅垹闄よ蹇?- `& <command>`锛氬湪褰撳墠宸ヤ綔鍖烘墽琛屾湰鍦板懡浠?
绀轰緥杈撳叆锛?
```text
璇峰厛闃呰褰撳墠椤圭洰鐩綍缁撴瀯骞剁粰鍑烘ā鍧楄亴璐ｆ憳瑕?甯垜鍦?internal/runtime 涓嬪畾浣嶄笌 tool result 鍥炵亴鐩稿叧閫昏緫
```

## 閰嶇疆鍏ュ彛

- 涓婚厤缃枃浠讹細`~/.neocode/config.yaml`
- 鑷畾涔?Provider锛歚~/.neocode/providers/<provider-name>/provider.yaml`

閰嶇疆鍘熷垯锛堢敤鎴蜂晶閲嶇偣锛夛細

- API Key 閫氳繃鐜鍙橀噺娉ㄥ叆锛屼笉鍐欏叆 `config.yaml`
- `--workdir` 鍙奖鍝嶅綋鍓嶈繍琛岋紝涓嶄細鍥炲啓鍒伴厤缃枃浠?
璇︾粏閰嶇疆璇峰弬鑰冿細[docs/guides/configuration.md](docs/guides/configuration.md)

## 鏂囨。瀵艰埅

- [閰嶇疆鎸囧崡](docs/guides/configuration.md)
- [鎵╁睍 Provider](docs/guides/adding-providers.md)
- [Runtime/Provider 浜嬩欢娴乚(docs/runtime-provider-event-flow.md)
- [Session 鎸佷箙鍖栬璁(docs/session-persistence-design.md)
- [Context Compact 璇存槑](docs/context-compact.md)
- [Tools 涓?TUI 闆嗘垚](docs/tools-and-tui-integration.md)
- [MCP 閰嶇疆鎸囧崡](docs/guides/mcp-configuration.md)
- [鏇存柊涓庡崌绾(docs/guides/update.md)

## 濡備綍鍙備笌

娆㈣繋閫氳繃 Issue 鍜?PR 鍙備笌鍏卞缓銆?
1. 鍦?[Issues](https://github.com/1024XEngineer/neo-code/issues) 鍏堟矡閫氶棶棰樻垨闇€姹傘€?2. Fork 浠撳簱骞跺垱寤哄姛鑳藉垎鏀€?3. 瀹屾垚寮€鍙戝苟纭繚鏀瑰姩鑱氱劍銆佽竟鐣屾竻鏅般€?4. 鏈湴鑷锛?
   ```bash
   gofmt -w ./cmd ./internal
   go test ./...
   go build ./...
   ```

5. 鎻愪氦 PR 鍒颁富浠撳簱骞惰鏄庡彉鏇寸洰鐨勩€佸奖鍝嶈寖鍥村拰楠岃瘉鏂瑰紡銆?
鎻愪氦鍓嶈纭锛?
- 涓嶆彁浜ゆ槑鏂囧瘑閽ャ€佷釜浜洪厤缃垨浼氳瘽鏁版嵁
- 涓嶆彁浜ゆ棤鍏虫敼鍔ㄤ笌涓存椂鏂囦欢

## 缃戝叧杩愮淮涓庡畨鍏紙GW-06锛?
- 闈欓粯璁よ瘉锛圫ilent Auth锛夛細
  - 鍚姩 `neocode gateway` 鏃朵細鑷姩璇诲彇 `~/.neocode/auth.json`銆?  - 鑻ュ嚟璇佷笉瀛樺湪鎴栨崯鍧忥紝浼氳嚜鍔ㄧ敓鎴愰珮寮哄害 token 骞跺啓鍥炶鏂囦欢銆?  - `url-dispatch` 浼氳嚜鍔ㄨ鍙栧悓涓€ token 骞跺厛鍙戦€?`gateway.authenticate`锛屽啀鍙戦€佷笟鍔¤姹傘€?- 璁よ瘉涓庢巿鏉冮『搴忥細`Auth -> ACL -> Dispatch`銆?  - 鏈璇佽繑鍥?`unauthorized`銆?  - 宸茶璇佷絾涓嶅厑璁哥殑鏂规硶杩斿洖 `access_denied`銆?- 杩愮淮绔偣锛?  - 鍏嶉壌鏉冿細`GET /healthz`銆乣GET /version`
  - 闇€閴存潈锛歚GET /metrics`銆乣GET /metrics.json`锛坄Authorization: Bearer <token>`锛?- 鍏抽敭榛樿娌荤悊鍙傛暟锛堝彲閫氳繃 `config.yaml` 鐨?`gateway.*` 閰嶇疆锛夛細
  - `max_frame_bytes=1MiB`
  - `ipc_max_connections=128`
  - `http_max_request_bytes=1MiB`
  - `http_max_stream_connections=128`
  - `ipc_read/write_sec=30/30`
  - `http_read/write/shutdown_sec=15/15/2`
- 璇︾粏璁捐鏂囨。锛歔`docs/gateway-detailed-design.md`](docs/gateway-detailed-design.md)

### Gateway JSON-RPC 方法清单（当前实现）

- `gateway.authenticate`：连接级鉴权握手
- `gateway.ping`：探活
- `gateway.bindStream`：会话流绑定
- `gateway.run`：发起一次运行（Accepted-ACK，异步执行）
- `gateway.compact`：触发会话压缩
- `gateway.cancel`：按 `run_id` 精确取消目标运行（`run_id` 必填）
- `gateway.listSessions`：查询会话摘要列表
- `gateway.loadSession`：加载单个会话详情
- `gateway.resolvePermission`：提交权限审批结果
- `wake.openUrl`：处理 `neocode://` 唤醒请求
- `gateway.event`：网关推送通知事件（notification）

## License

MIT

