//go:build !windows

package ptyproxy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/creack/pty"
	"golang.org/x/term"

	"neo-code/internal/gateway"
	gatewayclient "neo-code/internal/gateway/client"
	"neo-code/internal/gateway/protocol"
	"neo-code/internal/tools"
)

const (
	diagnoseCallTimeout     = 90 * time.Second
	autoDiagnoseCallTimeout = 60 * time.Second
	diagSocketReadDeadline  = 3 * time.Second
	autoProbeTimeout        = 1500 * time.Millisecond

	proxyInitializedBanner = "[ NeoCode Proxy initialized ]"
	proxyExitedBanner      = "[ NeoCode Proxy exited ]"
	shellSessionPrefix     = "shell"
)

var (
	hostTerminalInput = os.Stdin
	isTerminalFD      = term.IsTerminal
	makeRawTerminal   = term.MakeRaw
	restoreTerminal   = term.Restore
	shellSessionSeq   atomic.Uint64

	proxyOutputLineEndingNormalizer = strings.NewReplacer(
		"\r\n", "\r\n",
		"\r", "\r\n",
		"\n", "\r\n",
	)
)

// RunManualShell 闂傚倸鍊搁崐鎼佸磹閹间礁纾圭€瑰嫭鍣磋ぐ鎺戠倞鐟滃繘寮抽敃鍌涚厱妞ゎ厽鍨垫禍婵嬫煕濞嗗繒绠婚柡宀嬬秮婵偓闁靛繆鏅濋崝鍝ョ磽娴ｆ彃浜炬繝銏ｆ硾椤戝嫮鎹㈤崱娑欑厪闁割偅绻冮崳娲煕閿濆懏璐＄紒杈ㄥ浮楠炲洭顢樿閻や線姊洪崫鍕効缂佺粯绻傞悾鐑藉醇閺囩倣銊╂煏婢诡垰鍊诲Λ顖炴⒒?RunManualShell 闂傚倸鍊搁崐鎼佸磹閹间礁纾归柟闂寸绾惧綊鏌ｉ幋锝呅撻柛銈呭閺屾盯顢曢敐鍡欘槬缂佺偓鍎冲锟犲蓟閿濆顫呴柕蹇婃櫇閸旀悂姊哄Ч鍥р偓妤呭磻閹捐埖宕叉繝闈涱儐椤ュ牊绻涢幋婵嗚埞闁告搩鍓熷娲焻閻愯尪瀚板褍澧界槐鎺楁偐閾忣偄纰嶉梺浼欑秮閺€杈╃紦閻ｅ瞼鐭欓悹鍥﹀嫎閸旀垿寮婚弴鐔虹彾妞ゆ牗纰嶉崳瑙勵殽閻愨晛浜鹃梻鍌氬€峰ù鍥х暦閻㈢绐楅柟閭﹀枛閸ㄦ繄鈧箍鍎遍幉妯衡槈閵忕姷顦ㄥ銈庡幗閸ㄩ潧鈻撻妸锔剧瘈闁汇垽娼ф牎闂佽壈顫夐崕鎶筋敊韫囨挴鏀介悗锝庡亞閸樺憡绻濋姀锝嗙【妞ゆ垵鎳橀幃姗€濡疯閸嬫挸鈻撻崹顔界亾闂佺绻戦敋閾荤偤鏌涘☉娆愬剹闁轰礁鍟撮弻鏇＄疀婵炴儳浜鹃柟棰佺劍琚╂繝鐢靛Х閺佹悂宕戝☉姗嗗殨闁割偅娲橀崑瀣節婵犲倹鍣归柛銊︾箞閺屽秹宕崟顐熷亾婵犳碍鈷旈柛鏇ㄥ灡閻撴洘绻涢幋鐐╂闁割偁鍎辩粻?
func RunManualShell(ctx context.Context, options ManualShellOptions) error {
	normalized, err := NormalizeShellOptions(options)
	if err != nil {
		return err
	}

	shellPath := resolveShellPath(normalized.Shell)
	if shellPath == "" {
		return errors.New("ptyproxy: shell executable is empty")
	}
	shellSessionID := generateShellSessionID(os.Getpid())

	// 闂傚倸鍊搁崐鎼佸磹閹间礁纾归柣鎴ｅГ閸ゅ嫰鏌涢锝嗙５闁逞屽墾缁犳挸鐣锋總绋款潊闁炽儱鍟跨花銉╂⒒娴ｇ儤鍤€妞ゆ洦鍘介幈銊╁箻椤旂厧鐎┑鐘绘涧閻楀啴宕戦幘璇茬濠㈣泛锕ｆ竟鏇㈡⒒娓氣偓閳ь剛鍋涢懟顖涙櫠閹绢喗鐓欑€规洖娲ゆ禒閬嶆煛鐏炶姤顥滄い鎾炽偢瀹曘劑顢涢敐鍛殽闂傚倸鍊峰ù鍥ㄦ叏閵堝鏅俊鐐€х粻鎾寸閸洖绠栧Δ锝呭暙缁狀噣鏌ら幁鎺戝姉闁归攱妞藉濠氬磼濮樺崬顤€缂備礁顑嗛幐鍓у垝婵犳艾绀冩繛鏉戭儐閺傗偓婵＄偑鍊栭悧顓犲緤娴犲宓侀柡宥庡幖绾偓闂佸憡顨堥崑鎰板绩娴犲鐓熸俊顖濇閿涘秵銇勯敐鍡欏弨闁哄矉缍侀獮姗€宕￠悙鎻掝潥缂傚倷鑳剁划顖炴儎椤栫偟宓侀悗锝庡枟閸婄兘鏌涢…鎴濅簻婵炲懏鐟ラ埞鎴︽偐椤旇偐浼囧銈庡亜椤︾敻鍨鹃敃鍌氱倞妞ゆ巻鍋撻柣鎺戠仛閵囧嫰骞掗幋婵囩亾濠电偛鍚嬮崝娆撳蓟閻旂厧浼犻柕澶樺枤閸旀悂鏌ч懡銈呬沪缂佺粯鐩畷鍗炍熺拠鑼暡缂傚倷鑳舵刊顓㈠垂閸洖钃熸繛鎴炃氬Σ鍫熺箾閸℃ê濮夌紒瀣喘濮婃椽骞栭悙鎻掝瀳闂佺锕ょ紞濠冧繆閻㈢绀嬫い鏍ㄦ皑閻嫰姊洪幖鐐插姶闁绘挸鐗嗚灋闁告洦鍨遍埛鎴︽⒑椤愩倕浠滈柤娲诲灡閺呰埖瀵肩€涙鍘告繛杈剧到閹诧繝骞夐崨濠佺箚闁告瑥顦慨宥嗩殽閻愭惌鐒介柍褜鍓熷褔骞楀鍫濈疇婵犻潧顑嗛埛鎴︽煙閼测晛浠滈柛鏂哄亾闂備礁鎲￠崝锔界閸洖鐤柛顐犲灮绾句粙鏌涚仦鎹愬闁逞屽墮閸㈡煡鈥旈崘顔肩闁哄啠鍋撴い鎰矙閺屾洟宕煎┑鍥舵缂備浇顕уΛ婵嬪蓟濞戙垹唯闁靛繆鍓濋悵鏃堟⒑閹肩偛濡界紒璇插暟閹广垹鈽夐姀鐘茶€垮┑鈽嗗灥濞咃絾绂嶉崼鏇熲拺缂佸顑欓崕鎰版煙閻熺増鍠樼€殿噮鍋婇獮鍥敇閻斿嘲濡虫繝鐢靛█濞佳囨偋婵犲洤鐤鹃柤娴嬫櫇绾捐棄霉閿濆洦鍤€闁告柣鍊楅幉鎼佸箥椤旈棿鎴烽梺鍛婂笚鐢帡鍩㈡惔銊ョ鐎规洖娲ㄩ弳顐⑩攽閻愬樊鍤熷┑顔芥尦椤㈡牠骞囬弶鎸庣€悗骞垮劚濡稓绮绘ィ鍐╃厵閻庣數顭堟禒褔鏌熼崘鎻掓殻闁哄苯绉烽¨渚€鏌涢幘鏉戝摵鐎规洘绻傞埢搴ㄥ箻鐎圭姵鎲伴柣搴＄畭閸庨亶鎮у鍐剧€堕柕濞炬櫆閳锋垹绱掔€ｎ厽纭剁紒鐘崇叀閺屻劑寮村Ο铏逛患闂佷紮绲块崗姗€鐛€ｎ喗鏅濋柍褜鍓涚划濠氭嚒閵堝洨锛濇繛杈剧秬椤曟牠鎮炴禒瀣厱婵せ鍋撳ù婊嗘硾椤繐煤椤忓懎浠梻渚囧弿缁犳垵鈻撻懜鐢电瘈婵炲牆鐏濋弸鐔兼煙閸涘﹤鈻曟鐐叉瀹曟﹢顢欓懖鈺婂悈闂備胶绮…鍥极閹间焦鏅繝濠傜墛閻撴稑顭跨捄鐚村姛濠⒀勫灴閺屾盯寮幐搴㈠闯缂備緡鍠栭…宄邦嚕娴犲鏁冮柕鍫濇祫缁辨煡姊虹拠鎻掑毐缂傚秴妫欑粋宥夋倻閻ｅ苯褰嗗銈嗗笒鐎氼參鎮″▎鎰╀簻闁哄倹瀵ч幆鍫ユ煟韫囨梻鍙€闁哄矉绱曟禒锔炬嫚閹绘帩鐎烽梻渚€鈧偛鑻晶鍓х磽瀹ュ懏顥㈢€规洘鍨垮畷銊╁箹椤撶喐娅嗛梻浣瑰缁诲倿藝娴兼潙鐓曢柟鐑橆殕閻撴洟鎮橀悙鎻掆挃闁瑰啿鎳橀弻娑㈠棘鐠恒剱銈夋煙閸欏鍊愮€殿喖鐖煎畷褰掝敊閼恒儺鍟€闂傚倷鑳堕幊鎾剁不瀹ュ鍨傚ù鐘差儑瀹撲線鎮楅敐搴℃灍闁稿﹪鏀辩换娑㈠醇閻斿鍤嬬紓浣风贰閸嬪懏绌辨繝鍥ㄥ€锋い蹇撳閸嬫捇寮介‖顒佺⊕閹峰懘鎳栧┑鍥╂创鐎规洜鍠栭、姗€鎮ゆ担闀愮紦闂傚倷鑳剁划顖炲礉濡ゅ懌鈧倹绂掔€ｎ亞锛涢梺瑙勫礃瀹曢潧鐣垫笟鈧弻娑⑩€﹂幋婵囩亪婵犳鍠栭柊锝咁潖濞差亝鍋￠柡澶嬪浜涙俊鐐€栭崹鐢杆囬鐐村仼闁绘垹鐡旈弫宥嗙節闂堟稓澧㈤柡澶嬫倐濮婃椽宕烽鈩冾€楅梺鎼炲姂娴滃爼鏁愰悙宸悑濠㈣泛顑傞幏娲⒑閼姐倕鏋戞繝銏★耿閸╂盯寮崼鐔哄幍闂佹儳娴氶崑鍛暦瀹€鈧埀顒冾潐濞插繘宕曢棃娑氭殾闁绘垹鐡旈弫鍥煟濡⒈鏆滅紒鍗炲级娣囧﹪鎮欓鍕ㄥ亾閺嶎灛娑欐媴鐟欏嫬寮块梺闈涚墕濞诧綁鎮炴繝鍥ㄧ厽闁靛繈鍨洪弳鈺冪棯閹冩倯闁靛洤瀚板顕€宕堕懜鐢电Х婵犵數鍋涢幊搴ㄦ晝閵夆晛桅闁告洦鍨伴～鍛存煥濞戞ê顏ゆ俊鎻掔墕椤啴濡舵惔鈥崇闂佸憡姊归崹鐢告偩瀹勯偊娼ㄩ柍褜鍓欓悾宄拔熼崗鐓庣／闂侀潧臎閸曨剦鍟岄梻鍌氬€风粈渚€骞栭锔藉剶濠靛倻顭堢粣妤呮煛瀹ュ骸骞戦柍褜鍏涚欢姘嚕娴犲鏁囬柣鎰劋閺嗗懏绻濆閿嬫緲閳ь剚鎸惧▎銏ゆ晸閻樿尙鍔﹀銈嗗笂閼宠埖鏅堕崹顐ｅ弿?
	restoreGuard := installHostTerminalRestoreGuard()
	defer restoreGuard()

	restoreRawTerminal, err := enableHostTerminalRawMode()
	if err != nil {
		return err
	}
	defer func() {
		_ = restoreRawTerminal()
	}()

	command, cleanupRC := buildShellCommand(shellPath, normalized, shellSessionID)
	defer cleanupRC()
	ptyFile, err := pty.Start(command)
	if err != nil {
		return fmt.Errorf("ptyproxy: start pty shell: %w", err)
	}
	defer func() {
		_ = ptyFile.Close()
	}()

	if err := pty.InheritSize(os.Stdin, ptyFile); err != nil && normalized.Stderr != nil {
		writeProxyf(normalized.Stderr, "neocode shell: inherit terminal size failed: %v\n", err)
	}
	stopResizeWatcher := watchPTYWindowResize(normalized.Stderr, ptyFile)
	defer stopResizeWatcher()

	var outputMu sync.Mutex
	synchronizedOutput := &serializedWriter{writer: normalized.Stdout, lock: &outputMu}
	printProxyInitializedBanner(synchronizedOutput)
	printWelcomeBanner(synchronizedOutput)

	// 婵犵數濮烽弫鍛婃叏閻戝鈧倿鎸婃竟鈺嬬秮瀹曘劑寮堕幋鐙呯幢闂備礁鎲℃笟妤呭矗鎼淬劍鍋勯柛鈩兠肩换鍡樸亜閺嶃劍鐨戞慨锝囧仱閺屾稓鈧綆鍋呯亸顓熴亜椤愶絿绠為柟顔瑰墲閹棃鍩ラ崱姗€鐛庣紓?Gateway RPC 闂傚倸鍊搁崐鎼佸磹閹间礁纾归柟闂寸绾惧綊鏌熼梻瀵割槮缁炬儳缍婇弻锝夊閵忊晝鍔哥紒鐐劤椤兘寮婚悢鐓庣鐟滃繒鏁☉銏＄厽闁规崘娉涢埢鍫ユ煛瀹€鈧崰鎰版晬閹邦厽濯村〒姘煎灡琚﹂梻鍌欐祰椤曆勵殽韫囨洖绶ら悹鎭掑妽椤洟鏌熼悜姗嗘畷闁稿鍔欓弻銈嗘叏閹邦兘鍋撳Δ鍛辈妞ゆ劧闄勯埛鎴︽煕濠靛棗顏柣蹇涗憾閺屾盯鎮╁畷鍥р拰闂佺粯渚楅崰妤冪箔閻旂厧鐒垫い鎺戝瀹撲線鏌熼悜姗嗘當缂佺媴绲剧换婵嬫濞戞瑧鍘愰梺闈涳紡閸涱垽绱抽梻浣侯焾閺堫剟鎮疯钘濋柨鏃傚亾閸犳劙鏌℃径瀣靛劌婵☆垪鍋撻柣搴ゎ潐濞测晝鎹㈠┑瀣祦閹兼番鍔嶇€电姴顭跨捄铏圭伇闁?gateway 闂傚倸鍊搁崐宄懊归崶顒夋晪鐟滃秹锝炲┑瀣櫇闁稿矉濡囩粙蹇旂節閵忥絽鐓愰柛鏃€鐗犲畷鎴﹀Χ婢跺鍘搁梺鎼炲劗閺呮稑鐨梻浣虹帛鐢帡鏁冮妷鈺佄﹂柛鏇ㄥ枤閻も偓闂佸湱鍋撻幆灞轿涢敓鐘冲€甸悷娆忓缁€澶愭倶韫囨梻鎳囬柛鈹惧亾濡炪倖宸婚崑鎾剁磼閻樿尙效鐎规洘娲熷畷锟犳倶缂佹ɑ銇濋柡浣稿暣瀹曟帒顫濋幉瀣耿濠电姵顔栭崰妤呭Φ濞戙垹纾婚柟鍓х帛閻撴瑦銇勯弮鈧弸濠氭嚀閸ф鐓欐い鏃傛櫕閹冲洭鏌熼搹顐ょ煀闁崇粯鎹囬獮鎰償椤旂瓔妫勯梻鍌氬€搁崐宄懊归崶顒夋晪鐟滃繘骞戦姀銈呯婵°倐鍋撶紒鎰殜楠炴牕菐椤掆偓婵＄兘鏌嶈閸撱劍淇婇崶顒€绠查柛鏇ㄥ灠鎯熼梺鎸庢磵閸嬫挾鐥紒銏犵仸婵﹥妞介獮鍡氼槾缂佺姵婢橀…鑳檪缂傚秳鐒︽穱濠囨嚋闂堟稓绐為柣搴秵閸撴瑩鐛幇顑芥斀闁绘劘鍩栬ぐ褏绱撳鍕槮妞ゎ厼娲╅ˇ褰掓煕閳规儳浜炬俊鐐€栫敮濠囨嚄閸撲胶涓嶉柣鎰暯閸嬫挸鈻撻崹顔界彯闂佺顑呯€氫即銆佸Ο鑽ら檮缂佸娉曢崐鐐烘⒑閹稿孩顥嗘俊顐㈠閸掑﹥瀵肩€涙ǚ鎷绘繛杈剧悼椤牓骞冮幋婢濈懓顭ㄩ埀顒傚垝濞嗗繒鏆﹂柡鍥ュ灪閻掕偐鈧箍鍎遍幊鎰八囬銏♀拺闂傚牊鍗曢崼銉ョ柧婵犲﹤鐗嗛悡鏇㈡煙鏉堥箖妾柣鎾存礃缁绘盯骞嬮悜鍥у彆閻庤鎮堕崕宕囨閹烘嚦鏃€鎷呯粙鎸庢嚈闂備礁鎼惌澶岀礊娓氣偓楠炲啴濮€閵堝懐楠囬梺鐟扮摠缁诲秴顭囨惔銊︾厽閹兼番鍊ゅ鎰箾閼碱剙鏋庢い顓炴穿椤﹀綊鏌嶉妷顖滅暤濠碘剝鎮傞崺鈩冪瑹閳ь剟宕捄琛℃斀闁绘绮☉褔鏌ｅΔ浣虹煀閸楅亶鏌ｉ幋锝呅撻柍閿嬪笒閵嗘帒顫濋敐鍛婵犵數鍋橀崠鐘诲炊閿濆倸浜鹃柨鏇炲亞閺佸洭鏌ｅΟ鍝勬倎缂佹顦靛铏规兜閸涱喖娑ч柣搴ㄦ涧閻倸顕ｉ锝嗗闁革富鍘鹃鏇㈡⒑缁洖澧查拑閬嶆倶韫囧骸宓嗛柡灞剧〒閳ь剨绲芥晶搴ㄥ箖婵傚憡鐓欓柟缁樺笚閸熺偤鏌熼娑欘棃濠碘剝鎮傞弫鍐焵椤掑嫬鐒垫い鎺戝暙閻撴劙鏌熸笟鍨閾伙綁鏌熺粙鎸庢崳闁靛棙鍔欏?
	gwRPCClient, gwErr := gatewayclient.NewGatewayRPCClient(gatewayclient.GatewayRPCClientOptions{
		ListenAddress:       normalized.GatewayListenAddress,
		TokenFile:           normalized.GatewayTokenFile,
		DisableHeartbeatLog: true,
	})
	gatewayReady := false
	if gwErr != nil {
		writeProxyf(normalized.Stderr, "neocode shell: gateway client init failed: %v\n", gwErr)
	} else {
		authCtx, authCancel := context.WithTimeout(context.Background(), diagnoseCallTimeout)
		if authErr := gwRPCClient.Authenticate(authCtx); authErr != nil {
			writeProxyf(normalized.Stderr, "neocode shell: gateway auth failed: %v\n", authErr)
		} else {
			gatewayReady = true
		}
		authCancel()
	}
	if gwRPCClient != nil {
		defer gwRPCClient.Close()
	}

	if skillErr := EnsureTerminalDiagnosisSkillFile(); skillErr != nil {
		writeProxyf(normalized.Stderr, "neocode shell: prepare terminal diagnosis skill failed: %v\n", skillErr)
	}
	if gatewayReady {
		cleanupZombieIDMSessions(gwRPCClient, normalized.Stderr)
	}

	logBuffer := NewUTF8RingBuffer(DefaultRingBufferCapacity)
	outputSink := io.MultiWriter(synchronizedOutput, logBuffer)
	commandLogBuffer := NewUTF8RingBuffer(DefaultRingBufferCapacity / 2)

	autoState := &autoRuntimeState{}
	autoState.Enabled.Store(true)
	autoState.OSCReady.Store(false)

	printAutoModeBanner(synchronizedOutput, autoState)

	var (
		notificationStopFn          = func() {}
		notificationRelayWG         sync.WaitGroup
		gatewayEventNotifications   <-chan gatewayclient.Notification
		gatewayControlNotifications <-chan gatewayclient.Notification
		publishShellState           = func(bool) {}
	)
	if gatewayReady {
		stateCtx, stateCancel := context.WithTimeout(context.Background(), diagnoseCallTimeout)
		bindErr := bindShellRoleStream(stateCtx, gwRPCClient, shellSessionID, autoState.Enabled.Load())
		stateCancel()
		if bindErr != nil {
			writeProxyf(normalized.Stderr, "neocode shell: bind shell stream failed: %v\n", bindErr)
		} else {
			eventCh := make(chan gatewayclient.Notification, 256)
			controlCh := make(chan gatewayclient.Notification, 64)
			demuxCtx, demuxCancel := context.WithCancel(context.Background())
			notificationStopFn = demuxCancel
			gatewayEventNotifications = eventCh
			gatewayControlNotifications = controlCh
			notificationRelayWG.Add(1)
			go func() {
				defer notificationRelayWG.Done()
				defer close(eventCh)
				defer close(controlCh)
				demuxGatewayNotifications(demuxCtx, gwRPCClient.Notifications(), eventCh, controlCh)
			}()

			publishShellState = func(autoEnabled bool) {
				updateCtx, updateCancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer updateCancel()
				if err := bindShellRoleStream(updateCtx, gwRPCClient, shellSessionID, autoEnabled); err != nil && normalized.Stderr != nil {
					writeProxyf(normalized.Stderr, "neocode shell: update shell state failed: %v\n", err)
				}
			}
		}
	}
	defer func() {
		notificationStopFn()
		notificationRelayWG.Wait()
	}()

	idm := newIDMController(idmControllerOptions{
		PTYWriter:          ptyFile,
		Output:             synchronizedOutput,
		Stderr:             normalized.Stderr,
		RPCClient:          gwRPCClient,
		NotificationStream: gatewayEventNotifications,
		AutoState:          autoState,
		LogBuffer:          logBuffer,
		DefaultCap:         DefaultRingBufferCapacity,
		Workdir:            normalized.Workdir,
		ShellSessionID:     shellSessionID,
	})
	stopSignalForwarder := watchForwardSignals(command.Process, normalized.Stderr, idm.HandleSignal)
	defer stopSignalForwarder()

	diagnoseJobCh := make(chan diagnoseJob, 4)
	controlCtx, cancelControl := context.WithCancel(context.Background())
	var controlWG sync.WaitGroup
	if gatewayControlNotifications != nil {
		controlWG.Add(1)
		go func() {
			defer controlWG.Done()
			consumeGatewayControlNotifications(
				controlCtx,
				gatewayControlNotifications,
				shellSessionID,
				autoState,
				diagnoseJobCh,
				idm,
				synchronizedOutput,
				normalized.Stderr,
				publishShellState,
			)
		}()
	}

	diagCtx, cancelDiag := context.WithCancel(context.Background())
	var diagWG sync.WaitGroup
	diagWG.Add(1)
	autoDiagFatalCh := make(chan error, 1)
	diagCoordinator := newDiagnosisCoordinator()
	recentTriggerStore := &diagnosisTriggerStore{}
	go func() {
		defer diagWG.Done()
		consumeDiagSignals(
			diagCtx,
			gwRPCClient,
			gatewayEventNotifications,
			diagnoseJobCh,
			synchronizedOutput,
			logBuffer,
			normalized,
			shellSessionID,
			recentTriggerStore,
			autoState,
			func(diagnoseErr error) {
				if diagnoseErr == nil {
					return
				}
				select {
				case autoDiagFatalCh <- diagnoseErr:
				default:
				}
			},
			diagCoordinator,
		)
	}()

	inputTracker := &commandTracker{}
	inputCtx, cancelInput := context.WithCancel(context.Background())
	go func() {
		pumpProxyInput(inputCtx, normalized.Stdin, ptyFile, inputTracker, idm)
	}()

	autoTriggerCh := make(chan diagnoseTrigger, 2)
	go func() {
		probeTimer := time.NewTimer(autoProbeTimeout)
		defer probeTimer.Stop()
		<-probeTimer.C
		if !autoState.OSCReady.Load() {
			autoState.Enabled.Store(false)
			publishShellState(false)
			writeProxyf(normalized.Stderr, "neocode shell: OSC133 probe timed out, auto diagnosis downgraded\n")
			writeProxyLine(synchronizedOutput, "[ ! ] Auto diagnosis is downgraded because shell OSC133 is unavailable. Use `neocode diag` or `neocode diag -i` manually.")
		}
	}()

	var streamWG sync.WaitGroup
	streamWG.Add(1)
	go func() {
		defer streamWG.Done()
		streamPTYOutputWithIDM(ptyFile, outputSink, commandLogBuffer, inputTracker, autoTriggerCh, recentTriggerStore, autoState, idm)
	}()

	var triggerWG sync.WaitGroup
	triggerWG.Add(1)
	go func() {
		defer triggerWG.Done()
		for trigger := range autoTriggerCh {
			select {
			case <-diagCtx.Done():
				return
			case diagnoseJobCh <- diagnoseJob{Trigger: trigger, IsAuto: true}:
			}
		}
	}()

	waitDone := make(chan error, 1)
	go func() {
		waitDone <- command.Wait()
	}()

	var waitErr error
	forcedByAutoDiagFailure := false
	select {
	case <-ctx.Done():
		if command.Process != nil {
			_ = command.Process.Kill()
		}
		waitErr = <-waitDone
	case diagnoseErr := <-autoDiagFatalCh:
		forcedByAutoDiagFailure = true
		writeProxyLine(synchronizedOutput, "[ x ] Auto diagnosis failed, NeoCode proxy will exit and return to the native shell.")
		writeProxyf(synchronizedOutput, "[ reason: %s ]\n", strings.TrimSpace(diagnoseErr.Error()))
		if command.Process != nil {
			_ = syscall.Kill(-command.Process.Pid, syscall.SIGTERM)
			time.Sleep(200 * time.Millisecond)
			_ = command.Process.Kill()
		}
		waitErr = <-waitDone
	case waitErr = <-waitDone:
	}

	printProxyExitedBanner(synchronizedOutput)
	_ = ptyFile.Close()
	idm.Exit()

	cancelControl()
	controlWG.Wait()

	cancelInput()

	cancelDiag()
	streamWG.Wait()
	close(autoTriggerCh)
	triggerWG.Wait()
	diagWG.Wait()

	if waitErr != nil {
		if forcedByAutoDiagFailure {
			return nil
		}
		var exitErr *exec.ExitError
		if errors.As(waitErr, &exitErr) {
			if status, ok := exitErr.Sys().(syscall.WaitStatus); ok {
				return fmt.Errorf("ptyproxy: shell exited with code %d", status.ExitStatus())
			}
		}
		return waitErr
	}
	return nil
}

// printProxyInitializedBanner 闂傚倸鍊搁崐鎼佸磹閹间礁纾圭€瑰嫭鍣磋ぐ鎺戠倞鐟滃繘寮抽敃鍌涚厱妞ゎ厽鍨垫禍婵嬫煕濞嗗繒绠婚柡宀嬬秮婵偓闁靛繆鏅濋崝鍝ョ磽娴ｆ彃浜炬繝銏ｆ硾椤戝嫮鎹㈤崱娑欑厪闁割偅绻冮崳娲煕閿濆懏璐＄紒杈ㄥ浮楠炲洭顢樿閻や線姊洪崫鍕効缂佺粯绻傞悾鐑藉醇閺囩倣銊╂煏婢诡垰鍊诲Λ顖炴⒒?printProxyInitializedBanner 闂傚倸鍊搁崐鎼佸磹閹间礁纾归柟闂寸绾惧綊鏌ｉ幋锝呅撻柛銈呭閺屾盯顢曢敐鍡欘槬缂佺偓鍎冲锟犲蓟閿濆顫呴柕蹇婃櫇閸旀悂姊哄Ч鍥р偓妤呭磻閹捐埖宕叉繝闈涱儐椤ュ牊绻涢幋婵嗚埞闁告搩鍓熷娲焻閻愯尪瀚板褍澧界槐鎺楁偐閾忣偄纰嶉梺浼欑秮閺€杈╃紦閻ｅ瞼鐭欓悹鍥﹀嫎閸旀垿寮婚弴鐔虹彾妞ゆ牗纰嶉崳瑙勵殽閻愨晛浜鹃梻鍌氬€峰ù鍥х暦閻㈢绐楅柟閭﹀枛閸ㄦ繄鈧箍鍎遍幉妯衡槈閵忕姷顦ㄥ銈庡幗閸ㄩ潧鈻撻妸锔剧瘈闁汇垽娼ф牎闂佽壈顫夐崕鎶筋敊韫囨挴鏀介悗锝庡亞閸樺憡绻濋姀锝嗙【妞ゆ垵鎳橀幃姗€濡疯閸嬫挸鈻撻崹顔界亾闂佺绻戦敋閾荤偤鏌涘☉娆愬剹闁轰礁鍟撮弻鏇＄疀婵炴儳浜鹃柟棰佺劍琚╂繝鐢靛Х閺佹悂宕戝☉姗嗗殨闁割偅娲橀崑瀣節婵犲倹鍣归柛銊︾箞閺屽秹宕崟顐熷亾婵犳碍鈷旈柛鏇ㄥ灡閻撴洘绻涢幋鐐╂闁割偁鍎辩粻?
func printProxyInitializedBanner(writer io.Writer) {
	if writer == nil {
		return
	}
	writeProxyLine(writer, proxyInitializedBanner)
}

// printWelcomeBanner 闂傚倸鍊搁崐鎼佸磹閹间礁纾圭€瑰嫭鍣磋ぐ鎺戠倞鐟滃繘寮抽敃鍌涚厱妞ゎ厽鍨垫禍婵嬫煕濞嗗繒绠婚柡宀嬬秮婵偓闁靛繆鏅濋崝鍝ョ磽娴ｆ彃浜炬繝銏ｆ硾椤戝嫮鎹㈤崱娑欑厪闁割偅绻冮崳娲煕閿濆懏璐＄紒杈ㄥ浮楠炲洭顢樿閻や線姊洪崫鍕効缂佺粯绻傞悾鐑藉醇閺囩倣銊╂煏婢诡垰鍊诲Λ顖炴⒒?printWelcomeBanner 闂傚倸鍊搁崐鎼佸磹閹间礁纾归柟闂寸绾惧綊鏌ｉ幋锝呅撻柛銈呭閺屾盯顢曢敐鍡欘槬缂佺偓鍎冲锟犲蓟閿濆顫呴柕蹇婃櫇閸旀悂姊哄Ч鍥р偓妤呭磻閹捐埖宕叉繝闈涱儐椤ュ牊绻涢幋婵嗚埞闁告搩鍓熷娲焻閻愯尪瀚板褍澧界槐鎺楁偐閾忣偄纰嶉梺浼欑秮閺€杈╃紦閻ｅ瞼鐭欓悹鍥﹀嫎閸旀垿寮婚弴鐔虹彾妞ゆ牗纰嶉崳瑙勵殽閻愨晛浜鹃梻鍌氬€峰ù鍥х暦閻㈢绐楅柟閭﹀枛閸ㄦ繄鈧箍鍎遍幉妯衡槈閵忕姷顦ㄥ銈庡幗閸ㄩ潧鈻撻妸锔剧瘈闁汇垽娼ф牎闂佽壈顫夐崕鎶筋敊韫囨挴鏀介悗锝庡亞閸樺憡绻濋姀锝嗙【妞ゆ垵鎳橀幃姗€濡疯閸嬫挸鈻撻崹顔界亾闂佺绻戦敋閾荤偤鏌涘☉娆愬剹闁轰礁鍟撮弻鏇＄疀婵炴儳浜鹃柟棰佺劍琚╂繝鐢靛Х閺佹悂宕戝☉姗嗗殨闁割偅娲橀崑瀣節婵犲倹鍣归柛銊︾箞閺屽秹宕崟顐熷亾婵犳碍鈷旈柛鏇ㄥ灡閻撴洘绻涢幋鐐╂闁割偁鍎辩粻?
func printWelcomeBanner(writer io.Writer) {
	if writer == nil {
		return
	}
	lines := []string{
		"[ Welcome to NeoCode interactive shell ]",
		"[ Usage tips: ]",
		"[ - Auto diagnosis: toggle with `neocode diag auto off` / `neocode diag auto on` ]",
		"[ - Manual diagnosis: run `neocode diag` after a command fails ]",
		"[ - Interactive diagnosis: run `neocode diag -i` to enter IDM and use `@ai ...`; type `exit` to leave ]",
		"[ - More commands: run `neocode -h` ]",
		"[ - Exit shell: type `exit` or press `Ctrl+D` to return to NeoCode ]",
	}
	for _, line := range lines {
		writeProxyLine(writer, line)
	}
}

// printAutoModeBanner 闂傚倸鍊搁崐鎼佸磹閹间礁纾圭€瑰嫭鍣磋ぐ鎺戠倞鐟滃繘寮抽敃鍌涚厱妞ゎ厽鍨垫禍婵嬫煕濞嗗繒绠婚柡宀嬬秮婵偓闁靛繆鏅濋崝鍝ョ磽娴ｆ彃浜炬繝銏ｆ硾椤戝嫮鎹㈤崱娑欑厪闁割偅绻冮崳娲煕閿濆懏璐＄紒杈ㄥ浮楠炲洭顢樿閻や線姊洪崫鍕効缂佺粯绻傞悾鐑藉醇閺囩倣銊╂煏婢诡垰鍊诲Λ顖炴⒒?printAutoModeBanner 闂傚倸鍊搁崐鎼佸磹閹间礁纾归柟闂寸绾惧綊鏌ｉ幋锝呅撻柛銈呭閺屾盯顢曢敐鍡欘槬缂佺偓鍎冲锟犲蓟閿濆顫呴柕蹇婃櫇閸旀悂姊哄Ч鍥р偓妤呭磻閹捐埖宕叉繝闈涱儐椤ュ牊绻涢幋婵嗚埞闁告搩鍓熷娲焻閻愯尪瀚板褍澧界槐鎺楁偐閾忣偄纰嶉梺浼欑秮閺€杈╃紦閻ｅ瞼鐭欓悹鍥﹀嫎閸旀垿寮婚弴鐔虹彾妞ゆ牗纰嶉崳瑙勵殽閻愨晛浜鹃梻鍌氬€峰ù鍥х暦閻㈢绐楅柟閭﹀枛閸ㄦ繄鈧箍鍎遍幉妯衡槈閵忕姷顦ㄥ銈庡幗閸ㄩ潧鈻撻妸锔剧瘈闁汇垽娼ф牎闂佽壈顫夐崕鎶筋敊韫囨挴鏀介悗锝庡亞閸樺憡绻濋姀锝嗙【妞ゆ垵鎳橀幃姗€濡疯閸嬫挸鈻撻崹顔界亾闂佺绻戦敋閾荤偤鏌涘☉娆愬剹闁轰礁鍟撮弻鏇＄疀婵炴儳浜鹃柟棰佺劍琚╂繝鐢靛Х閺佹悂宕戝☉姗嗗殨闁割偅娲橀崑瀣節婵犲倹鍣归柛銊︾箞閺屽秹宕崟顐熷亾婵犳碍鈷旈柛鏇ㄥ灡閻撴洘绻涢幋鐐╂闁割偁鍎辩粻?
func printAutoModeBanner(writer io.Writer, autoState *autoRuntimeState) {
	if writer == nil {
		return
	}
	if autoState != nil && autoState.Enabled.Load() {
		writeProxyLine(writer, "[ auto diagnosis enabled ]")
	} else {
		writeProxyLine(writer, "[ i ] Auto diagnosis is disabled. Use `neocode diag` for manual analysis.")
	}
}

// printProxyExitedBanner 闂傚倸鍊搁崐鎼佸磹閹间礁纾圭€瑰嫭鍣磋ぐ鎺戠倞鐟滃繘寮抽敃鍌涚厱妞ゎ厽鍨垫禍婵嬫煕濞嗗繒绠婚柡宀嬬秮婵偓闁靛繆鏅濋崝鍝ョ磽娴ｆ彃浜炬繝銏ｆ硾椤戝嫮鎹㈤崱娑欑厪闁割偅绻冮崳娲煕閿濆懏璐＄紒杈ㄥ浮楠炲洭顢樿閻や線姊洪崫鍕効缂佺粯绻傞悾鐑藉醇閺囩倣銊╂煏婢诡垰鍊诲Λ顖炴⒒?printProxyExitedBanner 闂傚倸鍊搁崐鎼佸磹閹间礁纾归柟闂寸绾惧綊鏌ｉ幋锝呅撻柛銈呭閺屾盯顢曢敐鍡欘槬缂佺偓鍎冲锟犲蓟閿濆顫呴柕蹇婃櫇閸旀悂姊哄Ч鍥р偓妤呭磻閹捐埖宕叉繝闈涱儐椤ュ牊绻涢幋婵嗚埞闁告搩鍓熷娲焻閻愯尪瀚板褍澧界槐鎺楁偐閾忣偄纰嶉梺浼欑秮閺€杈╃紦閻ｅ瞼鐭欓悹鍥﹀嫎閸旀垿寮婚弴鐔虹彾妞ゆ牗纰嶉崳瑙勵殽閻愨晛浜鹃梻鍌氬€峰ù鍥х暦閻㈢绐楅柟閭﹀枛閸ㄦ繄鈧箍鍎遍幉妯衡槈閵忕姷顦ㄥ銈庡幗閸ㄩ潧鈻撻妸锔剧瘈闁汇垽娼ф牎闂佽壈顫夐崕鎶筋敊韫囨挴鏀介悗锝庡亞閸樺憡绻濋姀锝嗙【妞ゆ垵鎳橀幃姗€濡疯閸嬫挸鈻撻崹顔界亾闂佺绻戦敋閾荤偤鏌涘☉娆愬剹闁轰礁鍟撮弻鏇＄疀婵炴儳浜鹃柟棰佺劍琚╂繝鐢靛Х閺佹悂宕戝☉姗嗗殨闁割偅娲橀崑瀣節婵犲倹鍣归柛銊︾箞閺屽秹宕崟顐熷亾婵犳碍鈷旈柛鏇ㄥ灡閻撴洘绻涢幋鐐╂闁割偁鍎辩粻?
func printProxyExitedBanner(writer io.Writer) {
	if writer == nil {
		return
	}
	_, _ = fmt.Fprint(writer, "\r\n[ NeoCode Proxy exited ]\r\n")
}

// enableHostTerminalRawMode 闂傚倸鍊搁崐鎼佸磹閹间礁纾圭€瑰嫭鍣磋ぐ鎺戠倞鐟滃繘寮抽敃鍌涚厱妞ゎ厽鍨垫禍婵嬫煕濞嗗繒绠婚柡宀嬬秮婵偓闁靛繆鏅濋崝鍝ョ磽娴ｆ彃浜炬繝銏ｆ硾椤戝嫮鎹㈤崱娑欑厪闁割偅绻冮崳娲煕閿濆懏璐＄紒杈ㄥ浮楠炲洭顢樿閻や線姊洪崫鍕効缂佺粯绻傞悾鐑藉醇閺囩倣銊╂煏婢诡垰鍊诲Λ顖炴⒒?enableHostTerminalRawMode 闂傚倸鍊搁崐鎼佸磹閹间礁纾归柟闂寸绾惧綊鏌ｉ幋锝呅撻柛銈呭閺屾盯顢曢敐鍡欘槬缂佺偓鍎冲锟犲蓟閿濆顫呴柕蹇婃櫇閸旀悂姊哄Ч鍥р偓妤呭磻閹捐埖宕叉繝闈涱儐椤ュ牊绻涢幋婵嗚埞闁告搩鍓熷娲焻閻愯尪瀚板褍澧界槐鎺楁偐閾忣偄纰嶉梺浼欑秮閺€杈╃紦閻ｅ瞼鐭欓悹鍥﹀嫎閸旀垿寮婚弴鐔虹彾妞ゆ牗纰嶉崳瑙勵殽閻愨晛浜鹃梻鍌氬€峰ù鍥х暦閻㈢绐楅柟閭﹀枛閸ㄦ繄鈧箍鍎遍幉妯衡槈閵忕姷顦ㄥ銈庡幗閸ㄩ潧鈻撻妸锔剧瘈闁汇垽娼ф牎闂佽壈顫夐崕鎶筋敊韫囨挴鏀介悗锝庡亞閸樺憡绻濋姀锝嗙【妞ゆ垵鎳橀幃姗€濡疯閸嬫挸鈻撻崹顔界亾闂佺绻戦敋閾荤偤鏌涘☉娆愬剹闁轰礁鍟撮弻鏇＄疀婵炴儳浜鹃柟棰佺劍琚╂繝鐢靛Х閺佹悂宕戝☉姗嗗殨闁割偅娲橀崑瀣節婵犲倹鍣归柛銊︾箞閺屽秹宕崟顐熷亾婵犳碍鈷旈柛鏇ㄥ灡閻撴洘绻涢幋鐐╂闁割偁鍎辩粻?
func enableHostTerminalRawMode() (func() error, error) {
	if hostTerminalInput == nil {
		return func() error { return nil }, nil
	}

	fd := int(hostTerminalInput.Fd())
	if !isTerminalFD(fd) {
		return func() error { return nil }, nil
	}

	originalState, err := makeRawTerminal(fd)
	if err != nil {
		return nil, fmt.Errorf("ptyproxy: set host terminal raw mode: %w", err)
	}
	return func() error {
		if restoreErr := restoreTerminal(fd, originalState); restoreErr != nil {
			return fmt.Errorf("ptyproxy: restore host terminal state: %w", restoreErr)
		}
		return nil
	}, nil
}

// installHostTerminalRestoreGuard 闂傚倸鍊搁崐鎼佸磹閹间礁纾圭€瑰嫭鍣磋ぐ鎺戠倞鐟滃繘寮抽敃鍌涚厱妞ゎ厽鍨垫禍婵嬫煕濞嗗繒绠婚柡宀嬬秮婵偓闁靛繆鏅濋崝鍝ョ磽娴ｆ彃浜炬繝銏ｆ硾椤戝嫮鎹㈤崱娑欑厪闁割偅绻冮崳娲煕閿濆懏璐＄紒杈ㄥ浮楠炲洭顢樿閻や線姊洪崫鍕効缂佺粯绻傞悾鐑藉醇閺囩倣銊╂煏婢诡垰鍊诲Λ顖炴⒒?installHostTerminalRestoreGuard 闂傚倸鍊搁崐鎼佸磹閹间礁纾归柟闂寸绾惧綊鏌ｉ幋锝呅撻柛銈呭閺屾盯顢曢敐鍡欘槬缂佺偓鍎冲锟犲蓟閿濆顫呴柕蹇婃櫇閸旀悂姊哄Ч鍥р偓妤呭磻閹捐埖宕叉繝闈涱儐椤ュ牊绻涢幋婵嗚埞闁告搩鍓熷娲焻閻愯尪瀚板褍澧界槐鎺楁偐閾忣偄纰嶉梺浼欑秮閺€杈╃紦閻ｅ瞼鐭欓悹鍥﹀嫎閸旀垿寮婚弴鐔虹彾妞ゆ牗纰嶉崳瑙勵殽閻愨晛浜鹃梻鍌氬€峰ù鍥х暦閻㈢绐楅柟閭﹀枛閸ㄦ繄鈧箍鍎遍幉妯衡槈閵忕姷顦ㄥ銈庡幗閸ㄩ潧鈻撻妸锔剧瘈闁汇垽娼ф牎闂佽壈顫夐崕鎶筋敊韫囨挴鏀介悗锝庡亞閸樺憡绻濋姀锝嗙【妞ゆ垵鎳橀幃姗€濡疯閸嬫挸鈻撻崹顔界亾闂佺绻戦敋閾荤偤鏌涘☉娆愬剹闁轰礁鍟撮弻鏇＄疀婵炴儳浜鹃柟棰佺劍琚╂繝鐢靛Х閺佹悂宕戝☉姗嗗殨闁割偅娲橀崑瀣節婵犲倹鍣归柛銊︾箞閺屽秹宕崟顐熷亾婵犳碍鈷旈柛鏇ㄥ灡閻撴洘绻涢幋鐐╂闁割偁鍎辩粻?
func installHostTerminalRestoreGuard() func() {
	if hostTerminalInput == nil {
		return func() {}
	}
	fd := int(hostTerminalInput.Fd())
	if !isTerminalFD(fd) {
		return func() {}
	}
	state, err := term.GetState(fd)
	if err != nil {
		return func() {}
	}
	return func() {
		_ = restoreTerminal(fd, state)
	}
}

// syncPTYWindowSize 闂傚倸鍊搁崐鎼佸磹閹间礁纾圭€瑰嫭鍣磋ぐ鎺戠倞鐟滃繘寮抽敃鍌涚厱妞ゎ厽鍨垫禍婵嬫煕濞嗗繒绠婚柡宀嬬秮婵偓闁靛繆鏅濋崝鍝ョ磽娴ｆ彃浜炬繝銏ｆ硾椤戝嫮鎹㈤崱娑欑厪闁割偅绻冮崳娲煕閿濆懏璐＄紒杈ㄥ浮楠炲洭顢樿閻や線姊洪崫鍕効缂佺粯绻傞悾鐑藉醇閺囩倣銊╂煏婢诡垰鍊诲Λ顖炴⒒?syncPTYWindowSize 闂傚倸鍊搁崐鎼佸磹閹间礁纾归柟闂寸绾惧綊鏌ｉ幋锝呅撻柛銈呭閺屾盯顢曢敐鍡欘槬缂佺偓鍎冲锟犲蓟閿濆顫呴柕蹇婃櫇閸旀悂姊哄Ч鍥р偓妤呭磻閹捐埖宕叉繝闈涱儐椤ュ牊绻涢幋婵嗚埞闁告搩鍓熷娲焻閻愯尪瀚板褍澧界槐鎺楁偐閾忣偄纰嶉梺浼欑秮閺€杈╃紦閻ｅ瞼鐭欓悹鍥﹀嫎閸旀垿寮婚弴鐔虹彾妞ゆ牗纰嶉崳瑙勵殽閻愨晛浜鹃梻鍌氬€峰ù鍥х暦閻㈢绐楅柟閭﹀枛閸ㄦ繄鈧箍鍎遍幉妯衡槈閵忕姷顦ㄥ銈庡幗閸ㄩ潧鈻撻妸锔剧瘈闁汇垽娼ф牎闂佽壈顫夐崕鎶筋敊韫囨挴鏀介悗锝庡亞閸樺憡绻濋姀锝嗙【妞ゆ垵鎳橀幃姗€濡疯閸嬫挸鈻撻崹顔界亾闂佺绻戦敋閾荤偤鏌涘☉娆愬剹闁轰礁鍟撮弻鏇＄疀婵炴儳浜鹃柟棰佺劍琚╂繝鐢靛Х閺佹悂宕戝☉姗嗗殨闁割偅娲橀崑瀣節婵犲倹鍣归柛銊︾箞閺屽秹宕崟顐熷亾婵犳碍鈷旈柛鏇ㄥ灡閻撴洘绻涢幋鐐╂闁割偁鍎辩粻?
func syncPTYWindowSize(errWriter io.Writer, ptyFile *os.File) {
	if ptyFile == nil {
		return
	}
	if err := pty.InheritSize(os.Stdin, ptyFile); err != nil && errWriter != nil {
		writeProxyf(errWriter, "neocode shell: inherit terminal size failed: %v\n", err)
	}
}

// watchPTYWindowResize 闂傚倸鍊搁崐鎼佸磹閹间礁纾圭€瑰嫭鍣磋ぐ鎺戠倞鐟滃繘寮抽敃鍌涚厱妞ゎ厽鍨垫禍婵嬫煕濞嗗繒绠婚柡宀嬬秮婵偓闁靛繆鏅濋崝鍝ョ磽娴ｆ彃浜炬繝銏ｆ硾椤戝嫮鎹㈤崱娑欑厪闁割偅绻冮崳娲煕閿濆懏璐＄紒杈ㄥ浮楠炲洭顢樿閻や線姊洪崫鍕効缂佺粯绻傞悾鐑藉醇閺囩倣銊╂煏婢诡垰鍊诲Λ顖炴⒒?watchPTYWindowResize 闂傚倸鍊搁崐鎼佸磹閹间礁纾归柟闂寸绾惧綊鏌ｉ幋锝呅撻柛銈呭閺屾盯顢曢敐鍡欘槬缂佺偓鍎冲锟犲蓟閿濆顫呴柕蹇婃櫇閸旀悂姊哄Ч鍥р偓妤呭磻閹捐埖宕叉繝闈涱儐椤ュ牊绻涢幋婵嗚埞闁告搩鍓熷娲焻閻愯尪瀚板褍澧界槐鎺楁偐閾忣偄纰嶉梺浼欑秮閺€杈╃紦閻ｅ瞼鐭欓悹鍥﹀嫎閸旀垿寮婚弴鐔虹彾妞ゆ牗纰嶉崳瑙勵殽閻愨晛浜鹃梻鍌氬€峰ù鍥х暦閻㈢绐楅柟閭﹀枛閸ㄦ繄鈧箍鍎遍幉妯衡槈閵忕姷顦ㄥ銈庡幗閸ㄩ潧鈻撻妸锔剧瘈闁汇垽娼ф牎闂佽壈顫夐崕鎶筋敊韫囨挴鏀介悗锝庡亞閸樺憡绻濋姀锝嗙【妞ゆ垵鎳橀幃姗€濡疯閸嬫挸鈻撻崹顔界亾闂佺绻戦敋閾荤偤鏌涘☉娆愬剹闁轰礁鍟撮弻鏇＄疀婵炴儳浜鹃柟棰佺劍琚╂繝鐢靛Х閺佹悂宕戝☉姗嗗殨闁割偅娲橀崑瀣節婵犲倹鍣归柛銊︾箞閺屽秹宕崟顐熷亾婵犳碍鈷旈柛鏇ㄥ灡閻撴洘绻涢幋鐐╂闁割偁鍎辩粻?
func watchPTYWindowResize(errWriter io.Writer, ptyFile *os.File) func() {
	if ptyFile == nil {
		return func() {}
	}

	winchSignals := make(chan os.Signal, 1)
	signal.Notify(winchSignals, syscall.SIGWINCH)

	stopCh := make(chan struct{})
	var stopOnce sync.Once
	var watcherWG sync.WaitGroup
	watcherWG.Add(1)
	go func() {
		defer watcherWG.Done()
		for {
			select {
			case <-stopCh:
				return
			case _, ok := <-winchSignals:
				if !ok {
					return
				}
				syncPTYWindowSize(errWriter, ptyFile)
			}
		}
	}()

	return func() {
		stopOnce.Do(func() {
			signal.Stop(winchSignals)
			close(stopCh)
			watcherWG.Wait()
		})
	}
}

// watchForwardSignals 闂傚倸鍊搁崐鎼佸磹閹间礁纾圭€瑰嫭鍣磋ぐ鎺戠倞鐟滃繘寮抽敃鍌涚厱妞ゎ厽鍨垫禍婵嬫煕濞嗗繒绠婚柡宀嬬秮婵偓闁靛繆鏅濋崝鍝ョ磽娴ｆ彃浜炬繝銏ｆ硾椤戝嫮鎹㈤崱娑欑厪闁割偅绻冮崳娲煕閿濆懏璐＄紒杈ㄥ浮楠炲洭顢樿閻や線姊洪崫鍕効缂佺粯绻傞悾鐑藉醇閺囩倣銊╂煏婢诡垰鍊诲Λ顖炴⒒?watchForwardSignals 闂傚倸鍊搁崐鎼佸磹閹间礁纾归柟闂寸绾惧綊鏌ｉ幋锝呅撻柛銈呭閺屾盯顢曢敐鍡欘槬缂佺偓鍎冲锟犲蓟閿濆顫呴柕蹇婃櫇閸旀悂姊哄Ч鍥р偓妤呭磻閹捐埖宕叉繝闈涱儐椤ュ牊绻涢幋婵嗚埞闁告搩鍓熷娲焻閻愯尪瀚板褍澧界槐鎺楁偐閾忣偄纰嶉梺浼欑秮閺€杈╃紦閻ｅ瞼鐭欓悹鍥﹀嫎閸旀垿寮婚弴鐔虹彾妞ゆ牗纰嶉崳瑙勵殽閻愨晛浜鹃梻鍌氬€峰ù鍥х暦閻㈢绐楅柟閭﹀枛閸ㄦ繄鈧箍鍎遍幉妯衡槈閵忕姷顦ㄥ銈庡幗閸ㄩ潧鈻撻妸锔剧瘈闁汇垽娼ф牎闂佽壈顫夐崕鎶筋敊韫囨挴鏀介悗锝庡亞閸樺憡绻濋姀锝嗙【妞ゆ垵鎳橀幃姗€濡疯閸嬫挸鈻撻崹顔界亾闂佺绻戦敋閾荤偤鏌涘☉娆愬剹闁轰礁鍟撮弻鏇＄疀婵炴儳浜鹃柟棰佺劍琚╂繝鐢靛Х閺佹悂宕戝☉姗嗗殨闁割偅娲橀崑瀣節婵犲倹鍣归柛銊︾箞閺屽秹宕崟顐熷亾婵犳碍鈷旈柛鏇ㄥ灡閻撴洘绻涢幋鐐╂闁割偁鍎辩粻?
func watchForwardSignals(process *os.Process, errWriter io.Writer, interceptor func(os.Signal) bool) func() {
	if process == nil {
		return func() {}
	}
	proxySignals := make(chan os.Signal, 1)
	signal.Notify(proxySignals, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTSTP, syscall.SIGCONT)
	stopCh := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stopCh:
				return
			case signalValue, ok := <-proxySignals:
				if !ok {
					return
				}
				if interceptor != nil && interceptor(signalValue) {
					continue
				}
				sysSignal, ok := signalValue.(syscall.Signal)
				if !ok {
					continue
				}
				if process.Pid <= 0 {
					continue
				}
				if err := syscall.Kill(-process.Pid, sysSignal); err != nil && errWriter != nil {
					writeProxyf(errWriter, "neocode shell: forward signal %d failed: %v\n", sysSignal, err)
				}
			}
		}
	}()
	return func() {
		signal.Stop(proxySignals)
		close(stopCh)
		wg.Wait()
	}
}

// writeProxyText 闂傚倸鍊搁崐鎼佸磹閹间礁纾圭€瑰嫭鍣磋ぐ鎺戠倞鐟滃繘寮抽敃鍌涚厱妞ゎ厽鍨垫禍婵嬫煕濞嗗繒绠婚柡宀嬬秮婵偓闁靛繆鏅濋崝鍝ョ磽娴ｆ彃浜炬繝銏ｆ硾椤戝嫮鎹㈤崱娑欑厪闁割偅绻冮崳娲煕閿濆懏璐＄紒杈ㄥ浮楠炲洭顢樿閻や線姊洪崫鍕効缂佺粯绻傞悾鐑藉醇閺囩倣銊╂煏婢诡垰鍊诲Λ顖炴⒒?writeProxyText 闂傚倸鍊搁崐鎼佸磹閹间礁纾归柟闂寸绾惧綊鏌ｉ幋锝呅撻柛銈呭閺屾盯顢曢敐鍡欘槬缂佺偓鍎冲锟犲蓟閿濆顫呴柕蹇婃櫇閸旀悂姊哄Ч鍥р偓妤呭磻閹捐埖宕叉繝闈涱儐椤ュ牊绻涢幋婵嗚埞闁告搩鍓熷娲焻閻愯尪瀚板褍澧界槐鎺楁偐閾忣偄纰嶉梺浼欑秮閺€杈╃紦閻ｅ瞼鐭欓悹鍥﹀嫎閸旀垿寮婚弴鐔虹彾妞ゆ牗纰嶉崳瑙勵殽閻愨晛浜鹃梻鍌氬€峰ù鍥х暦閻㈢绐楅柟閭﹀枛閸ㄦ繄鈧箍鍎遍幉妯衡槈閵忕姷顦ㄥ銈庡幗閸ㄩ潧鈻撻妸锔剧瘈闁汇垽娼ф牎闂佽壈顫夐崕鎶筋敊韫囨挴鏀介悗锝庡亞閸樺憡绻濋姀锝嗙【妞ゆ垵鎳橀幃姗€濡疯閸嬫挸鈻撻崹顔界亾闂佺绻戦敋閾荤偤鏌涘☉娆愬剹闁轰礁鍟撮弻鏇＄疀婵炴儳浜鹃柟棰佺劍琚╂繝鐢靛Х閺佹悂宕戝☉姗嗗殨闁割偅娲橀崑瀣節婵犲倹鍣归柛銊︾箞閺屽秹宕崟顐熷亾婵犳碍鈷旈柛鏇ㄥ灡閻撴洘绻涢幋鐐╂闁割偁鍎辩粻?
func writeProxyText(writer io.Writer, text string) {
	if writer == nil || text == "" {
		return
	}
	_, _ = io.WriteString(writer, proxyOutputLineEndingNormalizer.Replace(text))
}

// writeProxyLine 闂傚倸鍊搁崐鎼佸磹閹间礁纾圭€瑰嫭鍣磋ぐ鎺戠倞鐟滃繘寮抽敃鍌涚厱妞ゎ厽鍨垫禍婵嬫煕濞嗗繒绠婚柡宀嬬秮婵偓闁靛繆鏅濋崝鍝ョ磽娴ｆ彃浜炬繝銏ｆ硾椤戝嫮鎹㈤崱娑欑厪闁割偅绻冮崳娲煕閿濆懏璐＄紒杈ㄥ浮楠炲洭顢樿閻や線姊洪崫鍕効缂佺粯绻傞悾鐑藉醇閺囩倣銊╂煏婢诡垰鍊诲Λ顖炴⒒?writeProxyLine 闂傚倸鍊搁崐鎼佸磹閹间礁纾归柟闂寸绾惧綊鏌ｉ幋锝呅撻柛銈呭閺屾盯顢曢敐鍡欘槬缂佺偓鍎冲锟犲蓟閿濆顫呴柕蹇婃櫇閸旀悂姊哄Ч鍥р偓妤呭磻閹捐埖宕叉繝闈涱儐椤ュ牊绻涢幋婵嗚埞闁告搩鍓熷娲焻閻愯尪瀚板褍澧界槐鎺楁偐閾忣偄纰嶉梺浼欑秮閺€杈╃紦閻ｅ瞼鐭欓悹鍥﹀嫎閸旀垿寮婚弴鐔虹彾妞ゆ牗纰嶉崳瑙勵殽閻愨晛浜鹃梻鍌氬€峰ù鍥х暦閻㈢绐楅柟閭﹀枛閸ㄦ繄鈧箍鍎遍幉妯衡槈閵忕姷顦ㄥ銈庡幗閸ㄩ潧鈻撻妸锔剧瘈闁汇垽娼ф牎闂佽壈顫夐崕鎶筋敊韫囨挴鏀介悗锝庡亞閸樺憡绻濋姀锝嗙【妞ゆ垵鎳橀幃姗€濡疯閸嬫挸鈻撻崹顔界亾闂佺绻戦敋閾荤偤鏌涘☉娆愬剹闁轰礁鍟撮弻鏇＄疀婵炴儳浜鹃柟棰佺劍琚╂繝鐢靛Х閺佹悂宕戝☉姗嗗殨闁割偅娲橀崑瀣節婵犲倹鍣归柛銊︾箞閺屽秹宕崟顐熷亾婵犳碍鈷旈柛鏇ㄥ灡閻撴洘绻涢幋鐐╂闁割偁鍎辩粻?
func writeProxyLine(writer io.Writer, text string) {
	writeProxyText(writer, text+"\n")
}

// writeProxyf 闂傚倸鍊搁崐鎼佸磹閹间礁纾圭€瑰嫭鍣磋ぐ鎺戠倞鐟滃繘寮抽敃鍌涚厱妞ゎ厽鍨垫禍婵嬫煕濞嗗繒绠婚柡宀嬬秮婵偓闁靛繆鏅濋崝鍝ョ磽娴ｆ彃浜炬繝銏ｆ硾椤戝嫮鎹㈤崱娑欑厪闁割偅绻冮崳娲煕閿濆懏璐＄紒杈ㄥ浮楠炲洭顢樿閻や線姊洪崫鍕効缂佺粯绻傞悾鐑藉醇閺囩倣銊╂煏婢诡垰鍊诲Λ顖炴⒒?writeProxyf 闂傚倸鍊搁崐鎼佸磹閹间礁纾归柟闂寸绾惧綊鏌ｉ幋锝呅撻柛銈呭閺屾盯顢曢敐鍡欘槬缂佺偓鍎冲锟犲蓟閿濆顫呴柕蹇婃櫇閸旀悂姊哄Ч鍥р偓妤呭磻閹捐埖宕叉繝闈涱儐椤ュ牊绻涢幋婵嗚埞闁告搩鍓熷娲焻閻愯尪瀚板褍澧界槐鎺楁偐閾忣偄纰嶉梺浼欑秮閺€杈╃紦閻ｅ瞼鐭欓悹鍥﹀嫎閸旀垿寮婚弴鐔虹彾妞ゆ牗纰嶉崳瑙勵殽閻愨晛浜鹃梻鍌氬€峰ù鍥х暦閻㈢绐楅柟閭﹀枛閸ㄦ繄鈧箍鍎遍幉妯衡槈閵忕姷顦ㄥ銈庡幗閸ㄩ潧鈻撻妸锔剧瘈闁汇垽娼ф牎闂佽壈顫夐崕鎶筋敊韫囨挴鏀介悗锝庡亞閸樺憡绻濋姀锝嗙【妞ゆ垵鎳橀幃姗€濡疯閸嬫挸鈻撻崹顔界亾闂佺绻戦敋閾荤偤鏌涘☉娆愬剹闁轰礁鍟撮弻鏇＄疀婵炴儳浜鹃柟棰佺劍琚╂繝鐢靛Х閺佹悂宕戝☉姗嗗殨闁割偅娲橀崑瀣節婵犲倹鍣归柛銊︾箞閺屽秹宕崟顐熷亾婵犳碍鈷旈柛鏇ㄥ灡閻撴洘绻涢幋鐐╂闁割偁鍎辩粻?
func writeProxyf(writer io.Writer, format string, args ...any) {
	writeProxyText(writer, fmt.Sprintf(format, args...))
}

// buildShellCommand 闂傚倸鍊搁崐鎼佸磹閹间礁纾圭€瑰嫭鍣磋ぐ鎺戠倞鐟滃繘寮抽敃鍌涚厱妞ゎ厽鍨垫禍婵嬫煕濞嗗繒绠婚柡宀嬬秮婵偓闁靛繆鏅濋崝鍝ョ磽娴ｆ彃浜炬繝銏ｆ硾椤戝嫮鎹㈤崱娑欑厪闁割偅绻冮崳娲煕閿濆懏璐＄紒杈ㄥ浮楠炲洭顢樿閻や線姊洪崫鍕効缂佺粯绻傞悾鐑藉醇閺囩倣銊╂煏婢诡垰鍊诲Λ顖炴⒒?buildShellCommand 闂傚倸鍊搁崐鎼佸磹閹间礁纾归柟闂寸绾惧綊鏌ｉ幋锝呅撻柛銈呭閺屾盯顢曢敐鍡欘槬缂佺偓鍎冲锟犲蓟閿濆顫呴柕蹇婃櫇閸旀悂姊哄Ч鍥р偓妤呭磻閹捐埖宕叉繝闈涱儐椤ュ牊绻涢幋婵嗚埞闁告搩鍓熷娲焻閻愯尪瀚板褍澧界槐鎺楁偐閾忣偄纰嶉梺浼欑秮閺€杈╃紦閻ｅ瞼鐭欓悹鍥﹀嫎閸旀垿寮婚弴鐔虹彾妞ゆ牗纰嶉崳瑙勵殽閻愨晛浜鹃梻鍌氬€峰ù鍥х暦閻㈢绐楅柟閭﹀枛閸ㄦ繄鈧箍鍎遍幉妯衡槈閵忕姷顦ㄥ銈庡幗閸ㄩ潧鈻撻妸锔剧瘈闁汇垽娼ф牎闂佽壈顫夐崕鎶筋敊韫囨挴鏀介悗锝庡亞閸樺憡绻濋姀锝嗙【妞ゆ垵鎳橀幃姗€濡疯閸嬫挸鈻撻崹顔界亾闂佺绻戦敋閾荤偤鏌涘☉娆愬剹闁轰礁鍟撮弻鏇＄疀婵炴儳浜鹃柟棰佺劍琚╂繝鐢靛Х閺佹悂宕戝☉姗嗗殨闁割偅娲橀崑瀣節婵犲倹鍣归柛銊︾箞閺屽秹宕崟顐熷亾婵犳碍鈷旈柛鏇ㄥ灡閻撴洘绻涢幋鐐╂闁割偁鍎辩粻?
func buildShellCommand(shellPath string, options ManualShellOptions, shellSessionID string) (*exec.Cmd, func()) {
	command := exec.Command(shellPath)
	command.Dir = options.Workdir
	command.Env = MergeEnvVar(os.Environ(), ShellSessionEnv, strings.TrimSpace(shellSessionID))

	cleanupTasks := make([]func(), 0, 2)
	cleanup := func() {
		for index := len(cleanupTasks) - 1; index >= 0; index-- {
			cleanupTasks[index]()
		}
	}
	if rcFile := prepareBashInitRC(shellPath); rcFile != "" {
		command.Args = append(command.Args, "--rcfile", rcFile)
		cleanupTasks = append(cleanupTasks, func() { deleteBashInitRCFile(rcFile) })
	}
	if zdotDir := prepareZshInitDir(shellPath); zdotDir != "" {
		command.Env = MergeEnvVar(command.Env, "ZDOTDIR", zdotDir)
		cleanupTasks = append(cleanupTasks, func() { deleteZshInitDir(zdotDir) })
	}
	return command, cleanup
}

// generateShellSessionID 闂傚倸鍊搁崐鎼佸磹閹间礁纾归柟闂寸绾惧綊鏌ｉ幋锝呅撻柛濠傛健閺屻劑寮撮悙娴嬪亾瑜版帒纾块柟瀵稿У閸犳劙鏌ｅΔ鈧悧鍡欑箔閹烘鐓曢柕濞垮劚閳ь剚鎮傞崺鐐哄箣閿旇棄浜归柣搴℃贡婵挳藟濠靛牏纾藉ù锝囨嚀婵牊銇勯妸銉含鐎殿喖顭烽幃銏ゅ礂閻撳簼缃曢梻浣稿閸嬪棝宕伴幘璇茬闁跨喓濮甸埛鎴︽煙閼测晛浠滃┑鈥炽偢閺屾稒绻濋崒婊呅ㄥΔ鐘靛仜閻楁挻淇婇幖浣肝ㄩ幖杈剧到閺嬫盯鏌熼鐟板⒉闁诡垱鏌ㄩ埞鎴﹀醇閵忕媭妲辨繝鐢靛Х閺佹悂宕戝☉銏″仱闁靛ě鍐ㄧ亰閻庡厜鍋撻柍褜鍓涘Σ鎰版倷鐎靛摜鐦堥梺鎼炲劀閸℃ɑ鍟洪梻鍌欒兌缁垰顫忔繝姘偍鐟滃繒鍒掔拠宸僵閺夊牄鍔岄弸鎴︽倵楠炲灝鍔氭い锔诲灠鏁堥柡灞诲劚缁狙囨煕椤愶絿鈽夊┑锛勵焾闇?shell 濠电姷鏁告慨鐑藉极閸涘﹥鍙忛柣鎴ｆ閺嬩線鏌熼梻瀵割槮缁惧墽绮换娑㈠箣濞嗗繒鍔撮梺杞扮椤戝棝濡甸崟顖氱閻犺櫣鍎ら悗濠氭⒑閹肩偛濡芥俊顐㈠暣楠炲啫螖閸涱喖浠哄┑鐐茬墣濞夋洟宕濋崫銉х＝濞达絿鏅崼顏堟煕婵犲啰绠炵€殿喛顕ч埥澶婎煥鎼粹懣顏呬繆閻愵亜鈧垿宕濆畝鍕疇闁归偊鍠栭崹婵嗏攽閻樺疇澹橀柛鎰ㄥ亾闁荤喐绮岄ˇ闈涱嚕閵婏妇顩烽悗锝庡亞閸樿棄鈹戦埥鍡楃仴妞ゆ泦鍛筏濠电姵纰嶉悡娑㈡煕閳╁喚鐒介柟鍐插閺屾洟宕惰椤忣厾鈧鍠楅幐鎶藉极閹剧粯鍋愰柛娆忣槹閸ゅ牓姊婚崒娆戠獢闁逞屽墯缁嬫捇鍩為幒妤佺厱闁哄倽娉曢悞鐑芥煟韫囨柨娴慨濠冩そ楠炲棜顦寸紒鐘烘珪缁绘盯宕ㄩ銏紕濠碘€冲级閸旀瑩骞冨▎寰濆湱鈧綆鍋呴弶鎼佹⒒娴ｅ摜鏋冩俊顐㈠铻炴俊銈呮噺閸婂爼鏌ｅΟ鑲╁笡闁绘挾鍠愭穱濠囧Χ閸曨厼濡介悗瑙勬礀閻ジ鍩€椤掍緡鍟忛柛鐘崇墵閳ワ箓鎮滈挊澶婄€俊銈忕到閸燁偆绮诲☉妯忓綊鏁愰崨顔兼殘闂佺顭崹璺侯潖缂佹ɑ濯撮柛娑橈工閺嗗牊绻涢幘瀵割暡妞ゃ劌锕ら悾鐑藉级濞嗙偓鍍甸柣鐘荤細濞咃綁宕㈤弶鎴旀斀闁绘劖娼欓悘鐔兼煕閵婏附銇濋柛鈺傜洴楠炲顭垮┑鍡欑Ш鐎规洘顨婇幊鏍煛閸屾碍鐝板┑鐘殿暜缁辨洟鎼规惔銊ラ棷闁挎繂顦拑鐔哥箾閹存瑥鐏╅幆鐔兼⒑閹稿孩纾甸柛瀣尰閵囧嫰寮撮悢铏圭厐闁?
func generateShellSessionID(pid int) string {
	if pid <= 0 {
		pid = os.Getpid()
	}
	sequence := shellSessionSeq.Add(1)
	return fmt.Sprintf("%s-%d-%d", shellSessionPrefix, pid, sequence)
}

// bindShellRoleStream 缂傚倸鍊搁崐鎼佸磹閹间礁纾归柟闂寸绾惧綊鏌熼梻瀵割槮缁炬儳缍婇弻鐔兼⒒鐎靛壊妲紒鐐劤缂嶅﹪寮婚敐澶婄闁挎繂鎲涢幘缁樼厱濠电姴鍊归崑銉╂煛鐏炶濮傜€殿喗鎸抽幃娆徝圭€ｎ亙澹曢梺褰掓？缁€渚€宕归崒鐐寸厱鐎光偓閳ь剟宕戦悙鐑樺亗闁绘柨鍚嬮悡蹇涚叓閸ャ劍绀€閺嶏繝姊洪崫銉ユ灁闁稿鍊濆?shell 闂傚倸鍊搁崐鎼佸磹閹间礁纾圭€瑰嫭鍣磋ぐ鎺戠倞鐟滃繘寮抽敃鍌涚厽闁靛繈鍩勯悞鍓х磼閹邦収娈滈柡宀€鍠栭獮宥夘敊绾拌鲸姣夐梻浣侯焾椤戞垹鎹㈠┑瀣摕闁靛ň鏅涚猾宥夋煕鐏炲墽鐓瑙勬礋濮婃椽宕崟顒夋！缂備緡鍠楅幑鍥ь嚕婵犳碍鏅插璺猴攻椤ユ繈姊洪崷顓х劸閻庢稈鏅犲畷浼村箛閻楀牃鎷虹紓鍌欑劍椤洨绮诲顓犵濠㈣泛顑囧ú鎾煕閳哄啫浠辨鐐差儔閺佸倿鎸婃径澶嬬潖闂傚倸鍊搁…顒勫磻閹烘鍌ㄩ柛鎾楀懐鐒奸梺閫炲苯澧存慨濠冩そ閹筹繝濡堕崨顔锯偓楣冩⒑閼姐倕鏋傞柛搴㈠▕閸┾偓妞ゆ帊绀侀崵顒勬煕閿濆繒绉鐐插暣閹粓鎳為妷銉ょ敾闂備線鈧稓绁锋繛鍛礋瀵剟鍩€椤掑嫭鈷掑ù锝堟鐢盯鎷戞潏鈺傚枑闁哄鐏濋弳锝嗐亜閵忊剝顥堟い銏℃礋閺佹劙宕樿閻╁酣姊绘担鐟邦嚋婵☆偂绶氶、姘愁樄鐎殿喚顭堥埥澶愬閳╁啫娈欓梻浣告惈缁嬩線宕㈡禒瀣亗闁绘柨鎲￠崣蹇斾繆閻愰鍤欏ù婊勫劤閳规垿鎮欓弶鍨殶闂佸憡娲﹂崣搴∶归崟顖涒拺闁圭瀛╅埛鎺楁煛閸滀礁浜炵€殿啫鍥х劦妞ゆ帒瀚埛鎴︽⒑椤愩倕浠滈柤娲诲灡閺呭墎鈧數纭堕崑鎾舵喆閸曨剛顦梺绋跨箲閿曘垽濡存担鍓叉建闁逞屽墴楠炲啴鍩￠崨顔间缓闂佸壊鐓堥崑鍛搭敊閺冨牊鈷掗柛灞捐壘閳ь剛鍏橀幊妤呭醇閺囨せ鍋撻敃鍌氶唶闁靛绠戦崜鑸电節闂堟稑鈧鈥﹂崼銏狀棜缂備焦顭囩粻楣冩煙鐎电浠﹂柣銊﹀灥閳藉骞樺畷鍥嗭綁鏌曢崶褍顏€殿喗娼欒灃闁逞屽墯缁傚秵銈ｉ崘鈹炬嫼闂佸憡绻傜€氼厼锕㈤悧鍫㈢闁告瑥顥㈤鍫晪闁靛鏅涚粈瀣亜閹邦喖鏋戞い顐㈢Ч濮婃椽妫冨☉姘辩暰濠碘剝褰冮幊姗€宕洪埀顒併亜閹哄棗浜剧紓浣割槺閺佹悂骞戦姀鐘婵﹫绲芥禍楣冩煟閵忊槅鍟忛柣鎺楃畺瀵粙鏁撻悩鏂ユ嫼闂佸湱顭堢€涒晝澹曢幖浣圭厱闁靛鍔嶉ˉ澶嬨亜?auto 闂傚倸鍊搁崐鎼佸磹閹间礁纾归柟闂寸绾惧綊鏌ｉ幋锝呅撻柛銈呭閺屾盯骞橀懠顒夋М闂佹悶鍔嶇换鍐Φ閸曨垰鍐€妞ゆ劦婢€濮规姊洪柅鐐茶嫰婢у墽绱掗悩铏碍闁伙綁鏀辩缓鐣岀矙鐠囦勘鍔戦弻鏇熷緞濞戙垺顎嶉悶姘剧秮濮婂宕掑▎鎴М闂佸湱鈷堥崑鍡欏垝濞嗘劗鐟归柍褜鍓欓悾鐑藉閿涘嫰妾梺鍛婄☉閿曘倝鍩€椤掆偓濞硷繝寮诲☉鈶┾偓锕傚箣濠靛懐鎸夊┑鐐茬摠缁秶鍒掗幘璇茶摕闁绘棁銆€閸嬫挸鈽夊▎妯煎姺缂備胶濮甸悧鐘诲蓟濞戞埃鍋撻敐鍐ㄥ闁告梹绮岃彁闁搞儜宥堝惈閻庤娲樼划蹇浰囬弻銉︾厱婵☆垳绮亸锕傛煛?
func bindShellRoleStream(
	ctx context.Context,
	rpcClient *gatewayclient.GatewayRPCClient,
	sessionID string,
	autoEnabled bool,
) error {
	if rpcClient == nil {
		return errors.New("gateway rpc client is not ready")
	}
	normalizedSessionID := strings.TrimSpace(sessionID)
	if normalizedSessionID == "" {
		return errors.New("shell session id is empty")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	return bindShellRoleStreamWithCaller(ctx, normalizedSessionID, autoEnabled, func(
		callCtx context.Context,
		params protocol.BindStreamParams,
		ack *gateway.MessageFrame,
	) error {
		return rpcClient.CallWithOptions(
			callCtx,
			protocol.MethodGatewayBindStream,
			params,
			ack,
			gatewayclient.GatewayRPCCallOptions{
				Timeout: diagnoseCallTimeout,
				Retries: 0,
			},
		)
	})
}

// bindShellRoleStreamWithCaller 闂傚倸鍊搁崐鎼佸磹瀹勬噴褰掑炊瑜忛弳锕傛煕椤垵浜濋柛娆忕箻閺岋綁濮€閵忊晝鍔哥紓浣插亾濠㈣泛顑勭换鍡涙煏閸繃鍣洪柛锝呮贡缁辨帡骞囬褎鐤侀梺鍝勭焿缁绘繂鐣烽崼鏇炍ㄩ柕澹倻妫梻?shell 闂傚倸鍊搁崐鎼佸磹瀹勬噴褰掑炊瑜忛弳锕傛煟閵忋埄鐒剧紒鎰殜閺岀喖骞嶉纰辨毉闂佺顑戠换婵嬪蓟閵娾晛绫嶉柛灞剧煯婢规洟姊洪崫鍕棡缂侇喗鎹囧濠氭晲婢跺﹥顥濋梺鍦焾鐎涒晠宕伴幇鐗堚拺缂備焦顭囩粻姘箾婢跺娲撮柛鈺冨仱楠炲鏁傞挊澶夋睏闂備礁婀辩划顖滄暜閳哄倸顕遍柍褜鍓涚槐鎾存媴閸濆嫪澹曢梺绋垮婵炲﹥淇婇崼鏇炵濞达絾鐡曢幗鏇㈡⒑缂佹ɑ顥嗛柕鍡忓亾闂佺顑嗛幐鎼佸煡婢跺ň鏋庢俊顖滃帶婵櫣绱撻崒娆掑厡濠殿喚鏁婚弫鍐閵堝懓鎽曢梺璺ㄥ枔婵挳鐛姀銈嗙厸闁搞儮鏅涙禒婊呯磼娓氬洤娅嶆慨濠呮閹风娀鍨鹃搹顐や邯婵犵數濮崑鎾绘⒑椤掆偓缁夋挳鎮為崹顐犱簻闁圭儤鍩婇崝鐔虹磼婢舵劖娑ч棁澶嬬節婵犲倸顏柣顓熷笧閳ь剚顔栭崰妤佺箾婵犲洤绠栭柕蹇嬪€曠粈鍌炴煠濞村娅呮鐐村姇閳规垿鎮欓懜闈涙锭缂傚倸绉崇粈渚€鈥﹂崶顏嶆▌闂佺硶鏂侀崑鎾愁渻閵堝棗绗傞柤鍐插缁鎮欑喊妯轰壕婵炲牆鐏濆▍姗€鏌涚€ｎ亜顏い銏∩戠缓鐣岀矙鐠恒劌鈧偤姊洪幐搴㈩梿婵☆偄瀚崚濠冨鐎涙ǚ鎷绘繛杈剧导鐠€锕傛倿妤ｅ啯鐓ラ柡鍥朵簽閻ｇ數鈧娲滄晶妤呭箚閺冨牆惟闁挎洍鍋撳ù鐘欏喚娓婚柕鍫濇婢ч亶鏌涚€ｎ剙浠遍柛鈺傜洴楠炲鏁傜憴锝嗗闂備礁澹婇崑鍛枈瀹ュ應鏋嶉柛顐犲灮绾惧ジ鏌嶈閸撴盯鍩€椤掑﹦绉甸柛鐘愁殜閹繝寮撮姀锛勫幐闂佺鏈换鍐兜閸撲讲鍋撳☉娆戠疄闁哄睙鍥ㄥ殥闁靛牆鎳嶅▽顏堟⒑闂堟稒鎼愰悗姘嵆閻涱噣宕堕鈧悡娑樏归敐鍡楃祷濞存粎鍋撴穱濠囧Χ閸涱厽娈銈冨劜閼规崘鐏冮梺鎸庣箓閹冲酣寮抽悙鐑樼厽闁规儳顕幊鍥煙椤旂瓔娈旈柍钘夘槸閳诲骸螣閻撳骸楔缂傚倸鍊烽懗鑸垫叏閻㈢绠板Δ锝呭暙缁犵喖鏌熼梻瀵割槮缂佺姰鍎查妵鍕棘鐟併倓绮撮梺闈涚箞閸婃牠鎮￠崘顔界參婵☆垯璀﹀Ο鍫熶繆椤栨浜惧┑掳鍊楁慨鐑藉磻閻愮儤鏅梻浣风串缁插潡宕楀Ο铏规殾闁割偅娲栭獮銏ゆ煙闁箑澧┑顕嗙畵濮婃椽宕崟顓犲姽缂傚倸绉崇欢姘嚕椤愶箑纾奸柣鎰綑閻у嫭绻濋姀锝嗙【濠靛倹姊诲Σ鎰攽鐎ｎ偆鍘?state 闂傚倸鍊搁崐鎼佸磹瀹勬噴褰掑炊椤掑鏅悷婊冪箻閸┾偓妞ゆ帊鑳堕埢鎾绘煛閸涱喚绠橀柛鎺撳笒閳诲酣骞樺畷鍥跺敽婵犵绱曢崑娑㈡儍閻戣棄纾婚柟鎹愵嚙缁€鍐┿亜閺冨倸甯堕柤鏉跨仢閳规垿鎮欓弶鎴犱紘婵＄偛鐡ㄩ幃鍌氼嚕閺屻儱鐓涢柛娑卞枛閳ь剛鏁婚弻娑滅疀閹垮啯笑婵炲瓨绮撶粻鏍ь潖濞差亜宸濆┑鐘插暙椤︹晠姊虹粙娆惧剰婵☆偅绻堥獮鍡涘醇閵夈儳顦板銈嗙墬缁嬪牓骞忓ú顏呯厽闁绘ê寮剁粈宀勬煃瑜滈崜娆戝椤撶喓顩锋い鎾跺亹閺€浠嬫煟濮楀棗鏋涢柣蹇ｄ邯閺屻劌顫濋懜鐢靛帗缂傚倷鐒﹁摫閻忓繋鍗抽幃妤佹媴閸愩劋姹楅梺閫炲苯澧紒瀣浮閳ワ箓宕堕鈧崒銊╂⒑椤掆偓缁夌敻鍩涢幋锔界厱婵犻潧妫楅鈺呮煃瑜滈崗娑氬垝濞嗘挶鈧礁顫濋幇浣剐梻渚€鈧稓鈹掗柛鏃€鍨甸悾鐑藉箳閹存梹顫嶅┑掳鍊曢崯浼存偩閹€鏀介幒鎶藉磹瑜忓濠冪鐎ｎ亞鐛ュ┑顔斤供閸庨潧鈽夐姀鐘殿吅闂佹寧姊婚弲顐﹀储娴犲鈷戦梻鍫熶緱濡牓鏌涢悩铏闂囧姊洪崹顕呭剭濞存粍绮撻弻銊╁即濡も偓娴滃墽绱撻崒姘毙㈤柨鏇ㄤ簻椤曪絿鎷犲ù瀣潔闂侀潧绻嗛崜婵嗏枍濠婂牊鈷戦柛娑橈梗缁堕亶鏌涢悩鎰佹疁鐎规洑鍗抽獮鍥偋閸碍瀚奸柣鐔哥矌婢ф鏁埡浣勬盯骞嬪┑鍐╂杸闂佺粯鍔忛弲娑欑妤ｅ啯鐓熼幖娣焺閸熷繘鏌涢悩鎰佹當妞ゎ厼娲ら埢搴ㄥ箳閺傛崘鍩呴梻浣筋嚙濞寸兘骞婇幘鍨涘亾濮橆偄宓嗛柕鍡楀€块弫鍌炴偩瀹€鈧ぐ?
func bindShellRoleStreamWithCaller(
	ctx context.Context,
	sessionID string,
	autoEnabled bool,
	caller func(context.Context, protocol.BindStreamParams, *gateway.MessageFrame) error,
) error {
	if caller == nil {
		return errors.New("bind stream caller is nil")
	}
	primaryParams := protocol.BindStreamParams{
		SessionID: strings.TrimSpace(sessionID),
		Channel:   "all",
		Role:      "shell",
		State: map[string]any{
			"auto_enabled": autoEnabled,
		},
	}
	legacyParams := primaryParams
	legacyParams.State = nil

	var ack gateway.MessageFrame
	if err := caller(ctx, primaryParams, &ack); err != nil {
		if !shouldFallbackBindStreamState(err) {
			return err
		}
		ack = gateway.MessageFrame{}
		if retryErr := caller(ctx, legacyParams, &ack); retryErr != nil {
			return retryErr
		}
		return validateBindStreamAckFrame(ack)
	}
	return validateBindStreamAckFrame(ack)
}

// shouldFallbackBindStreamState 闂傚倸鍊搁崐鎼佸磹閹间礁纾瑰瀣捣閻棗銆掑锝呬壕濡ょ姷鍋涢ˇ鐢稿极瀹ュ绀嬫い鎺嶇劍椤斿洭姊绘担瑙勫仩闁稿孩妞藉畷婊冣枎閹存繍妫滈悷婊呭鐢鎮″☉姘ｅ亾楠炲灝鍔氬Δ鐘虫倐閻涱噣寮介鐔哄弮?bind_stream 婵犵數濮烽弫鍛婃叏閻戣棄鏋侀柛娑橈攻閸欏繐霉閸忓吋缍戦柛銊ュ€圭换娑橆啅椤旇崵鐩庣紒鐐劤濞硷繝寮婚妶鍚ゅ湱鈧綆鍋呴悵鏃堟⒑閹肩偛濡界紒璇茬墦瀵鈽夐姀鐘殿啋濠德板€愰崑鎾绘倵濮樼厧澧寸€殿喗鎮傚畷鎺楁倷缁瀚奸梻浣告贡椤牏鈧稈鏅濈划鍫ュ焵椤掑嫭鈷戠紒瀣皡閺€濠氭煙椤旂厧鈧悂鎮鹃悜鑺ユ櫜闁糕剝鐟ч惁鍫ユ⒑閸涘﹤濮х紓鍌涙皑閼鸿鲸绻濆顓涙嫼闂佸憡绻傜€氼喛鈪归梻浣告啞閺屻劑鏌婇敐澶娢ラ柛宀€鍋涢拑鐔兼煏婢舵稑顩柛姗€浜堕弻鐔兼嚃閳哄媻澶嬩繆閹绘帒顏柣鎿冨墴椤㈡宕熼鍌氬箥婵＄偑鍊栧褰掑几婵犳碍鍤€闁秆勵殕閻撴瑦銇勯弮鍌涙珪闁瑰啿娲︽穱濠囶敃閵忕姵娈梺瀹犳椤︻垵鐏冮梺鍛婂姦閸犳浜搁懠顒傜＝闁稿本鑹鹃埀顒佹倐瀹曟劖顦版惔銏╁仺闂佽法鍠撴慨鐢稿磻閸岀偞鐓曟い鎰Т閸旀氨绱掗埀顒勫醇閵夛妇鍘介梺鍝勫€归娆忊枔閻樼粯鐓曢柕鍫濇缁€鍐煏閸パ冾伃鐎殿噮鍣ｉ崺鈧い鎺戝閺佸嫭绻涢崱妤冪翱闁挎繂妫涚弧鈧┑顔斤供閸樺ジ鍩€椤掑倹鏆柟顔煎槻閳诲氦绠涢幙鍐х棯缂傚倷璁查崑鎾绘煕閹般劍鏉哄ù婊勭矒閺屾洘绻涢崹顔煎Б婵犫拃宥夋妞ゃ劊鍎甸幃娆撴嚑椤掑偆鍞洪梻渚€鈧偛鑻晶鍓х磽瀹ュ懏顥㈢€规洘濞婇、姘跺焵椤掆偓椤曪綁顢曢敐鍥╃槇闂佹悶鍎崝搴ㄥ储閻愵剛绡€婵炲牆鐏濋弸鐔兼煕閺冣偓濞茬喖宕洪埀顒併亜閹哄秶璐版繛鍫熺矋椤ㄣ儵鎮欏顔叫梺闈涙处閸旀瑩鐛幒妤€绠绘い鏍ㄧ☉椤忓綊姊绘担钘夊惞闁稿鍋熺划娆撳醇閵夈儳鍔﹀銈嗗坊閸嬫捇鏌涘Ο鑽ゅ缂佹梻鍠栧鎾偄閸撲胶鐣鹃梻浣虹帛閸旓附绂嶅鍫濈劦妞ゆ帊鑳舵晶鐢告煙椤斻劌鍚橀弮鍫濈妞ゅ繐妫涢崢顖炴⒒娴ｄ警鏀伴柟娲讳邯濮婁粙宕熼姘憋紵?state 闂傚倸鍊搁崐鎼佸磹閹间礁纾归柣鎴ｅГ閸婂潡鏌ㄩ弴鐐测偓褰掑磿閹寸姵鍠愰柣妤€鐗嗙粭鎺旂磼閳ь剚寰勭仦绋夸壕闁稿繐顦禍楣冩⒑闁偛鑻晶鎾煕閳规儳浜炬俊鐐€栫敮濠勭矆娓氣偓瀹曠敻顢楅崟顒傚幈闂佽宕樺▔娑㈠几鎼淬劍鐓熼柨婵嗘搐閸樻潙鈹戦敍鍕効妞わ富鍣ｉ弻娑氣偓锝庡亝瀹曞矂鏌℃担鐟板闁诡垱妫冮崹楣冨礃閼碱剦鍚呴梻鍌氬€烽懗鍫曗€﹂崼銉晞闁糕剝绋戠粻鏌ユ煕閵夋垵鎳忓▓楣冩⒑缂佹ê鐏╅柣鈩冩瀵偊宕堕浣哄幗闂佸搫鍟悧濠囧极闁秵鐓熼柨婵嗛婢ь垳绱掔紒妯肩疄闁诡喕绮欏Λ鍐ㄢ槈鏉堛劎绋堥梻鍌欑窔閳ь剛鍋涢懟顖涙櫠鐎电硶鍋撶憴鍕闁搞劌娼￠悰顔嘉熼懖鈺冿紲濠碘槅鍨堕弨杈┾偓姘偢濮婄粯鎷呴崨濠傛殘缂備浇顕ч崐濠氬焵椤掍礁鍤柛妯圭矙瀹曟碍绻濋崘顏堟闂佸憡绋戦敃锕傚储閸楃儐娓婚柕鍫濋楠炴鏌涢妸銉т粵闁哄懎鐖奸獮姗€顢欓悾灞藉箞闂備礁鐤囬～澶愬磿閾忣偆顩?
func shouldFallbackBindStreamState(err error) bool {
	if err == nil {
		return false
	}
	var rpcErr *gatewayclient.GatewayRPCError
	if errors.As(err, &rpcErr) {
		if rpcErr != nil && rpcErr.Code == protocol.JSONRPCCodeInvalidParams {
			return true
		}
	}
	return strings.Contains(strings.ToLower(strings.TrimSpace(err.Error())), "invalid params")
}

// validateBindStreamAckFrame 缂傚倸鍊搁崐鎼佸磹閹间礁纾归柟闂寸绾惧綊鏌熼梻瀵割槮缁炬儳缍婇弻锝夊箣閿濆憛鎾绘煕閵堝懎顏柡灞剧洴椤㈡洟鏁愰崱娆樻К缂傚倷鐒﹂崝鏍€冮崼銉ョ劦妞ゆ巻鍋撶紒鐘茬Ч瀹曟洟宕￠悘缁樻そ婵℃悂鍩℃担渚敤婵犳鍠楅…鍫ュ春閺嶎厽鍋傛繛鍡樺灩绾捐棄霉閿濆娑ч柣蹇婃櫊閺岋紕鈧綆鍋嗗ú鎾煛鐏炲墽娲存鐐叉喘婵℃悂濡烽妶鍜佹闂傚倸鍊搁崐鎼佸磹瀹勬噴褰掑炊瑜夐弸鏍煛閸ャ儱鐏╃紒鎰殜閺岀喖骞嗚閹界娀鏌涘▎蹇曠闁逞屽墰閹虫挾鈧凹鍘界粩鐔煎幢濞戞ɑ杈?bind_stream 闂傚倸鍊搁崐鎼佸磹閹间礁纾归柣鎴ｅГ閸婂潡鏌ㄩ弴鐐测偓鍝ョ不閺嶎厽鐓曟い鎰剁稻缁€鈧紒鐐劤濞硷繝寮婚悢鐓庣畾闁绘鐗滃Λ鍕磼閹冣挃缂侇噮鍨抽幑銏犫槈閵忕姷顓洪梺缁樺姌鐏忔瑩宕濇导瀛樷拺缂佸顑欓崕鎰版煙閻熺増鎼愰柣锝囨焿閵囨劙骞掑┑鍥ㄦ珖婵＄偑鍊栫敮鎺楀疮閻樿纾婚柟鎹愵嚙闁裤倖淇婇妶鍕厡闁告ê鎲＄换娑欐綇閸撗冨煂闂佺濮ょ划宀勬偤椤撱垺鈷掑ù锝夘棑娑撹尙绱掗幓鎺戔挃闁瑰箍鍨藉畷濂稿Ψ閵夛附袣闂備線鈧偛鑻晶顖炴煏閸パ冾伃妤犵偞甯￠獮瀣攽閹邦亝鍋呴梻鍌欐祰椤曆呮崲濡も偓閳诲秹寮撮悢琛℃敵婵犵數濮村ù鍌炲极瀹ュ棛绡€闂傚牊绋掗ˉ婊堟煙椤栨粌浠辨慨濠傛惈閻ｇ兘宕堕妸銉︾暚缂傚倷绀侀ˇ閬嶅极閹间礁绠查柕蹇曞Л閺€浠嬫倵閿濆骸浜芥俊顐㈠暙閳规垿鎮欓弶鎴犱淮闂佸摜鍠愮€笛囧箞閵娿儮妲堟俊顖炴敱閺傗偓闂備胶绮崝妯间焊濞嗘劖娅犻柡鍥ュ灪閻撶喖鏌″畵顔荤娴狀噣鎮楀▓鍨灈妞ゎ厾鍏樺顐﹀箛椤撶偟绐炲┑鐐村灦濮樸劑鍩ｉ妶鍡曠箚闁绘劦浜滈埀顒佺墪椤斿繑绻濆顒傦紱闂佸湱鍋撻弸濂稿绩娴犲鐓冮柍杞扮閺嗘瑦銇勯弴鐔虹煉闁哄本娲熷畷鎯邦槻妞ゅ浚鍓涚槐鎺楁偐瀹曞洦鍒涢梺杞扮劍閸旀瑥鐣烽妸鈺婃晣婵炴垶鐟ラ鈺呮⒒娴ｇ瓔鍤欐慨姗堢畵閿濈偞寰勬繛鎺撴そ閸╋繝宕橀鍡╂Ц濠电偞鎸婚崺鍐磻閹剧粯鐓忛柛鈩冩礈缁愭梻鈧鍣崳锝呯暦閻撳簶鏀介柛鈩冪懅瀹曞搫鈹戦敍鍕杭闁稿﹥鐗犻獮鎰偅閸愩劎锛熼梺褰掑亰閸庣敻鏁愰崱娆戠槇濠殿喗锕╅崢鎼佸箯缂佹绠鹃弶鍫濆⒔閸掔増銇勯锝嗙闁糕斁鍋撳銈嗗灱濡嫭绂嶆ィ鍐┾拻濞达絼璀﹂悞鐐亜閹存繃顥㈡鐐村灴瀹曞爼顢楅埀顒勬嫅閻斿吋鐓忛柛顐ｇ箖瀹告繈鏌ｉ鐔烘噰闁诡喖缍婇獮渚€骞掗幋婵愮€辩紓鍌欑贰閸ｏ絽顭囪閳ユ棃宕橀鍢壯囧箹缁厜鍋撻懠顒€鍤梻鍌欒兌缁垶銆冮崱娆忓灊闁规崘顕х粻姘舵煃?ACK闂?
func validateBindStreamAckFrame(ack gateway.MessageFrame) error {
	if ack.Type == gateway.FrameTypeError && ack.Error != nil {
		return fmt.Errorf(
			"gateway bind_stream failed (%s): %s",
			strings.TrimSpace(ack.Error.Code),
			strings.TrimSpace(ack.Error.Message),
		)
	}
	if ack.Type != gateway.FrameTypeAck {
		return fmt.Errorf("unexpected gateway frame type for bind_stream: %s", ack.Type)
	}
	return nil
}

// demuxGatewayNotifications 闂傚倸鍊搁崐鎼佸磹閹间礁纾圭€瑰嫭鍣磋ぐ鎺戠倞妞ゆ帒顦伴弲顏堟偡濠婂啰绠绘鐐村灴婵偓闁靛牆鎳愰悿鈧俊鐐€栧Λ浣肝涢崟顒佸劅濠电姴娲﹂埛鎴犳喐閻楀牆绗掑ù婊€鍗抽弻娑欐償閵婏附閿梺瀹犳椤︻垵鐏掗梻浣哥仢椤戞劕顭囬弮鈧换婵嗏枔閸喗鐏嶅銈庡幖閻楀﹦绮嬪澶樻晢闁告洖锕ゅú顓€佸☉妯锋婵浜Σ鍥⒒娓氣偓濞佳勵殽韫囨洖绶ゅù鐘差儐閸嬪倿鏌熼幍顔碱暭闁绘挻娲熼弻锟犲礃閿濆懍澹曟繝鐢靛仜瀵墎鍒掓惔銊ョ闁圭儤顨呴柋鍥煏婢跺牆鍔ら柨娑欑矒濮婅櫣绱掑Ο鍝勵潓濡炪倖娲﹂崣鍐ㄧ暦閵忋倖鍋ㄩ柛娑樑堥幏娲煟閻樺弶绀岄柍褜鍓欑壕顓熺缁嬪簱鏀介柣姗嗗枛閻忊晝绱掔紒姗堣€块柣娑卞櫍楠炴帡骞婇搹顐ｎ棃闁糕斁鍋撳銈嗗笒閸婃悂藟濮橆兘鏀介柣妯虹－椤ｆ煡鏌ｉ幘瀵告噰闁哄矉缍侀獮鍥濞戞﹩娼诲┑鐘媰鐏炵晫浠紓浣虹帛缁嬫挻绂嶉幖浣稿唨闁靛濡囪ぐ瀣煟鎼淬埄鍟忛柛鐘崇墵閹儵鎮℃惔锝嗘闂佺粯鎸哥€垫帒顭囬埡鍌樹簻闁规崘娉涢弸鏃傛喐閺夋寧鍤囬柡宀嬬稻閹棃濮€閳轰焦娅涢梻浣告憸婵敻骞戦崶褏鏆︽繝闈涱儏缁狅絾绻濋崹顐㈠缂傚秴楠搁埞鎴︽倷閸欏鏋欐繛瀛樼矋缁诲牆鐣烽幋锔芥櫢闁绘ê纾崢鍛婄節閵忥絾纭炬い鎴濇嚇椤㈡艾顭ㄩ崟顓ф锤濠电偞鍨堕悷褏寮ч埀顒傜磼閸撗冾暭閽冭鲸銇勯顫含闁哄本绋撻埀顒婄秵娴滄繈宕甸崶顒佺厪闁糕剝顨呴弳鐐电磼缂佹绠炵€规洘甯掗…銊╁礋椤掑鏅梻鍌欐祰瀹曠敻宕伴幇鐗堝仭闁靛／鈧崑鎾愁潩閻撳骸绫嶉悗瑙勬礃濞茬喖骞冮姀銈呯闁兼祴鏅涚敮楣冩⒒婵犲骸浜滄繛灞傚€濋弫鍐Ψ閳轰線妫锋繛瀵稿帶閻°劑鎮?runtime event 婵犵數濮烽弫鍛婃叏閻戣棄鏋侀柛娑橈攻閸欏繘鏌ｉ幋锝嗩棄闁哄绶氶弻鐔兼⒒鐎靛壊妲紒鐐劤椤兘寮婚敐澶婄睄闁稿本鑹炬俊娲倵鐟欏嫭绀€鐎殿喖澧庨幑銏犫攽鐎ｎ偒妫冨┑鐐村灦閻熻京妲愰悙娴嬫斀闁绘劕鐡ㄧ亸顓熴亜椤撶姴鍘寸€殿喖顭烽幃銏ゅ礂閻撳簶鍋撶紒妯圭箚妞ゆ牗绮岄崝锕傛煙閻ゎ垰鍚圭紒?control 闂傚倸鍊搁崐鎼佸磹閹间礁纾归柟闂寸绾惧綊鏌熼梻瀵割槮缁惧墽鎳撻—鍐偓锝庝簻椤掋垺銇勯幇顏嗙煓闁哄被鍔戦幃銏ゅ传閸曟垯鍨婚惀顏堝箚瑜滈悡濂告煛鐏炲墽鈽夐柍钘夘樀瀹曪繝鎮欓幓鎺濆妧濠电姷鏁搁崑娑㈡儍閻戣棄鐤鹃柣妯款嚙閽冪喖鏌￠崶鈺佹灁缂佺娀绠栭弻锝夊箛闂堟稑顫梺缁樼箖濞茬喎顫忛搹鍦煓婵炲棙鍎抽崜浼存⒑缁嬪尅宸ユ繛灏栤偓鎰佸殨閻犲洤妯婇崥瀣熆鐠哄ソ锟犲Ω閳哄倻鍘繝鐢靛Т濞撮攱绂掗姀锛勭瘈濠电姴鍊绘晶娑㈡煕鐎Ｑ冨⒉缂佺粯鐩畷鍗炍旈崘顏嶅敹婵＄偑鍊曞ù姘濠婂牆鐓橀柟杈惧瘜閺佸秵鎱ㄥΟ鐓庡付婵炲牆鐖煎鍝勑ч崶褍濮舵繛瀛樼矤娴滎亪鐛崘顓ф▌閻庤娲樺浠嬨€侀弴銏╂晝闁挎稑瀚崵顒傜磽?
func demuxGatewayNotifications(
	ctx context.Context,
	source <-chan gatewayclient.Notification,
	eventSink chan<- gatewayclient.Notification,
	controlSink chan<- gatewayclient.Notification,
) {
	if source == nil {
		return
	}
	for {
		select {
		case <-ctx.Done():
			return
		case notification, ok := <-source:
			if !ok {
				return
			}
			switch strings.TrimSpace(notification.Method) {
			case protocol.MethodGatewayEvent:
				if !forwardGatewayNotification(ctx, eventSink, notification) {
					return
				}
			case protocol.MethodGatewayNotification:
				if !forwardGatewayNotification(ctx, controlSink, notification) {
					return
				}
			}
		}
	}
}

func forwardGatewayNotification(
	ctx context.Context,
	target chan<- gatewayclient.Notification,
	notification gatewayclient.Notification,
) bool {
	if target == nil {
		return true
	}
	select {
	case <-ctx.Done():
		return false
	case target <- notification:
		return true
	}
}

// consumeGatewayControlNotifications 婵犵數濮烽弫鍛婃叏閻戣棄鏋侀柛娑橈攻閸欏繘鏌ｉ幋锝嗩棄闁哄绶氶弻鐔兼⒒鐎靛壊妲紒鐐劤椤兘寮婚敐澶婄疀妞ゆ帊鐒﹂崕鎾绘⒑閹肩偛濡奸柛濠傛健瀵鏁愭径濠庢綂闂佺粯锚閸熷潡寮抽崼銉︹拺缂佸顑欓崕蹇斻亜閹存繍妯€鐎殿喖顭烽幃銏ゅ礂閻撳簼缃曢梻浣稿閸嬪棝宕伴幘璇茬闁跨喓濮甸埛?shell 闂傚倸鍊搁崐鎼佸磹閹间礁纾圭€瑰嫭鍣磋ぐ鎺戠倞鐟滃繘寮抽敃鍌涚厽闁靛繈鍩勯悞鍓х磼閹邦収娈滈柡宀€鍠栭獮宥夘敊绾拌鲸姣夐梻浣侯焾椤戞垹鎹㈠┑瀣摕闁靛ň鏅涚猾宥夋煕鐏炲墽鐓瑙勬礋濮婃椽宕崟顒夋！缂備緡鍠楅幑鍥ь嚕婵犳碍鏅插璺猴攻椤ユ繈姊洪崷顓х劸閻庢稈鏅犲畷浼村箛閻楀牃鎷虹紓鍌欑劍椤洨绮诲顓犵濠㈣泛顑囧ú鎾煕閳哄啫浠辨鐐差儔閺佸倿鎸婃径澶嬬潖闂傚倷绀侀幉锟犳偡閵夆敡鍥ㄥ閺夋垹鐣哄┑鐐叉閸ㄥ湱澹曢挊澹濆綊鏁愰崨顔藉創閻庢稒绻勭槐鎺楀礈瑜戝鎼佹煕濞嗗繐鏆ｉ柣娑卞枟瀵板嫬鐣濋埀顒勬儗濞嗘劗绠鹃柛鈩兠崝銈夋煙椤旇棄鐏︾紒缁樼箞婵偓闁挎繂妫涢妴鎰版⒑閹稿孩纾搁柛搴″船瀹撳嫬顪冮妶鍡楀潑闁稿鎹囬弻宥囨喆閸曨偆浠稿Δ鐘靛仜閿曨亪寮诲☉娆戠瘈闁告劏鏅濋ˇ鏉课旈悩闈涗粶闁挎洏鍔庣划璇测槈濡攱顫嶅┑鐐叉缁诲棝宕戦幘鑸靛磯闁靛ě鍜冪闯濠电偠鎻紞鈧柛瀣€块獮瀣晝閳ь剟鎯屽Δ鍛彄闁搞儯鍔嶉悡锝囩磼鐠囧弶顥為柕鍥у瀵粙濡搁妷锔藉劎濠电偛顕慨鐢垫暜閿熺姴钃熸繛鎴烇供濞尖晠鏌ｉ幘宕囧嚬闁哥姴锕娲传閸曨剚鎷辩紓浣割儐閸ㄥ潡宕洪姀鈩冨劅闁挎繂瀚崟鍐⒑閻熸壆鎽犻悽顖涘浮钘熸慨姗嗗厴閺€浠嬫煟濡灝鐨烘俊顐㈠閺屸剝寰勬繝鍕殤闂佺顑嗛幐姝岀亙婵炶揪绲块幊鎾愁嚕閸ф鈷戠紓浣姑悘杈ㄤ繆椤愩垹顏瑙勬礃缁绘繂顫濋鐘插箺闂備礁缍婇崑濠囧礈濮樿泛绠氶柛顐犲灮濡垶鏌熼鍡楀娴犳挳姊洪崫鍕潶闁稿﹥鐩俊鐢稿箛閺夎法顔婇梺鐟扮摠缁矂寮搁崼銉︹拻濞达絽鎲￠幆鍫熴亜閿旇姤绶叉い顏勫暣閹稿﹥寰勫Ο鑽ょ▉缂傚倸鍊烽悞锕佹懌婵犳鍨遍幐鎶藉蓟濞戞ǚ妲堟慨妤€鐗嗘导鎰版⒑閸濆嫭顥滈柣妤佹尭椤繐煤椤忓懎娈ラ梺闈涚墕閹冲繘鎮甸姀銏㈢＝濞达絽鎼牎闂佺懓鎲￠幃鍌炲箖妤ｅ啯鍊婚柦妯侯槹瀹撳秴顪冮妶鍡樺暗闁哥姴娴锋竟鏇㈩敍閻愮补鎷虹紓鍌欑劍钃遍柣銊﹀灦閵囧嫰濡烽敃鈧慨宥夋煕閳规儳浜炬俊鐐€栧濠氬磻閹剧粯鐓熼煫鍥ㄦ⒒缁犵偞绻濋埀顒佺瑹閳ь剙顫忛搹鍦煓閻犳亽鍔庨鍥⒑閸涘﹨澹樻い鎴濇川濡叉劙鎮欓崫鍕吅闂佹寧姊婚弲顐﹀储閻㈠憡鈷戦柤濮愬€曢弸鎴濐熆閻熺増顥㈢€规洘妞介弫鎰緞鐎ｎ剙骞愰梺璇茬箳閸嬬喖鈥﹂崼鐔告珷闁肩⒈鍏橀崑鎾诲垂椤愶絿鍑￠柣搴㈠嚬閸橀箖骞戦姀鐘斀閻庯綆浜為ˇ鏉款渻閵堝懐绠版繛璇х畵椤㈡挸螖閸涱喒鎷洪梺鐓庮潟閸婃洘绂掗敂鑺ュ弿婵☆垳顭堟慨鍌溾偓瑙勬磻閸楁娊鐛崶顒夋晩闁芥ê顦花銉︾節閻㈤潧浠﹂柛銊ョ埣閹兘鏁傞崜褏顦梺鎼炲労閸撴岸鎮￠弴銏＄厵闁绘劦鍓﹀▓鏃堟煙椤栨稓鎳囨鐐差儔閺佸倻鎲撮敐鍡楊伖闂傚倷鑳堕、濠囧磻閹版澘纾绘繛鎴欏灩閻掑灚銇勯幒宥囧妽缂佲偓鐎ｎ喗鐓涢悘鐐存灮闊剟鏌℃担鐟板鐎垫澘瀚换婵嬪礃閳诡剚宀稿?
func consumeGatewayControlNotifications(
	ctx context.Context,
	notifications <-chan gatewayclient.Notification,
	shellSessionID string,
	autoState *autoRuntimeState,
	diagnoseJobCh chan<- diagnoseJob,
	idm *idmController,
	output io.Writer,
	errWriter io.Writer,
	publishShellState func(bool),
) {
	targetSessionID := strings.TrimSpace(shellSessionID)
	if targetSessionID == "" {
		return
	}
	for {
		select {
		case <-ctx.Done():
			return
		case notification, ok := <-notifications:
			if !ok {
				return
			}
			payload, ok := decodeGatewayNotificationPayload(notification.Params)
			if !ok {
				continue
			}
			sessionID := strings.TrimSpace(readMapString(payload, "session_id"))
			if sessionID != "" && !strings.EqualFold(sessionID, targetSessionID) {
				continue
			}
			action := strings.ToLower(strings.TrimSpace(readMapString(payload, "action")))
			switch action {
			case protocol.TriggerActionDiagnose:
				select {
				case <-ctx.Done():
					return
				case diagnoseJobCh <- diagnoseJob{IsAuto: false}:
				}
			case protocol.TriggerActionIDMEnter:
				if idm == nil {
					continue
				}
				if err := idm.Enter(); err != nil && errWriter != nil {
					writeProxyf(errWriter, "neocode shell: idm enter rejected: %v\n", err)
				}
			case protocol.TriggerActionAutoOn:
				if autoState != nil {
					autoState.Enabled.Store(true)
				}
				if publishShellState != nil {
					publishShellState(true)
				}
				writeProxyLine(output, "[ auto diagnosis enabled ]")
			case protocol.TriggerActionAutoOff:
				if autoState != nil {
					autoState.Enabled.Store(false)
				}
				if publishShellState != nil {
					publishShellState(false)
				}
				writeProxyLine(output, "[ auto diagnosis disabled ]")
			}
		}
	}
}

func decodeGatewayNotificationPayload(raw json.RawMessage) (map[string]any, bool) {
	if len(raw) == 0 {
		return nil, false
	}
	decoded := map[string]any{}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, false
	}
	return decoded, true
}

func readMapString(container map[string]any, key string) string {
	if container == nil {
		return ""
	}
	value, exists := container[strings.TrimSpace(key)]
	if !exists || value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

// prepareBashInitRC 闂傚倸鍊搁崐鎼佸磹瀹勬噴褰掑炊瑜忛弳锕傛煕椤垵浜濋柛娆忕箻閺岋綁濮€閵忊晝鍔哥紓浣插亾濠㈣泛顑勭换鍡涙煏閸繃鍣洪柛锝呮贡缁辨帡骞囬褎鐤侀梺鍝勭焿缁绘繂鐣烽崼鏇炍ㄩ柕澹倻妫梻?Bash/Zsh 闂傚倸鍊搁崐鎼佸磹閹间礁纾瑰瀣捣閻棗銆掑锝呬壕濡ょ姷鍋涢ˇ鐢稿极瀹ュ绀嬫い鎺嶇劍椤斿洦绻濆閿嬫緲閳ь剚娲熷畷顖烆敍濮樿鲸娈鹃梺鍝勮閸庢煡鎮￠弴鐘亾閸忓浜鹃梺閫炲苯澧寸€规洘娲熼獮瀣偐閻㈡妲搁梻浣告惈缁嬩線宕㈡禒瀣亗婵炲棙鎸婚悡鐔镐繆閵堝倸浜鹃梺缁橆殔閿曪箑鈻庨姀銈嗗殤妞ゆ帒鍊婚敍婊堟⒑闁偛鑻晶顕€鏌ｉ敐鍡欑疄鐎规洜鍠栭、妤呭磼濮橆剛顔囬梻浣筋嚙妤犲摜绮诲澶婄？閺夊牜鐓堝▓浠嬫煕濞戞﹫鏀绘繛鍛У閵囧嫰寮崒妤佸珱闂佺粯鏌ㄥΛ婵嬪箖瀹勬壋鏋庨煫鍥ㄦ惄娴尖偓濠电姭鎷冮崨顔芥瘓闂佸搫澶囬埀顒€纾弳鍡涙倵閿濆骸澧伴柣锕€鐗撻幃妤冩喆閸曨剛顦ラ梺闈涚墛閹倿濡存笟鈧鎾閻欌偓濞煎﹪姊虹紒妯兼喛闁稿鎸荤换娑㈡⒒鐎靛摜鐓撳┑顔硷功缁垶骞忛崨鏉戝窛濠电姴鍟崜鍨繆閻愵亜鈧牠宕归悽绋跨疇婵☆垵娅ｉ弳锔姐亜閺嶎偄鍓遍柡浣哥У缁绘繃绻濋崒娑樻闂佺粯甯掑鈥愁潖濞差亜鎹舵い鎾跺仜婵″搫顪冮妶鍐ㄥ闁硅櫕鍔楅崚鎺楀醇閵夈儵鍞跺┑鐘灪缁诲倻绱炴繝鍥ф瀬闁圭増婢樺婵囥亜閺冨洦纭舵い銏犳嚇濮婄粯鎷呴崨濠冨創濠碘槅鍋呯粙鎺旀崲濞戙垹鐒垫い鎺嶇劍閸欏繐鈹戦悩鎻掓殲闁靛洦绻勯埀顒冾潐濞插繘宕濆鍥ㄥ床婵犻潧顑呯壕鍏肩節婵犲倹鍣洪柛銈呭暟缁辨捇宕掑顑藉亾妞嬪海鐭嗗〒姘ｅ亾妞ゃ垺鐗犲畷鍗炩槈濡櫣鈧參姊洪棃鈺佺槣闁告ü绮欓敐鐐哄即閵忥紕鍘甸梺纭咁潐閸旓箓宕靛▎鎴犵＜闁绘ê妯婇悡濂告煙椤旂瓔娈滈柟顔荤矙閹粙鎮欓崗澶婁壕濠电姴娲﹂悡娑㈡倵閿濆骸浜炲褏鏁婚弻鈥崇暆閳ь剟宕伴弽褏鏆︽俊銈呮噺閸ゅ啴鏌嶉崫鍕偓褰掝敊閸愵喗鈷掑ù锝堫潐閸嬬娀鏌涢弬璺ㄧ劯闁炽儻绠撻幃婊勬叏閹般劌浜惧〒姘ｅ亾鐎殿噮鍣ｅ畷鐓庘攽閸偄鏅ｆ繝鐢靛仩閹活亞寰婇挊澶涜€块梺顒€绉撮崒銊ッ归悩宸剱闁抽攱鍨圭槐鎾存媴婵埈浜濋幈銊╁炊閳规儳浜炬繛鍫濈仢閺嬫盯鏌ｉ弽顒€顥嶆俊顐犲灩閳规垿鎮╃拠褍浼愰柣搴㈠嚬閸犳氨鍒掔紒妯稿亝闁告劏鏅濋崢鍗炩攽閻愬弶顥滄繛瀛樿壘鍗遍柛婵嗗閻斿棛鎲搁弮鈧粋宥夊醇閺囩偠鎽曢梺缁樻煥閹芥粓姊介崟顐唵閻犺櫣灏ㄥ妤呮煕閻樿韬柟顔筋殘閹叉挳宕熼鍌ゆО闂備焦瀵уú蹇涘垂閽樺鍤曟い鎰剁畱绾惧ジ鏌ｉ幇顓熺稇濞寸媭鍘芥穱濠囶敃閿旂粯娈ョ紓浣插亾濞撴埃鍋撻挊?
var (
	createBashInitRCFile = defaultCreateBashInitRCFile
	deleteBashInitRCFile = defaultDeleteBashInitRCFile
	createZshInitDir     = defaultCreateZshInitDir
	deleteZshInitDir     = defaultDeleteZshInitDir
)

func prepareBashInitRC(shellPath string) string {
	if !isBashShell(shellPath) {
		return ""
	}
	path, err := createBashInitRCFile()
	if err != nil {
		return ""
	}
	return path
}

// prepareZshInitDir 闂傚倸鍊搁崐鎼佸磹閹间礁纾圭€瑰嫭鍣磋ぐ鎺戠倞鐟滃繘寮抽敃鍌涚厱妞ゎ厽鍨垫禍婵嬫煕濞嗗繒绠婚柡宀嬬秮婵偓闁靛繆鏅濋崝鍝ョ磽娴ｆ彃浜炬繝銏ｆ硾椤戝嫮鎹㈤崱娑欑厪闁割偅绻冮崳娲煕閿濆懏璐＄紒杈ㄥ浮楠炲洭顢樿閻や線姊洪崫鍕効缂佺粯绻傞悾鐑藉醇閺囩倣銊╂煏婢诡垰鍊诲Λ顖炴⒒?prepareZshInitDir 闂傚倸鍊搁崐鎼佸磹閹间礁纾归柟闂寸绾惧綊鏌ｉ幋锝呅撻柛銈呭閺屾盯顢曢敐鍡欘槬缂佺偓鍎冲锟犲蓟閿濆顫呴柕蹇婃櫇閸旀悂姊哄Ч鍥р偓妤呭磻閹捐埖宕叉繝闈涱儐椤ュ牊绻涢幋婵嗚埞闁告搩鍓熷娲焻閻愯尪瀚板褍澧界槐鎺楁偐閾忣偄纰嶉梺浼欑秮閺€杈╃紦閻ｅ瞼鐭欓悹鍥﹀嫎閸旀垿寮婚弴鐔虹彾妞ゆ牗纰嶉崳瑙勵殽閻愨晛浜鹃梻鍌氬€峰ù鍥х暦閻㈢绐楅柟閭﹀枛閸ㄦ繄鈧箍鍎遍幉妯衡槈閵忕姷顦ㄥ銈庡幗閸ㄩ潧鈻撻妸锔剧瘈闁汇垽娼ф牎闂佽壈顫夐崕鎶筋敊韫囨挴鏀介悗锝庡亞閸樺憡绻濋姀锝嗙【妞ゆ垵鎳橀幃姗€濡疯閸嬫挸鈻撻崹顔界亾闂佺绻戦敋閾荤偤鏌涘☉娆愬剹闁轰礁鍟撮弻鏇＄疀婵炴儳浜鹃柟棰佺劍琚╂繝鐢靛Х閺佹悂宕戝☉姗嗗殨闁割偅娲橀崑瀣節婵犲倹鍣归柛銊︾箞閺屽秹宕崟顐熷亾婵犳碍鈷旈柛鏇ㄥ灡閻撴洘绻涢幋鐐╂闁割偁鍎辩粻?
func prepareZshInitDir(shellPath string) string {
	if !isZshShell(shellPath) {
		return ""
	}
	path, err := createZshInitDir()
	if err != nil {
		return ""
	}
	return path
}

func isBashShell(shellPath string) bool {
	base := strings.ToLower(filepath.Base(shellPath))
	return base == "bash"
}

// isZshShell 闂傚倸鍊搁崐鎼佸磹閹间礁纾圭€瑰嫭鍣磋ぐ鎺戠倞鐟滃繘寮抽敃鍌涚厱妞ゎ厽鍨垫禍婵嬫煕濞嗗繒绠婚柡宀嬬秮婵偓闁靛繆鏅濋崝鍝ョ磽娴ｆ彃浜炬繝銏ｆ硾椤戝嫮鎹㈤崱娑欑厪闁割偅绻冮崳娲煕閿濆懏璐＄紒杈ㄥ浮楠炲洭顢樿閻や線姊洪崫鍕効缂佺粯绻傞悾鐑藉醇閺囩倣銊╂煏婢诡垰鍊诲Λ顖炴⒒?isZshShell 闂傚倸鍊搁崐鎼佸磹閹间礁纾归柟闂寸绾惧綊鏌ｉ幋锝呅撻柛銈呭閺屾盯顢曢敐鍡欘槬缂佺偓鍎冲锟犲蓟閿濆顫呴柕蹇婃櫇閸旀悂姊哄Ч鍥р偓妤呭磻閹捐埖宕叉繝闈涱儐椤ュ牊绻涢幋婵嗚埞闁告搩鍓熷娲焻閻愯尪瀚板褍澧界槐鎺楁偐閾忣偄纰嶉梺浼欑秮閺€杈╃紦閻ｅ瞼鐭欓悹鍥﹀嫎閸旀垿寮婚弴鐔虹彾妞ゆ牗纰嶉崳瑙勵殽閻愨晛浜鹃梻鍌氬€峰ù鍥х暦閻㈢绐楅柟閭﹀枛閸ㄦ繄鈧箍鍎遍幉妯衡槈閵忕姷顦ㄥ銈庡幗閸ㄩ潧鈻撻妸锔剧瘈闁汇垽娼ф牎闂佽壈顫夐崕鎶筋敊韫囨挴鏀介悗锝庡亞閸樺憡绻濋姀锝嗙【妞ゆ垵鎳橀幃姗€濡疯閸嬫挸鈻撻崹顔界亾闂佺绻戦敋閾荤偤鏌涘☉娆愬剹闁轰礁鍟撮弻鏇＄疀婵炴儳浜鹃柟棰佺劍琚╂繝鐢靛Х閺佹悂宕戝☉姗嗗殨闁割偅娲橀崑瀣節婵犲倹鍣归柛銊︾箞閺屽秹宕崟顐熷亾婵犳碍鈷旈柛鏇ㄥ灡閻撴洘绻涢幋鐐╂闁割偁鍎辩粻?
func isZshShell(shellPath string) bool {
	base := strings.ToLower(filepath.Base(shellPath))
	return base == "zsh"
}

func defaultCreateBashInitRCFile() (string, error) {
	content := `
# Load user's original bashrc to preserve custom prompt, aliases, etc.
if [ -f ~/.bashrc ]; then
	. ~/.bashrc
fi
` + shellInitScript + "\n"
	tmpFile, err := os.CreateTemp("", "neocode-bash-rc-*.sh")
	if err != nil {
		return "", fmt.Errorf("ptyproxy: create bash rc file: %w", err)
	}
	if _, err := tmpFile.WriteString(content); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpFile.Name())
		return "", fmt.Errorf("ptyproxy: write bash rc file: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpFile.Name())
		return "", fmt.Errorf("ptyproxy: close bash rc file: %w", err)
	}
	return tmpFile.Name(), nil
}

func defaultDeleteBashInitRCFile(path string) {
	if path != "" {
		_ = os.Remove(path)
	}
}

// defaultCreateZshInitDir 闂傚倸鍊搁崐鎼佸磹閹间礁纾圭€瑰嫭鍣磋ぐ鎺戠倞鐟滃繘寮抽敃鍌涚厱妞ゎ厽鍨垫禍婵嬫煕濞嗗繒绠婚柡宀嬬秮婵偓闁靛繆鏅濋崝鍝ョ磽娴ｆ彃浜炬繝銏ｆ硾椤戝嫮鎹㈤崱娑欑厪闁割偅绻冮崳娲煕閿濆懏璐＄紒杈ㄥ浮楠炲洭顢樿閻や線姊洪崫鍕効缂佺粯绻傞悾鐑藉醇閺囩倣銊╂煏婢诡垰鍊诲Λ顖炴⒒?defaultCreateZshInitDir 闂傚倸鍊搁崐鎼佸磹閹间礁纾归柟闂寸绾惧綊鏌ｉ幋锝呅撻柛銈呭閺屾盯顢曢敐鍡欘槬缂佺偓鍎冲锟犲蓟閿濆顫呴柕蹇婃櫇閸旀悂姊哄Ч鍥р偓妤呭磻閹捐埖宕叉繝闈涱儐椤ュ牊绻涢幋婵嗚埞闁告搩鍓熷娲焻閻愯尪瀚板褍澧界槐鎺楁偐閾忣偄纰嶉梺浼欑秮閺€杈╃紦閻ｅ瞼鐭欓悹鍥﹀嫎閸旀垿寮婚弴鐔虹彾妞ゆ牗纰嶉崳瑙勵殽閻愨晛浜鹃梻鍌氬€峰ù鍥х暦閻㈢绐楅柟閭﹀枛閸ㄦ繄鈧箍鍎遍幉妯衡槈閵忕姷顦ㄥ銈庡幗閸ㄩ潧鈻撻妸锔剧瘈闁汇垽娼ф牎闂佽壈顫夐崕鎶筋敊韫囨挴鏀介悗锝庡亞閸樺憡绻濋姀锝嗙【妞ゆ垵鎳橀幃姗€濡疯閸嬫挸鈻撻崹顔界亾闂佺绻戦敋閾荤偤鏌涘☉娆愬剹闁轰礁鍟撮弻鏇＄疀婵炴儳浜鹃柟棰佺劍琚╂繝鐢靛Х閺佹悂宕戝☉姗嗗殨闁割偅娲橀崑瀣節婵犲倹鍣归柛銊︾箞閺屽秹宕崟顐熷亾婵犳碍鈷旈柛鏇ㄥ灡閻撴洘绻涢幋鐐╂闁割偁鍎辩粻?
func defaultCreateZshInitDir() (string, error) {
	content := `
# Load user's original zshrc to preserve custom prompt, aliases, etc.
if [ -f "${HOME}/.zshrc" ]; then
	. "${HOME}/.zshrc"
fi
` + shellInitScript + "\n"
	directory, err := os.MkdirTemp("", "neocode-zsh-*")
	if err != nil {
		return "", fmt.Errorf("ptyproxy: create zsh init directory: %w", err)
	}
	rcPath := filepath.Join(directory, ".zshrc")
	if writeErr := os.WriteFile(rcPath, []byte(content), 0o600); writeErr != nil {
		_ = os.RemoveAll(directory)
		return "", fmt.Errorf("ptyproxy: write zsh rc file: %w", writeErr)
	}
	return directory, nil
}

// defaultDeleteZshInitDir 闂傚倸鍊搁崐鎼佸磹閹间礁纾圭€瑰嫭鍣磋ぐ鎺戠倞鐟滃繘寮抽敃鍌涚厱妞ゎ厽鍨垫禍婵嬫煕濞嗗繒绠婚柡宀嬬秮婵偓闁靛繆鏅濋崝鍝ョ磽娴ｆ彃浜炬繝銏ｆ硾椤戝嫮鎹㈤崱娑欑厪闁割偅绻冮崳娲煕閿濆懏璐＄紒杈ㄥ浮楠炲洭顢樿閻や線姊洪崫鍕効缂佺粯绻傞悾鐑藉醇閺囩倣銊╂煏婢诡垰鍊诲Λ顖炴⒒?defaultDeleteZshInitDir 闂傚倸鍊搁崐鎼佸磹閹间礁纾归柟闂寸绾惧綊鏌ｉ幋锝呅撻柛銈呭閺屾盯顢曢敐鍡欘槬缂佺偓鍎冲锟犲蓟閿濆顫呴柕蹇婃櫇閸旀悂姊哄Ч鍥р偓妤呭磻閹捐埖宕叉繝闈涱儐椤ュ牊绻涢幋婵嗚埞闁告搩鍓熷娲焻閻愯尪瀚板褍澧界槐鎺楁偐閾忣偄纰嶉梺浼欑秮閺€杈╃紦閻ｅ瞼鐭欓悹鍥﹀嫎閸旀垿寮婚弴鐔虹彾妞ゆ牗纰嶉崳瑙勵殽閻愨晛浜鹃梻鍌氬€峰ù鍥х暦閻㈢绐楅柟閭﹀枛閸ㄦ繄鈧箍鍎遍幉妯衡槈閵忕姷顦ㄥ銈庡幗閸ㄩ潧鈻撻妸锔剧瘈闁汇垽娼ф牎闂佽壈顫夐崕鎶筋敊韫囨挴鏀介悗锝庡亞閸樺憡绻濋姀锝嗙【妞ゆ垵鎳橀幃姗€濡疯閸嬫挸鈻撻崹顔界亾闂佺绻戦敋閾荤偤鏌涘☉娆愬剹闁轰礁鍟撮弻鏇＄疀婵炴儳浜鹃柟棰佺劍琚╂繝鐢靛Х閺佹悂宕戝☉姗嗗殨闁割偅娲橀崑瀣節婵犲倹鍣归柛銊︾箞閺屽秹宕崟顐熷亾婵犳碍鈷旈柛鏇ㄥ灡閻撴洘绻涢幋鐐╂闁割偁鍎辩粻?
func defaultDeleteZshInitDir(path string) {
	if path != "" {
		_ = os.RemoveAll(path)
	}
}

// resolveShellPath 闂傚倸鍊搁崐鎼佸磹閹间礁纾圭€瑰嫭鍣磋ぐ鎺戠倞鐟滃繘寮抽敃鍌涚厱妞ゎ厽鍨垫禍婵嬫煕濞嗗繒绠婚柡宀嬬秮婵偓闁靛繆鏅濋崝鍝ョ磽娴ｆ彃浜炬繝銏ｆ硾椤戝嫮鎹㈤崱娑欑厪闁割偅绻冮崳娲煕閿濆懏璐＄紒杈ㄥ浮楠炲洭顢樿閻や線姊洪崫鍕効缂佺粯绻傞悾鐑藉醇閺囩倣銊╂煏婢诡垰鍊诲Λ顖炴⒒?resolveShellPath 闂傚倸鍊搁崐鎼佸磹閹间礁纾归柟闂寸绾惧綊鏌ｉ幋锝呅撻柛銈呭閺屾盯顢曢敐鍡欘槬缂佺偓鍎冲锟犲蓟閿濆顫呴柕蹇婃櫇閸旀悂姊哄Ч鍥р偓妤呭磻閹捐埖宕叉繝闈涱儐椤ュ牊绻涢幋婵嗚埞闁告搩鍓熷娲焻閻愯尪瀚板褍澧界槐鎺楁偐閾忣偄纰嶉梺浼欑秮閺€杈╃紦閻ｅ瞼鐭欓悹鍥﹀嫎閸旀垿寮婚弴鐔虹彾妞ゆ牗纰嶉崳瑙勵殽閻愨晛浜鹃梻鍌氬€峰ù鍥х暦閻㈢绐楅柟閭﹀枛閸ㄦ繄鈧箍鍎遍幉妯衡槈閵忕姷顦ㄥ銈庡幗閸ㄩ潧鈻撻妸锔剧瘈闁汇垽娼ф牎闂佽壈顫夐崕鎶筋敊韫囨挴鏀介悗锝庡亞閸樺憡绻濋姀锝嗙【妞ゆ垵鎳橀幃姗€濡疯閸嬫挸鈻撻崹顔界亾闂佺绻戦敋閾荤偤鏌涘☉娆愬剹闁轰礁鍟撮弻鏇＄疀婵炴儳浜鹃柟棰佺劍琚╂繝鐢靛Х閺佹悂宕戝☉姗嗗殨闁割偅娲橀崑瀣節婵犲倹鍣归柛銊︾箞閺屽秹宕崟顐熷亾婵犳碍鈷旈柛鏇ㄥ灡閻撴洘绻涢幋鐐╂闁割偁鍎辩粻?
func resolveShellPath(shellOption string) string {
	if trimmed := strings.TrimSpace(shellOption); trimmed != "" {
		return trimmed
	}
	if envShell := strings.TrimSpace(os.Getenv("SHELL")); envShell != "" {
		return envShell
	}
	return "/bin/bash"
}

// consumeDiagSignals 闂傚倸鍊搁崐鎼佸磹閹间礁纾圭€瑰嫭鍣磋ぐ鎺戠倞鐟滃繘寮抽敃鍌涚厱妞ゎ厽鍨垫禍婵嬫煕濞嗗繒绠婚柡宀嬬秮婵偓闁靛繆鏅濋崝鍝ョ磽娴ｆ彃浜炬繝銏ｆ硾椤戝嫮鎹㈤崱娑欑厪闁割偅绻冮崳娲煕閿濆懏璐＄紒杈ㄥ浮楠炲洭顢樿閻や線姊洪崫鍕効缂佺粯绻傞悾鐑藉醇閺囩倣銊╂煏婢诡垰鍊诲Λ顖炴⒒?consumeDiagSignals 闂傚倸鍊搁崐鎼佸磹閹间礁纾归柟闂寸绾惧綊鏌ｉ幋锝呅撻柛銈呭閺屾盯顢曢敐鍡欘槬缂佺偓鍎冲锟犲蓟閿濆顫呴柕蹇婃櫇閸旀悂姊哄Ч鍥р偓妤呭磻閹捐埖宕叉繝闈涱儐椤ュ牊绻涢幋婵嗚埞闁告搩鍓熷娲焻閻愯尪瀚板褍澧界槐鎺楁偐閾忣偄纰嶉梺浼欑秮閺€杈╃紦閻ｅ瞼鐭欓悹鍥﹀嫎閸旀垿寮婚弴鐔虹彾妞ゆ牗纰嶉崳瑙勵殽閻愨晛浜鹃梻鍌氬€峰ù鍥х暦閻㈢绐楅柟閭﹀枛閸ㄦ繄鈧箍鍎遍幉妯衡槈閵忕姷顦ㄥ銈庡幗閸ㄩ潧鈻撻妸锔剧瘈闁汇垽娼ф牎闂佽壈顫夐崕鎶筋敊韫囨挴鏀介悗锝庡亞閸樺憡绻濋姀锝嗙【妞ゆ垵鎳橀幃姗€濡疯閸嬫挸鈻撻崹顔界亾闂佺绻戦敋閾荤偤鏌涘☉娆愬剹闁轰礁鍟撮弻鏇＄疀婵炴儳浜鹃柟棰佺劍琚╂繝鐢靛Х閺佹悂宕戝☉姗嗗殨闁割偅娲橀崑瀣節婵犲倹鍣归柛銊︾箞閺屽秹宕崟顐熷亾婵犳碍鈷旈柛鏇ㄥ灡閻撴洘绻涢幋鐐╂闁割偁鍎辩粻?
func consumeDiagSignals(
	ctx context.Context,
	rpcClient *gatewayclient.GatewayRPCClient,
	_ <-chan gatewayclient.Notification,
	jobCh <-chan diagnoseJob,
	output io.Writer,
	buffer *UTF8RingBuffer,
	options ManualShellOptions,
	shellSessionID string,
	recentTriggerStore *diagnosisTriggerStore,
	autoState *autoRuntimeState,
	onAutoDiagnoseFailure func(error),
	coordinator *diagnosisCoordinator,
) {
	var autoWG sync.WaitGroup
	autoSlots := make(chan struct{}, 1)
	defer autoWG.Wait()
	for {
		select {
		case <-ctx.Done():
			return
		case job, ok := <-jobCh:
			if !ok {
				return
			}
			if job.IsAuto {
				if coordinator != nil {
					prepared, prepareErr := prepareDiagnoseRequest(buffer, options, shellSessionID, job.Trigger)
					if prepareErr == nil && coordinator.shouldDropAuto(prepared.Fingerprint) {
						continue
					}
				}
				select {
				case autoSlots <- struct{}{}:
				case <-ctx.Done():
					return
				default:
					continue
				}
				autoWG.Add(1)
				go func(autoJob diagnoseJob) {
					defer autoWG.Done()
					defer func() { <-autoSlots }()
					diagnoseErr := runSingleDiagnosisWithCoordinator(
						ctx,
						coordinator,
						rpcClient,
						nil,
						output,
						buffer,
						options,
						shellSessionID,
						autoJob.Trigger,
						true,
						autoState,
					)
					if diagnoseErr != nil && onAutoDiagnoseFailure != nil && shouldTerminateShellOnAutoDiagnoseError(diagnoseErr) {
						onAutoDiagnoseFailure(diagnoseErr)
					}
				}(job)
				continue
			}
			diagnoseErr := runSingleDiagnosisWithCoordinator(
				ctx,
				coordinator,
				rpcClient,
				nil,
				output,
				buffer,
				options,
				shellSessionID,
				resolveManualDiagnoseTrigger(job.Trigger, recentTriggerStore),
				false,
				autoState,
			)
			if diagnoseErr != nil {
				continue
			}
		}
	}
}

// shouldTerminateShellOnAutoDiagnoseError 闂傚倸鍊搁崐鎼佸磹閹间礁纾归柟闂寸绾剧懓顪冪€ｎ亝鎹ｉ柣顓炴閵嗘帒顫濋敐鍛婵°倗濮烽崑娑⑺囬悽绋挎瀬鐎广儱顦粈瀣亜閹哄秶鍔嶆い鏂挎喘濮婄粯鎷呯憴鍕哗闂佺瀛╁钘夌暦濠婂啠鏋庨柟瀛樼箥濡粓鎮峰鍛暭閻㈩垱顨婇幃鈥斥槈濮橈絽浜炬鐐茬仢閸旀艾螖閻樿櫕鍊愰柣娑卞櫍瀵粙顢橀悢鍝勫籍闂備礁鎲￠崝锔界濠婂牆鍑犳繛鎴欏灪閻撴盯鎮橀悙鎻掆挃婵炲弶娼欓埞鎴︽晬閸曨偄骞嬪銈冨灪閻熲晠骞冮埄鍐╁劅妞ゆ棁濮ょ粊浼存⒒閸屾艾鈧兘鎮為敃鍌氱畺闁割偅娲栫壕褰掓煛閸ャ儱鐏柡鍛箞閺屾洘寰勯崱妯荤彅婵℃鎳樺娲川婵犲啫顦╅梺鍝ュУ瀹€鎼佸箖閸ф鐒垫い鎺戝閳锋垿鏌涢敂璇插箹濞存粓绠栭弻娑㈠箻鐎靛壊鏆＄紓浣稿€圭敮妤冪紦娴犲宸濆┑鐘插€烽崫妤佺節濞堝灝鏋熼柨姘扁偓瑙勬处閸撶喎顕ｇ粙搴撴闁靛骏绱曢崢鎼佹倵閸忓浜鹃柣搴秵閸撴稖鈪甸梻鍌欐祰椤曟牠宕伴幒妤€鐤鹃柣妯垮皺閺嗭箓鏌ㄥ┑鍡橆棞缂佸墎鍋ら幃妤呮晲鎼粹€茬盎闂侀€炲苯澧伴柛蹇斆～蹇曠磼濡顎撻梺鑽ゅ枛閸嬪﹪宕电€ｎ亖鏀介柣鎰綑缁茶崵绱掔紒妯忣亪鎮鹃悜钘夊嵆闁靛繆妲呭Λ鍐ㄢ攽閻愭潙鐏ョ€规洦鍓欓埢鎾诲Ω閿斿墽鐦堥梺姹囧灲濞佳勭墡闂備胶鍘х紞濠勭不閺嶎厼绠栨繛鍡樺姉缁♀偓闂佹悶鍎崝搴ㄥ储椤忓懐绡€闁汇垽娼ф禒婊堟煟韫囨梻绠炵€规洘绻堟俊鍫曞炊閳哄喛绱查梻浣哥秺閸嬪﹪宕伴弽銊ь浄闂侇剙绉甸悡娑㈡倶閻愭鐒惧褑灏欓埀顒€鐏氬妯尖偓姘煎幘閹广垹鈹戠€ｎ亞顦板銈嗗姉閸犳劗鈧碍鐩濠氬磼濞嗘帒鍘″銈庡幖閻楁捇銆侀弽顓炲窛婵縿鍊曞ú顓㈢嵁鎼淬劍鐓ｉ柟娈垮枛閸樺鈧娲樼敮锟犲箖濞嗘垵鍨濋悷娆忓椤忕儤绻濋悽闈涗哗闁规椿浜炲濠冪鐎ｎ亞鐤呴梺璺ㄥ枔婵挳鎮块鈧弻锝夊箛椤旂厧濡洪梺缁樻尵婵炩偓闁哄矉缍侀幃銏ゅ川婵犲嫬鍤掗梻浣瑰▕閺€鍗炍涢崘顔艰摕鐎广儱鐗滃銊╂⒑閸涘﹥灏版慨妯稿妿閸掓帒鈻庨幘鎶藉敹闂侀潧顦崕鎶藉蓟瑜嶉埞鎴︽倷閺夋垹浠搁梺鎸庢皑缁辨帗绗熼崶褌鍠婂┑顔硷功缁垶骞忛崨鏉戜紶闁靛鐏濋妸銉㈡斀闁宠棄妫楁禍顖涚箾婢跺绀堥柛鎺撳浮瀹曞ジ鎮㈢粙娆锯偓鎾绘⒑缂佹ê鐏﹂柨姘舵煛鐎ｎ亜鈧灝顫忓ú顏勭閹兼番鍨婚ˇ銉╂⒑缁嬪尅宸ョ紓宥咃躬楠炲啴鏁撻悩鍙傘劑鏌嶉崫鍕偓濠氬储閹间焦鈷戦柟鑲╁仜閸旀挳鏌ｉ幘宕囧闁崇粯鎹囧顒€螞閻㈠灚鍤€妞ゎ厹鍔戝畷姗€濡搁姀鈥崇槺缂傚倸鍊峰ù鍥ㄣ仈閹间礁绠板┑鐘宠壘缁狀垶鏌涘☉娆愮稇濞磋偐濞€閺屾盯寮撮妸銉ょ盎閻炴碍绻堝濠氬磼濞嗘帒鍘″銈庡幖閻楁捇銆侀弽顓炲耿婵炴垶顭囬鍥╃磽閸屾瑧鍔嶆い顓炴喘閸╃偛顓奸崨顏呮杸闂佺粯锚瀵埖寰勯崟顖涚厱濠电姴娲﹂弫閬嶆煏閸パ冾伂缂佺姵鐩獮姗€骞囨担閿嬵潟闂傚倷鑳堕…鍫ヮ敄閸℃稑绠伴柟闂寸閻撯€愁熆鐠哄ソ锟犳偄閻撳海顦ч梺绋跨箳閳峰牓宕惔銊︹拻闁稿本鐟ч崝宥夋煟椤忓嫮绉虹€规洖缍婇幐濠冨緞濡厧濮洪柣鐔哥矋閺屻劑鎮鹃悜鑺ュ仭闁逛絻娅曢弬鈧梺璇插嚱缂嶅棝宕戦崱娑樺偍闁汇垹鎲￠埛鎴犵磽娴ｅ顏呮叏閿曞倹鐓曢柟鎯ь嚟閹冲嫰鏌￠崨顓犲煟妞ゃ垺绋戦埥澶婎潩妲屾牕鏅梻鍌欑閸氬绮婇幘顔肩柧婵炴埈娼块埀顒€鍊圭缓浠嬪川婵犲嫬甯鹃梻浣规偠閸庢粓宕橀崣銉х＞濠碉紕鍋戦崐鎴﹀垂閸濆娊娲偄缁楄　鍋撴笟鈧鎾閻欌偓濞煎﹪姊虹紒妯曟垿顢欓弽顓炵柈闁搞儺鍓氶埛鎺楁煕鐏炲墽鎳勭紒浣哄娣囧﹤顪冮悡搴☆瀷闂佺懓鍢查澶婎嚕婵犳艾唯闁靛／鍕弰闂傚倷鑳剁划顖炩€﹂崼銉晪鐟滄棃骞忛幋锔藉亜闁稿繗鍋愰崣鍡椻攽閻樼粯娑ф俊顐幖鍗辩憸鐗堝笚閻撴洖鈹戦悩鎻掝仼闁告ɑ鎸抽弻锛勪沪閸撗€妲堥梺瀹狀嚙濮橈妇绮诲☉銏℃櫜闁糕€崇箲閻忎礁鈹戦悩娈挎毌婵℃彃鎳樺畷鎴﹀幢濞戞锛涢梺闈涚墕閹峰顭囬弽銊х鐎瑰壊鍠曠花鑽ょ磼閻樺崬宓嗘鐐寸墬濞煎繘宕滆閻繃绻濆▓鍨灈婵炲樊鍘奸～蹇撁洪鍕祶濡炪倖鎸鹃崰搴♀枔鐏炶В鏀介柣鎰邦杺閸ゅ姊婚崟顐㈩伀缂?
func shouldTerminateShellOnAutoDiagnoseError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(strings.TrimSpace(err.Error()))
	if message == "" {
		return false
	}
	if strings.Contains(message, "context deadline exceeded") {
		return false
	}
	if strings.Contains(message, "rate limit") || strings.Contains(message, "rate_limited") {
		return false
	}
	if strings.Contains(message, "provider generate") || strings.Contains(message, "sdk stream error") {
		return false
	}
	if strings.Contains(message, "unauthorized") {
		return true
	}
	if strings.Contains(message, "transport error") ||
		strings.Contains(message, "connection refused") ||
		strings.Contains(message, "no such file") ||
		strings.Contains(message, "use of closed network connection") {
		return true
	}
	return false
}

// runSingleDiagnosis 闂傚倸鍊搁崐鎼佸磹閹间礁纾圭€瑰嫭鍣磋ぐ鎺戠倞鐟滃繘寮抽敃鍌涚厱妞ゎ厽鍨垫禍婵嬫煕濞嗗繒绠婚柡宀嬬秮婵偓闁靛繆鏅濋崝鍝ョ磽娴ｆ彃浜炬繝銏ｆ硾椤戝嫮鎹㈤崱娑欑厪闁割偅绻冮崳娲煕閿濆懏璐＄紒杈ㄥ浮楠炲洭顢樿閻や線姊洪崫鍕効缂佺粯绻傞悾鐑藉醇閺囩倣銊╂煏婢诡垰鍊诲Λ顖炴⒒?runSingleDiagnosis 闂傚倸鍊搁崐鎼佸磹閹间礁纾归柟闂寸绾惧綊鏌ｉ幋锝呅撻柛銈呭閺屾盯顢曢敐鍡欘槬缂佺偓鍎冲锟犲蓟閿濆顫呴柕蹇婃櫇閸旀悂姊哄Ч鍥р偓妤呭磻閹捐埖宕叉繝闈涱儐椤ュ牊绻涢幋婵嗚埞闁告搩鍓熷娲焻閻愯尪瀚板褍澧界槐鎺楁偐閾忣偄纰嶉梺浼欑秮閺€杈╃紦閻ｅ瞼鐭欓悹鍥﹀嫎閸旀垿寮婚弴鐔虹彾妞ゆ牗纰嶉崳瑙勵殽閻愨晛浜鹃梻鍌氬€峰ù鍥х暦閻㈢绐楅柟閭﹀枛閸ㄦ繄鈧箍鍎遍幉妯衡槈閵忕姷顦ㄥ銈庡幗閸ㄩ潧鈻撻妸锔剧瘈闁汇垽娼ф牎闂佽壈顫夐崕鎶筋敊韫囨挴鏀介悗锝庡亞閸樺憡绻濋姀锝嗙【妞ゆ垵鎳橀幃姗€濡疯閸嬫挸鈻撻崹顔界亾闂佺绻戦敋閾荤偤鏌涘☉娆愬剹闁轰礁鍟撮弻鏇＄疀婵炴儳浜鹃柟棰佺劍琚╂繝鐢靛Х閺佹悂宕戝☉姗嗗殨闁割偅娲橀崑瀣節婵犲倹鍣归柛銊︾箞閺屽秹宕崟顐熷亾婵犳碍鈷旈柛鏇ㄥ灡閻撴洘绻涢幋鐐╂闁割偁鍎辩粻?
func runSingleDiagnosis(
	rpcClient *gatewayclient.GatewayRPCClient,
	output io.Writer,
	buffer *UTF8RingBuffer,
	options ManualShellOptions,
	shellSessionID string,
	trigger diagnoseTrigger,
	isAuto bool,
	autoState *autoRuntimeState,
) error {
	return runSingleDiagnosisWithCoordinator(
		context.Background(),
		nil,
		rpcClient,
		nil,
		output,
		buffer,
		options,
		shellSessionID,
		trigger,
		isAuto,
		autoState,
	)
}

// runSingleDiagnosisWithCoordinator 闂傚倸鍊搁崐鎼佸磹閹间礁纾归柟闂寸绾剧懓顪冪€ｎ亝鎹ｉ柣顓炴闇夐柨婵嗩槹娴溿倝鏌ら弶鎸庡仴婵﹥妞介、妤呭焵椤掑倻鐭撻柣銏㈩焾绾惧鏌ㄩ悢鍝勑ｉ柣鎾寸懇閺岀喖顢涘鍐炬毉濡炪們鍎遍ˇ浼淬€冮妷鈺傚€锋い蹇撳娴煎牓姊洪崫鍕缂佸鎳撻悾鐑筋敃閿曗偓缁€瀣亜閹邦喖鏋庡ù婊冨⒔閹叉悂鎮ч崼婵堢懆缂備胶瀚忛崨顖滐紲闁诲函缍嗘禍鐐差潩閵娾晜鐓涢柍褜鍓氱粋鎺斺偓锝庡亞閸樹粙姊鸿ぐ鎺戜喊闁告鏅槐鐐哄箣閿旂晫鍘介棅顐㈡储閸庢娊鎮鹃悽鍛婄厸鐎光偓閳ь剟宕伴弽褏鏆﹂柕濠忓缁♀偓闂佸憡鍔戦崝宀勫绩椤撱垺鐓熼幖娣€ゅ鎰箾閸欏鑰跨€规洖缍婂畷绋课旈崘銊с偊婵犵妲呴崹浼存儍閻戣棄纾婚柟鍓х帛椤ュ牊绻涢幋鐐垫噭闁哥姵鍔曢—鍐Χ閸℃ǚ鎷瑰┑鐐跺皺閸犲酣锝炶箛鎾佹椽顢旈崟顏嗙倞闂備線娼чˇ顓㈠磿濞嗘挸围濠㈣泛顑囬崢鍛婄節閵忥絾纭鹃柨鏇畱椤繈濡搁埡鍌滃幈闁诲函缍嗛崑鍛焊椤撱垺鎳氶柡宥庡幗閻撳啰鎲稿鍫濈婵炲棗绻掗弳銈夋煕閺囥劌浜愰柡鈧懞銉ｄ簻闁哄啫鍊婚幗鍌炴煟閹烘鐣洪柡灞剧洴閸╋繝宕掑鍐ｆ嫲闂備浇顕栭崰鎺楀焵椤掍焦鐏辨俊鎻掔墦閺屾洝绠涢妷褏锛熼梺鍛婏耿娴滃爼寮婚敐鍫㈢杸闁哄洨鍋橀崫妤€顪冮妶鍡樿偁闁搞儜鍛Е婵＄偑鍊栫敮濠囨倿閿斿墽鐭嗛柍褜鍓欓—鍐Χ閸℃鈹涚紓浣瑰絻濞硷繝鐛幋锕€绀嬫い鏍嚤閳哄啯鍠愰煫鍥ㄧ⊕閺咁剚绻濇繝鍌滃闁绘挻绋戦…璺ㄦ崉閻氭潙濮涙繝鈷€鍕伌闁哄本鐩顒勫箰鎼淬垹闂紓鍌欑贰閸犳牠鈥﹂悜钘夌畺婵炲棙鎸婚崐缁樹繆椤栨粌甯舵鐐搭殕缁绘繈鎮介棃娑楁勃闂佹悶鍔戝褔鎮鹃悜绛嬫晢闁逞屽墴閸┿垹顓奸崪浣告倯婵犮垼娉涢鍥储閻㈠憡鈷戦柟顖嗗嫮顩伴梺绋款儏濡繈骞嗗畝鍕闁规崘灏欑粻姘渻閵堝棗濮傞柛銊ョ秺瀵啿鈻庨幘瀵稿帗闂佽姤锚椤﹁棄危閸涘浜滄い鎰剁悼缁犵偞銇勯姀鈭额亪鍩ユ径濞炬瀻闁瑰瓨鏌ㄦ禍鎯р攽閻樺磭顣查柍閿嬪浮閺屾稓浠﹂幑鎰棟闂侀€炲苯澧柟顔煎€搁悾鐑藉箛閺夎法顔愭繛杈剧到閹芥粓鏁嶅▎鎴犵＝濞达絽澹婇崕鎰版煥濞戞瑦绀€閻撱倖銇勮箛鎾村櫣濞存粌缍婇弻锝夋偐閸欏鈹涢柣蹇撶箲閻熲晠寮鍛闁靛繆妾ч幏娲⒑闂堚晛鐦滈柛妯哄悑缁傚秵銈ｉ崘鈺冨幈闂佸湱鍋撳娆撳传濞差亝鐓欐い鏂挎惈閻忚尙鈧娲栧畷顒勫煡婢跺ň鏋庨煫鍥ч鐎靛弶绻濆閿嬫緲閳ь儸鍥ㄢ挃闁告洦鍘搁崑鎾剁箔濞戞ɑ绁╅柡浣稿€块弻锝呂熼懖鈺佺闂佹悶鍊曢崯鎾蓟閳ユ剚鍚嬮幖绮光偓宕囶啈闂備焦濞婇弨閬嶅垂閸噮鍤曢柛顐ｆ礃閸婄兘鏌℃径濠勪虎缂佷緡鍠栭埞鎴﹀灳閸愭祴鎸冮梺绋跨箲閿曘垹顕ｇ拠娴嬫婵炲棙鍨归惁鍫ユ⒑閸涘﹤濮﹀ù婊勵殜閸┾偓妞ゆ帊绀佺粭褏绱掓潏銊ユ诞妞ゃ垺鐟ラ埢搴ㄥ箚瑜庨鍕⒒娴ｅ摜鏋冩俊顐㈠钘濋柛顭戝亝閸欏繘鏌嶈閸撶喖寮诲澶婁紶闁告洦鍋€閸嬫捇鎮烽幍铏€洪梺鎼炲労閸撴岸鍩涢幒鎳ㄥ綊鏁愰崶銊ユ畬濡炪倖娲樼划宥夊箞閵婏妇绡€闁稿被鍊楅崥瀣旈悩闈涗粶闁挎洦浜悰顔嘉熺亸鏍т壕闂傚牊绋戦ˉ蹇旂箾閹绘帗鍟炲ǎ鍥э躬婵℃儼绠涢弴鐐茬厒闂備礁鎽滈崳銉╁垂閸洖绠栭柨鐔哄У閸嬪嫰鏌涜箛姘汗闁告ɑ鎮傚娲川婵犲倸顫囬梺鍛婃煥閻倿宕洪埀顒併亜閹烘垵鈧綊寮抽渚囨闁绘劘灏欑粻濠氭煕閳轰礁顏€规洘锕㈤、鏃堝幢閳轰焦娅楅梻鍌欐祰椤曆呪偓娑掓櫇缁瑩骞掗弴鐔稿櫡缂傚倸鍊风拋鏌ュ磻閹剧粯鍊甸柨婵嗛閺嬬喖鏌ｉ幘瀵告创闁诡喗锕㈤幃娆撳箵閹哄棙瀵栭梻浣哥枃濡嫰藝閻㈢钃熺€广儱娲﹂崰鍡涙煕閺囥劌浜炲ù鐓庣焸閹鎲撮崟顒傤槬閻庤娲﹂崜鐔煎春閵忕媭鍚嬪璺侯儐閸庮亪姊洪懡銈呮瀾濠㈢懓顑夊鎼佸川鐎涙ǚ鎷洪梺鍛婄箓鐎氼垳鈧碍澹嗙槐鎺撳緞鐏炶棄骞嬬紒鈩冩尭閵嗘帒顫濋敐鍛闂備礁鎼張顒傜矙閺嶎偆涓嶆繛鎴欏灩缁秹鏌涚仦鍓р槈闁搞劑浜跺缁樻媴娓氼垱鏁┑鐐叉噺濞茬喖濡撮崘鈺冪瘈闁稿本绮嶅▓鎯р攽閻樿宸ラ悗姘煎幗閸掑﹪骞橀钘変化闂佹悶鍎荤徊鑺ユ櫠閼碱剛纾界€广儱妫▓婊勬叏婵犲嫮甯涢柟宄版嚇閹兘鏌囬敃鈧▓婵囩節绾版ɑ顫婇柛瀣瀹曨垶寮堕幋顓炴濡炪倖鍔х€靛矂寮€ｎ喗鐓冪憸婊堝礈濞嗘挻鍋╅柣鎴ｆ缁狅綁鏌ㄩ弴妤€浜剧紓浣哄Т椤兘寮婚敓鐘茬＜婵☆垰鎼慨鏇烆渻閵堝骸浜滅紒澶嬫尦閸╃偤骞嬮敂钘夆偓鐑芥煕濞嗗浚妲告い搴㈢洴濮婃椽宕ㄦ繝鍕枦闂佺顑嗛幑鍥ь潖閾忓湱鐭欐繛鍡樺劤閸撻亶姊洪崨濠冣拹妞ゃ劌锕ら悾?in-flight 闂傚倸鍊搁崐鎼佸磹閹间礁纾归柟闂寸绾剧懓顪冪€ｎ亝鎹ｉ柣顓炴閵嗘帒顫濋敐鍛婵°倗濮烽崑娑⑺囬悽绋挎瀬闁瑰墽绮崑鎰版煕閹邦剙绾ч柣銈呭閳规垶骞婇柛濞у懎绶ゅù鐘差儏閻ゎ喗銇勯弽顐粶缁炬儳顭烽幃妤呮晲鎼粹剝鐏嶉梺缁樻尰濞茬喖寮诲澶婄厸濞达絽鎲″▓鍫曟⒑閹颁礁鐏℃繛鑼枛瀵鈽夐姀鐘栄囨煕閳╁喚娈旀い顐ｅ浮濮婃椽宕崟顓犲姽缂傚倸绉崇粈渚€顢氶敐澶樻晪闁逞屽墮閻ｇ兘骞掗幋顓熷兊濡炪倖鍨煎▔鏇犳暜濞戙垺鈷?
func runSingleDiagnosisWithCoordinator(
	ctx context.Context,
	coordinator *diagnosisCoordinator,
	rpcClient *gatewayclient.GatewayRPCClient,
	eventStream <-chan gatewayclient.Notification,
	output io.Writer,
	buffer *UTF8RingBuffer,
	options ManualShellOptions,
	shellSessionID string,
	trigger diagnoseTrigger,
	isAuto bool,
	autoState *autoRuntimeState,
) error {
	if output == nil {
		return nil
	}
	if isAuto && autoState != nil && !autoState.Enabled.Load() {
		return nil
	}

	prepared, prepareErr := prepareDiagnoseRequest(buffer, options, shellSessionID, trigger)
	if prepareErr != nil {
		if options.Stderr != nil {
			writeProxyf(options.Stderr, "neocode diag: build diagnose payload failed: %v\n", prepareErr)
		}
		err := errors.New("failed to build diagnosis payload")
		writeProxyf(output, "\n\033[31m[NeoCode Diagnosis]\033[0m %s\n", strings.TrimSpace(err.Error()))
		return err
	}

	if coordinator != nil {
		if cached, ok := coordinator.cached(prepared.Fingerprint); ok {
			if cached.Err != nil {
				writeProxyf(output, "\n\033[31m[NeoCode Diagnosis]\033[0m %s\n", strings.TrimSpace(cached.Err.Error()))
				return cached.Err
			}
			if !isAuto {
				writeProxyLine(output, "\n\033[36m[NeoCode Diagnosis]\033[0m using cached diagnosis result")
			}
			renderDiagnosis(output, cached.Result.Content, cached.Result.IsError)
			return nil
		}
	}

	renderDiagnosisInitialFeedback(output, prepared, isAuto)

	var (
		result tools.ToolResult
		err    error
	)
	execute := func(timeout time.Duration) diagnosisOutcome {
		if coordinator == nil {
			result, callErr := executePreparedDiagnoseToolWithTimeout(rpcClient, nil, options, prepared, timeout)
			return diagnosisOutcome{Result: result, Err: callErr}
		}
		return coordinator.run(ctx, prepared.Fingerprint, func() (tools.ToolResult, error) {
			return executePreparedDiagnoseToolWithTimeout(rpcClient, nil, options, prepared, timeout)
		})
	}
	if isAuto {
		outcome := execute(autoDiagnoseCallTimeout)
		result, err = outcome.Result, outcome.Err
	} else {
		outcome := execute(diagnoseCallTimeout)
		result, err = outcome.Result, outcome.Err
	}
	if err != nil {
		writeProxyf(output, "\n\033[31m[NeoCode Diagnosis]\033[0m %s\n", strings.TrimSpace(err.Error()))
		return err
	}
	if isAuto && autoState != nil && !autoState.Enabled.Load() {
		return nil
	}
	renderDiagnosis(output, result.Content, result.IsError)
	return nil
}

// callDiagnoseTool 闂傚倸鍊搁崐鎼佸磹閹间礁纾圭€瑰嫭鍣磋ぐ鎺戠倞鐟滃繘寮抽敃鍌涚厱妞ゎ厽鍨垫禍婵嬫煕濞嗗繒绠婚柡宀嬬秮婵偓闁靛繆鏅濋崝鍝ョ磽娴ｆ彃浜炬繝銏ｆ硾椤戝嫮鎹㈤崱娑欑厪闁割偅绻冮崳娲煕閿濆懏璐＄紒杈ㄥ浮楠炲洭顢樿閻や線姊洪崫鍕効缂佺粯绻傞悾鐑藉醇閺囩倣銊╂煏婢诡垰鍊诲Λ顖炴⒒?callDiagnoseTool 闂傚倸鍊搁崐鎼佸磹閹间礁纾归柟闂寸绾惧綊鏌ｉ幋锝呅撻柛銈呭閺屾盯顢曢敐鍡欘槬缂佺偓鍎冲锟犲蓟閿濆顫呴柕蹇婃櫇閸旀悂姊哄Ч鍥р偓妤呭磻閹捐埖宕叉繝闈涱儐椤ュ牊绻涢幋婵嗚埞闁告搩鍓熷娲焻閻愯尪瀚板褍澧界槐鎺楁偐閾忣偄纰嶉梺浼欑秮閺€杈╃紦閻ｅ瞼鐭欓悹鍥﹀嫎閸旀垿寮婚弴鐔虹彾妞ゆ牗纰嶉崳瑙勵殽閻愨晛浜鹃梻鍌氬€峰ù鍥х暦閻㈢绐楅柟閭﹀枛閸ㄦ繄鈧箍鍎遍幉妯衡槈閵忕姷顦ㄥ銈庡幗閸ㄩ潧鈻撻妸锔剧瘈闁汇垽娼ф牎闂佽壈顫夐崕鎶筋敊韫囨挴鏀介悗锝庡亞閸樺憡绻濋姀锝嗙【妞ゆ垵鎳橀幃姗€濡疯閸嬫挸鈻撻崹顔界亾闂佺绻戦敋閾荤偤鏌涘☉娆愬剹闁轰礁鍟撮弻鏇＄疀婵炴儳浜鹃柟棰佺劍琚╂繝鐢靛Х閺佹悂宕戝☉姗嗗殨闁割偅娲橀崑瀣節婵犲倹鍣归柛銊︾箞閺屽秹宕崟顐熷亾婵犳碍鈷旈柛鏇ㄥ灡閻撴洘绻涢幋鐐╂闁割偁鍎辩粻?
func callDiagnoseTool(
	rpcClient *gatewayclient.GatewayRPCClient,
	buffer *UTF8RingBuffer,
	options ManualShellOptions,
	shellSessionID string,
	trigger diagnoseTrigger,
) (tools.ToolResult, error) {
	return callDiagnoseToolWithTimeout(rpcClient, buffer, options, shellSessionID, trigger, diagnoseCallTimeout)
}

// callDiagnoseToolWithTimeout 闂傚倸鍊搁崐鎼佸磹閹间礁纾圭€瑰嫭鍣磋ぐ鎺戠倞鐟滃繘寮抽敃鍌涚厱妞ゎ厽鍨垫禍婵嬫煕濞嗗繒绠婚柡宀嬬秮婵偓闁靛繆鏅濋崝鍝ョ磽娴ｆ彃浜炬繝銏ｆ硾椤戝嫮鎹㈤崱娑欑厪闁割偅绻冮崳娲煕閿濆懏璐＄紒杈ㄥ浮楠炲洭顢樿閻や線姊洪崫鍕効缂佺粯绻傞悾鐑藉醇閺囩倣銊╂煏婢诡垰鍊诲Λ顖炴⒒?callDiagnoseToolWithTimeout 闂傚倸鍊搁崐鎼佸磹閹间礁纾归柟闂寸绾惧綊鏌ｉ幋锝呅撻柛銈呭閺屾盯顢曢敐鍡欘槬缂佺偓鍎冲锟犲蓟閿濆顫呴柕蹇婃櫇閸旀悂姊哄Ч鍥р偓妤呭磻閹捐埖宕叉繝闈涱儐椤ュ牊绻涢幋婵嗚埞闁告搩鍓熷娲焻閻愯尪瀚板褍澧界槐鎺楁偐閾忣偄纰嶉梺浼欑秮閺€杈╃紦閻ｅ瞼鐭欓悹鍥﹀嫎閸旀垿寮婚弴鐔虹彾妞ゆ牗纰嶉崳瑙勵殽閻愨晛浜鹃梻鍌氬€峰ù鍥х暦閻㈢绐楅柟閭﹀枛閸ㄦ繄鈧箍鍎遍幉妯衡槈閵忕姷顦ㄥ銈庡幗閸ㄩ潧鈻撻妸锔剧瘈闁汇垽娼ф牎闂佽壈顫夐崕鎶筋敊韫囨挴鏀介悗锝庡亞閸樺憡绻濋姀锝嗙【妞ゆ垵鎳橀幃姗€濡疯閸嬫挸鈻撻崹顔界亾闂佺绻戦敋閾荤偤鏌涘☉娆愬剹闁轰礁鍟撮弻鏇＄疀婵炴儳浜鹃柟棰佺劍琚╂繝鐢靛Х閺佹悂宕戝☉姗嗗殨闁割偅娲橀崑瀣節婵犲倹鍣归柛銊︾箞閺屽秹宕崟顐熷亾婵犳碍鈷旈柛鏇ㄥ灡閻撴洘绻涢幋鐐╂闁割偁鍎辩粻?
func callDiagnoseToolWithTimeout(
	rpcClient *gatewayclient.GatewayRPCClient,
	buffer *UTF8RingBuffer,
	options ManualShellOptions,
	shellSessionID string,
	trigger diagnoseTrigger,
	timeout time.Duration,
) (tools.ToolResult, error) {
	prepared, err := prepareDiagnoseRequest(buffer, options, shellSessionID, trigger)
	if err != nil {
		if options.Stderr != nil {
			writeProxyf(options.Stderr, "neocode diag: build diagnose payload failed: %v\n", err)
		}
		return tools.ToolResult{}, errors.New("failed to build diagnosis payload")
	}
	return executePreparedDiagnoseToolWithTimeout(rpcClient, nil, options, prepared, timeout)
}

// decodeToolResult 闂傚倸鍊搁崐鎼佸磹閹间礁纾圭€瑰嫭鍣磋ぐ鎺戠倞鐟滃繘寮抽敃鍌涚厱妞ゎ厽鍨垫禍婵嬫煕濞嗗繒绠婚柡宀嬬秮婵偓闁靛繆鏅濋崝鍝ョ磽娴ｆ彃浜炬繝銏ｆ硾椤戝嫮鎹㈤崱娑欑厪闁割偅绻冮崳娲煕閿濆懏璐＄紒杈ㄥ浮楠炲洭顢樿閻や線姊洪崫鍕効缂佺粯绻傞悾鐑藉醇閺囩倣銊╂煏婢诡垰鍊诲Λ顖炴⒒?decodeToolResult 闂傚倸鍊搁崐鎼佸磹閹间礁纾归柟闂寸绾惧綊鏌ｉ幋锝呅撻柛銈呭閺屾盯顢曢敐鍡欘槬缂佺偓鍎冲锟犲蓟閿濆顫呴柕蹇婃櫇閸旀悂姊哄Ч鍥р偓妤呭磻閹捐埖宕叉繝闈涱儐椤ュ牊绻涢幋婵嗚埞闁告搩鍓熷娲焻閻愯尪瀚板褍澧界槐鎺楁偐閾忣偄纰嶉梺浼欑秮閺€杈╃紦閻ｅ瞼鐭欓悹鍥﹀嫎閸旀垿寮婚弴鐔虹彾妞ゆ牗纰嶉崳瑙勵殽閻愨晛浜鹃梻鍌氬€峰ù鍥х暦閻㈢绐楅柟閭﹀枛閸ㄦ繄鈧箍鍎遍幉妯衡槈閵忕姷顦ㄥ銈庡幗閸ㄩ潧鈻撻妸锔剧瘈闁汇垽娼ф牎闂佽壈顫夐崕鎶筋敊韫囨挴鏀介悗锝庡亞閸樺憡绻濋姀锝嗙【妞ゆ垵鎳橀幃姗€濡疯閸嬫挸鈻撻崹顔界亾闂佺绻戦敋閾荤偤鏌涘☉娆愬剹闁轰礁鍟撮弻鏇＄疀婵炴儳浜鹃柟棰佺劍琚╂繝鐢靛Х閺佹悂宕戝☉姗嗗殨闁割偅娲橀崑瀣節婵犲倹鍣归柛銊︾箞閺屽秹宕崟顐熷亾婵犳碍鈷旈柛鏇ㄥ灡閻撴洘绻涢幋鐐╂闁割偁鍎辩粻?
func decodeToolResult(payload any) (tools.ToolResult, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("encode tool payload: %w", err)
	}
	var result tools.ToolResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return tools.ToolResult{}, fmt.Errorf("decode tool payload: %w", err)
	}
	return result, nil
}

// renderDiagnosis 闂傚倸鍊搁崐鎼佸磹閹间礁纾圭€瑰嫭鍣磋ぐ鎺戠倞鐟滃繘寮抽敃鍌涚厱妞ゎ厽鍨垫禍婵嬫煕濞嗗繒绠婚柡宀嬬秮婵偓闁靛繆鏅濋崝鍝ョ磽娴ｆ彃浜炬繝銏ｆ硾椤戝嫮鎹㈤崱娑欑厪闁割偅绻冮崳娲煕閿濆懏璐＄紒杈ㄥ浮楠炲洭顢樿閻や線姊洪崫鍕効缂佺粯绻傞悾鐑藉醇閺囩倣銊╂煏婢诡垰鍊诲Λ顖炴⒒?renderDiagnosis 闂傚倸鍊搁崐鎼佸磹閹间礁纾归柟闂寸绾惧綊鏌ｉ幋锝呅撻柛銈呭閺屾盯顢曢敐鍡欘槬缂佺偓鍎冲锟犲蓟閿濆顫呴柕蹇婃櫇閸旀悂姊哄Ч鍥р偓妤呭磻閹捐埖宕叉繝闈涱儐椤ュ牊绻涢幋婵嗚埞闁告搩鍓熷娲焻閻愯尪瀚板褍澧界槐鎺楁偐閾忣偄纰嶉梺浼欑秮閺€杈╃紦閻ｅ瞼鐭欓悹鍥﹀嫎閸旀垿寮婚弴鐔虹彾妞ゆ牗纰嶉崳瑙勵殽閻愨晛浜鹃梻鍌氬€峰ù鍥х暦閻㈢绐楅柟閭﹀枛閸ㄦ繄鈧箍鍎遍幉妯衡槈閵忕姷顦ㄥ銈庡幗閸ㄩ潧鈻撻妸锔剧瘈闁汇垽娼ф牎闂佽壈顫夐崕鎶筋敊韫囨挴鏀介悗锝庡亞閸樺憡绻濋姀锝嗙【妞ゆ垵鎳橀幃姗€濡疯閸嬫挸鈻撻崹顔界亾闂佺绻戦敋閾荤偤鏌涘☉娆愬剹闁轰礁鍟撮弻鏇＄疀婵炴儳浜鹃柟棰佺劍琚╂繝鐢靛Х閺佹悂宕戝☉姗嗗殨闁割偅娲橀崑瀣節婵犲倹鍣归柛銊︾箞閺屽秹宕崟顐熷亾婵犳碍鈷旈柛鏇ㄥ灡閻撴洘绻涢幋鐐╂闁割偁鍎辩粻?
func renderDiagnosis(output io.Writer, content string, isError bool) {
	headerColor := "\033[36m"
	if isError {
		headerColor = "\033[31m"
	}
	writeProxyf(output, "\n%s[NeoCode Diagnosis]\033[0m\n", headerColor)

	trimmedContent := strings.TrimSpace(content)
	if trimmedContent == "" {
		writeProxyLine(output, "- no diagnosis output")
		return
	}

	var parsed diagnoseToolResult
	if err := json.Unmarshal([]byte(trimmedContent), &parsed); err != nil || strings.TrimSpace(parsed.RootCause) == "" {
		writeProxyLine(output, trimmedContent)
		return
	}

	writeProxyf(output, "confidence: %.2f\n", parsed.Confidence)
	writeProxyf(output, "root cause: %s\n", strings.TrimSpace(parsed.RootCause))
	if len(parsed.InvestigationCommands) > 0 {
		writeProxyLine(output, "investigation commands:")
		for _, command := range parsed.InvestigationCommands {
			writeProxyf(output, "- %s\n", strings.TrimSpace(command))
		}
	}
	if len(parsed.FixCommands) > 0 {
		writeProxyLine(output, "fix commands:")
		for _, command := range parsed.FixCommands {
			writeProxyf(output, "- %s\n", strings.TrimSpace(command))
		}
	}
}

// streamPTYOutput 闂傚倸鍊搁崐鎼佸磹閹间礁纾圭€瑰嫭鍣磋ぐ鎺戠倞鐟滃繘寮抽敃鍌涚厱妞ゎ厽鍨垫禍婵嬫煕濞嗗繒绠婚柡宀嬬秮婵偓闁靛繆鏅濋崝鍝ョ磽娴ｆ彃浜炬繝銏ｆ硾椤戝嫮鎹㈤崱娑欑厪闁割偅绻冮崳娲煕閿濆懏璐＄紒杈ㄥ浮楠炲洭顢樿閻や線姊洪崫鍕効缂佺粯绻傞悾鐑藉醇閺囩倣銊╂煏婢诡垰鍊诲Λ顖炴⒒?streamPTYOutput 闂傚倸鍊搁崐鎼佸磹閹间礁纾归柟闂寸绾惧綊鏌ｉ幋锝呅撻柛銈呭閺屾盯顢曢敐鍡欘槬缂佺偓鍎冲锟犲蓟閿濆顫呴柕蹇婃櫇閸旀悂姊哄Ч鍥р偓妤呭磻閹捐埖宕叉繝闈涱儐椤ュ牊绻涢幋婵嗚埞闁告搩鍓熷娲焻閻愯尪瀚板褍澧界槐鎺楁偐閾忣偄纰嶉梺浼欑秮閺€杈╃紦閻ｅ瞼鐭欓悹鍥﹀嫎閸旀垿寮婚弴鐔虹彾妞ゆ牗纰嶉崳瑙勵殽閻愨晛浜鹃梻鍌氬€峰ù鍥х暦閻㈢绐楅柟閭﹀枛閸ㄦ繄鈧箍鍎遍幉妯衡槈閵忕姷顦ㄥ銈庡幗閸ㄩ潧鈻撻妸锔剧瘈闁汇垽娼ф牎闂佽壈顫夐崕鎶筋敊韫囨挴鏀介悗锝庡亞閸樺憡绻濋姀锝嗙【妞ゆ垵鎳橀幃姗€濡疯閸嬫挸鈻撻崹顔界亾闂佺绻戦敋閾荤偤鏌涘☉娆愬剹闁轰礁鍟撮弻鏇＄疀婵炴儳浜鹃柟棰佺劍琚╂繝鐢靛Х閺佹悂宕戝☉姗嗗殨闁割偅娲橀崑瀣節婵犲倹鍣归柛銊︾箞閺屽秹宕崟顐熷亾婵犳碍鈷旈柛鏇ㄥ灡閻撴洘绻涢幋鐐╂闁割偁鍎辩粻?
func streamPTYOutput(
	ptyReader io.Reader,
	outputSink io.Writer,
	commandLogBuffer *UTF8RingBuffer,
	tracker *commandTracker,
	autoTriggerCh chan<- diagnoseTrigger,
	autoState *autoRuntimeState,
) {
	streamPTYOutputWithIDM(ptyReader, outputSink, commandLogBuffer, tracker, autoTriggerCh, nil, autoState, nil)
}

// streamPTYOutputWithIDM 闂傚倸鍊搁崐鎼佸磹閹间礁纾圭€瑰嫭鍣磋ぐ鎺戠倞鐟滃繘寮抽敃鍌涚厱妞ゎ厽鍨垫禍婵嬫煕濞嗗繒绠婚柡宀嬬秮婵偓闁靛繆鏅濋崝鍝ョ磽娴ｆ彃浜炬繝銏ｆ硾椤戝嫮鎹㈤崱娑欑厪闁割偅绻冮崳娲煕閿濆懏璐＄紒杈ㄥ浮楠炲洭顢樿閻や線姊洪崫鍕効缂佺粯绻傞悾鐑藉醇閺囩倣銊╂煏婢诡垰鍊诲Λ顖炴⒒?streamPTYOutputWithIDM 闂傚倸鍊搁崐鎼佸磹閹间礁纾归柟闂寸绾惧綊鏌ｉ幋锝呅撻柛銈呭閺屾盯顢曢敐鍡欘槬缂佺偓鍎冲锟犲蓟閿濆顫呴柕蹇婃櫇閸旀悂姊哄Ч鍥р偓妤呭磻閹捐埖宕叉繝闈涱儐椤ュ牊绻涢幋婵嗚埞闁告搩鍓熷娲焻閻愯尪瀚板褍澧界槐鎺楁偐閾忣偄纰嶉梺浼欑秮閺€杈╃紦閻ｅ瞼鐭欓悹鍥﹀嫎閸旀垿寮婚弴鐔虹彾妞ゆ牗纰嶉崳瑙勵殽閻愨晛浜鹃梻鍌氬€峰ù鍥х暦閻㈢绐楅柟閭﹀枛閸ㄦ繄鈧箍鍎遍幉妯衡槈閵忕姷顦ㄥ銈庡幗閸ㄩ潧鈻撻妸锔剧瘈闁汇垽娼ф牎闂佽壈顫夐崕鎶筋敊韫囨挴鏀介悗锝庡亞閸樺憡绻濋姀锝嗙【妞ゆ垵鎳橀幃姗€濡疯閸嬫挸鈻撻崹顔界亾闂佺绻戦敋閾荤偤鏌涘☉娆愬剹闁轰礁鍟撮弻鏇＄疀婵炴儳浜鹃柟棰佺劍琚╂繝鐢靛Х閺佹悂宕戝☉姗嗗殨闁割偅娲橀崑瀣節婵犲倹鍣归柛銊︾箞閺屽秹宕崟顐熷亾婵犳碍鈷旈柛鏇ㄥ灡閻撴洘绻涢幋鐐╂闁割偁鍎辩粻?
func streamPTYOutputWithIDM(
	ptyReader io.Reader,
	outputSink io.Writer,
	commandLogBuffer *UTF8RingBuffer,
	tracker *commandTracker,
	autoTriggerCh chan<- diagnoseTrigger,
	recentTriggerStore *diagnosisTriggerStore,
	autoState *autoRuntimeState,
	idm *idmController,
) {
	if ptyReader == nil || outputSink == nil || commandLogBuffer == nil {
		return
	}
	parser := &OSC133Parser{}
	altScreen := newAltScreenState(IsAltScreenGuardEnabledFromEnv())
	collectingCommand := false
	pendingTrigger := (*diagnoseTrigger)(nil)
	fallbackCommandBuffer := NewUTF8RingBuffer(DefaultRingBufferCapacity / 2)

	buffer := make([]byte, 4096)
	for {
		readBytes, err := ptyReader.Read(buffer)
		if readBytes > 0 {
			altScreen.Observe(buffer[:readBytes])
			cleanOutput, events := parser.Feed(buffer[:readBytes])
			if idm != nil && len(cleanOutput) > 0 {
				cleanOutput = idm.FilterPTYOutput(cleanOutput)
			}
			if len(cleanOutput) > 0 {
				_, _ = outputSink.Write(cleanOutput)
				_, _ = fallbackCommandBuffer.Write(cleanOutput)
				if collectingCommand {
					_, _ = commandLogBuffer.Write(cleanOutput)
				}
			}
			for _, event := range events {
				if idm != nil {
					idm.OnShellEvent(event)
				}
				switch event.Type {
				case ShellEventPromptReady:
					autoState.OSCReady.Store(true)
					if pendingTrigger != nil && autoState.Enabled.Load() {
						if altScreen.ShouldSuppressAutoTrigger(true) {
							pendingTrigger = nil
							fallbackCommandBuffer.Reset()
							continue
						}
						select {
						case autoTriggerCh <- *pendingTrigger:
						default:
							// 闂傚倸鍊搁崐鎼佸磹閹间礁纾归柟闂寸绾剧懓顪冪€ｎ亜顒㈡い鎰矙閺屻劑鎮㈤崫鍕戙垻鐥幆褜鐓奸柡灞剧☉閳藉宕￠悙鑼啋闂備礁鎼Λ妤€螞濠靛钃熸繛鎴炃氬Σ鍫熸叏濡も偓閻楀棙鎱ㄥ☉姗嗘富闁靛牆鍊瑰▍鍡欐喐閺夊灝鏆㈤柣蹇撳暣濮婃椽骞愭惔锝囩暤闂佺懓鍟块柊锝咁嚕閸愭祴鏋庨柟瀵稿Х閿涙粓姊虹紒妯兼喛闁稿鎹囬弻娑㈠Ω閵壯冪厽閻庢鍠栭…宄邦嚕閹绢喗鍋勫瀣捣閻涱噣姊绘担绋款棌闁稿鎳愰幑銏ゅ磼濞戞瑥寮块梺鍓插亝濞叉﹢鍩涢幋锕€绾ч柣鎰綑椤ュ鏌涢弬璺ㄐら柍褜鍓欓崢婊堝磻閹剧粯鐓欓梻鍌氼嚟椤︼箓鏌﹂崘顏勬灈闁哄被鍔戦幃銏ゅ传閸曟垯鍨荤槐鎺楀焵椤掑嫬绀冩い鏃傛櫕閸橆亝绻濋悽闈涗粶闁诲繑绻堝畷婵嬪箻椤旂晫鍘搁梺绯曞墲閸旀洟鎮橀埡鍛厓閻熸瑥瀚悘鎾煙椤旂晫鎳囩€殿喖鐖奸獮瀣偐閻愮懓浠繝鐢靛Х閺佹悂宕戦悢鐓幬ラ悗锝庡墯瀹曞弶绻濋棃娑卞剱闁稿蓱閵囧嫰寮村Δ鈧禍鎯旈悩闈涗沪閻㈩垱甯熼悘鍐⒑闁偛鑻晶顕€鏌℃笟鍥ф珝婵﹦绮粭鐔煎焵椤掆偓椤洩顦归柟顔ㄥ洤骞㈡俊鐐灪閻╊垰顕ｉ浣瑰劅闁圭偓鎯屽鏃€淇婇悙顏勨偓鏇犳崲閹扮増鍋嬪┑鐘叉搐绾惧綊鏌涢…鎴濅簽缂佺娀绠栭弻娑㈩敃閿濆洨鐣兼繛瀵稿О閸ㄤ粙寮婚敍鍕ㄥ亾閿濆骸浜炴繛鍙夋尦閺岋紕浠﹂崜褎鍒涢梺璇″枔閸婃牠骞忛崨鏉戜紶闁靛鍊曢幗瀣⒒閸屾瑧鍔嶉悗绗涘厾楦跨疀濞戞ê鐎梺鑺ッˇ浠嬪吹閺囥垺鐓犵紒瀣硶娴犳盯鏌涙惔锝呮灈闁哄瞼鍠栭獮鍡氼檨闁搞倗鍠栭弻宥堫檨闁告挻鐟╅垾锕€鐣￠柇锔界稁婵犵數濮甸懝鍓х矆鐎ｎ偁浜滈柟鏉垮閹ジ鏌嶇憴鍕槈妞ゎ亜鍟存俊鍫曞礃閵娿垹浜鹃柡鍥ュ灩绾惧綊鏌涢…鎴濇珮婵炲吋鐗楃换娑橆啅椤旇崵鐩庨悗鐟版啞缁诲倿鍩為幋锔藉亹闁圭粯甯╂禒楣冩⒑闂堚晝绉甸柛娆忓暙椤繘鎼归悷鏉款嚙闂佸搫娲ㄩ崰鎰版偟閺冨牊鈷戦柟绋垮缁岃法绱掗悩鍐茬仴鐎规挸瀚板楦裤亹閹烘垳鍠婇梺绋跨箲閿曘垹顕ｉ妸锔绢浄閻庯綆鍋勯埀顒傛暬閺屻劌鈹戦崱娑扁偓妤€霉濠婂懎浠遍柡灞剧洴瀵剛鎹勯妸銉у幗婵犳鍠栭敃銉ヮ渻閽樺鏆︽繝濠傜墛閸ゆ垶銇勯幒鍡椾壕闁汇埄鍨崕闈涱潖濞差亜浼犻柕澶堝剾閿濆鐓熸俊銈勮兌閻帞鈧鍣崑濠囥€佸璺虹劦妞ゆ帒瀚畵渚€骞栧ǎ顒€濡介柛銈呯Ч閺屾洘寰勯崼婵堜痪闁诲孩鐭划娆忣潖缂佹ɑ濯撮柛娑橈工閺嗗牓姊洪崨濠冾棖缂佺姵鎸搁悾鐑芥晲閸℃ê鍔呴梺闈涱焾閸庨亶骞嗛悙娴嬫斀闁绘劕寮堕ˉ鐐烘煕閺冣偓閸ㄥ墎绮?PTY 闂傚倸鍊搁崐鎼佸磹妞嬪海鐭嗗〒姘ｅ亾妤犵偞鐗犻、鏇氱秴闁搞儺鍓﹂弫宥夋煟閹邦厽缍戝ù婊嗛哺缁绘繈鎮介棃娴躲垺绻涚仦鍌氣偓妤冨垝缂佹ê顕遍悗娑欘焽閸樼敻姊婚崒姘偓鎼侇敋椤撯懞鍥晝閸屾稓鍘卞┑顔缴戦崜姘焽閹扮増鐓忛柛銉戝喚浼冨Δ鐘靛仦椤洭骞戦崟顖涘€绘俊顖炴？缁ㄦ挳姊婚崒娆戭槮缁剧虎鍘鹃崚鎺楊敍濮ｅ吋鐩畷姗€顢欓懖鈺冩毇闁荤喐绮庢晶妤冩暜閹烘鍊峰┑鐘插閸犳劗鈧箍鍎卞ú銊ノ涢婊勫枑闁哄啫鐗嗛拑鐔哥箾閹寸偟鎳勯柛搴ｅ枛閺屾洝绠涢妷褏锛熷銈忕細閸楀啿顫忓ú顏勪紶闁告洦鍘鹃崝鍦磽娴ｈ棄绱︾紒顔界懇閸ㄩ箖鏁冮崒姣尖晠鏌ㄩ弮鍥棄闁告柨鎳樺娲箰鎼达絺妲堥柣搴㈠嚬娴滅偤宕氶幒妤€閱囬柡鍥╁暱閹?
						}
						pendingTrigger = nil
					}
					fallbackCommandBuffer.Reset()
				case ShellEventCommandStart:
					collectingCommand = true
					commandLogBuffer = NewUTF8RingBuffer(DefaultRingBufferCapacity / 2)
					fallbackCommandBuffer.Reset()
				case ShellEventCommandDone:
					collectingCommand = false
					commandText := ""
					if tracker != nil {
						commandText = tracker.LastCommand()
					}
					outputText := commandLogBuffer.SnapshotString()
					if !hasMeaningfulOutput(outputText) {
						outputText = fallbackCommandBuffer.SnapshotString()
					}
					trigger := diagnoseTrigger{
						CommandText: commandText,
						ExitCode:    event.ExitCode,
						OutputText:  outputText,
					}
					if recentTriggerStore != nil {
						recentTriggerStore.Remember(trigger)
					}
					if ShouldTriggerAutoDiagnosis(event.ExitCode, commandText, outputText) {
						if altScreen.ShouldSuppressAutoTrigger(true) {
							pendingTrigger = nil
							continue
						}
						pendingTrigger = &trigger
					}
				}
			}
		}
		if err != nil {
			return
		}
	}
}

// pumpProxyInput 闂傚倸鍊搁崐鎼佸磹閹间礁纾圭€瑰嫭鍣磋ぐ鎺戠倞鐟滃繘寮抽敃鍌涚厱妞ゎ厽鍨垫禍婵嬫煕濞嗗繒绠婚柡宀嬬秮婵偓闁靛繆鏅濋崝鍝ョ磽娴ｆ彃浜炬繝銏ｆ硾椤戝嫮鎹㈤崱娑欑厪闁割偅绻冮崳娲煕閿濆懏璐＄紒杈ㄥ浮楠炲洭顢樿閻や線姊洪崫鍕効缂佺粯绻傞悾鐑藉醇閺囩倣銊╂煏婢诡垰鍊诲Λ顖炴⒒?pumpProxyInput 闂傚倸鍊搁崐鎼佸磹閹间礁纾归柟闂寸绾惧綊鏌ｉ幋锝呅撻柛銈呭閺屾盯顢曢敐鍡欘槬缂佺偓鍎冲锟犲蓟閿濆顫呴柕蹇婃櫇閸旀悂姊哄Ч鍥р偓妤呭磻閹捐埖宕叉繝闈涱儐椤ュ牊绻涢幋婵嗚埞闁告搩鍓熷娲焻閻愯尪瀚板褍澧界槐鎺楁偐閾忣偄纰嶉梺浼欑秮閺€杈╃紦閻ｅ瞼鐭欓悹鍥﹀嫎閸旀垿寮婚弴鐔虹彾妞ゆ牗纰嶉崳瑙勵殽閻愨晛浜鹃梻鍌氬€峰ù鍥х暦閻㈢绐楅柟閭﹀枛閸ㄦ繄鈧箍鍎遍幉妯衡槈閵忕姷顦ㄥ銈庡幗閸ㄩ潧鈻撻妸锔剧瘈闁汇垽娼ф牎闂佽壈顫夐崕鎶筋敊韫囨挴鏀介悗锝庡亞閸樺憡绻濋姀锝嗙【妞ゆ垵鎳橀幃姗€濡疯閸嬫挸鈻撻崹顔界亾闂佺绻戦敋閾荤偤鏌涘☉娆愬剹闁轰礁鍟撮弻鏇＄疀婵炴儳浜鹃柟棰佺劍琚╂繝鐢靛Х閺佹悂宕戝☉姗嗗殨闁割偅娲橀崑瀣節婵犲倹鍣归柛銊︾箞閺屽秹宕崟顐熷亾婵犳碍鈷旈柛鏇ㄥ灡閻撴洘绻涢幋鐐╂闁割偁鍎辩粻?
func pumpProxyInput(
	ctx context.Context,
	src io.Reader,
	ptyWriter io.Writer,
	tracker *commandTracker,
	idm *idmController,
) {
	if src == nil || ptyWriter == nil {
		return
	}
	buffer := make([]byte, 4096)
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		readCount, err := src.Read(buffer)
		if readCount > 0 {
			payload := buffer[:readCount]
			for _, item := range payload {
				if idm != nil && idm.IsActive() {
					if idm.ShouldPassthroughInput() {
						if tracker != nil {
							tracker.Observe([]byte{item})
						}
						_, _ = ptyWriter.Write([]byte{item})
						continue
					}
					idm.HandleInputByte(item)
					continue
				}
				if tracker != nil {
					tracker.Observe([]byte{item})
				}
				_, _ = ptyWriter.Write([]byte{item})
			}
		}
		if err != nil {
			return
		}
	}
}

// copyInputWithTracker 闂傚倸鍊搁崐鎼佸磹閹间礁纾圭€瑰嫭鍣磋ぐ鎺戠倞鐟滃繘寮抽敃鍌涚厱妞ゎ厽鍨垫禍婵嬫煕濞嗗繒绠婚柡宀嬬秮婵偓闁靛繆鏅濋崝鍝ョ磽娴ｆ彃浜炬繝銏ｆ硾椤戝嫮鎹㈤崱娑欑厪闁割偅绻冮崳娲煕閿濆懏璐＄紒杈ㄥ浮楠炲洭顢樿閻や線姊洪崫鍕効缂佺粯绻傞悾鐑藉醇閺囩倣銊╂煏婢诡垰鍊诲Λ顖炴⒒?copyInputWithTracker 闂傚倸鍊搁崐鎼佸磹閹间礁纾归柟闂寸绾惧綊鏌ｉ幋锝呅撻柛銈呭閺屾盯顢曢敐鍡欘槬缂佺偓鍎冲锟犲蓟閿濆顫呴柕蹇婃櫇閸旀悂姊哄Ч鍥р偓妤呭磻閹捐埖宕叉繝闈涱儐椤ュ牊绻涢幋婵嗚埞闁告搩鍓熷娲焻閻愯尪瀚板褍澧界槐鎺楁偐閾忣偄纰嶉梺浼欑秮閺€杈╃紦閻ｅ瞼鐭欓悹鍥﹀嫎閸旀垿寮婚弴鐔虹彾妞ゆ牗纰嶉崳瑙勵殽閻愨晛浜鹃梻鍌氬€峰ù鍥х暦閻㈢绐楅柟閭﹀枛閸ㄦ繄鈧箍鍎遍幉妯衡槈閵忕姷顦ㄥ銈庡幗閸ㄩ潧鈻撻妸锔剧瘈闁汇垽娼ф牎闂佽壈顫夐崕鎶筋敊韫囨挴鏀介悗锝庡亞閸樺憡绻濋姀锝嗙【妞ゆ垵鎳橀幃姗€濡疯閸嬫挸鈻撻崹顔界亾闂佺绻戦敋閾荤偤鏌涘☉娆愬剹闁轰礁鍟撮弻鏇＄疀婵炴儳浜鹃柟棰佺劍琚╂繝鐢靛Х閺佹悂宕戝☉姗嗗殨闁割偅娲橀崑瀣節婵犲倹鍣归柛銊︾箞閺屽秹宕崟顐熷亾婵犳碍鈷旈柛鏇ㄥ灡閻撴洘绻涢幋鐐╂闁割偁鍎辩粻?
func copyInputWithTracker(dst io.Writer, src io.Reader, tracker *commandTracker) (int64, error) {
	if dst == nil || src == nil {
		return 0, nil
	}
	written := int64(0)
	buffer := make([]byte, 4096)
	for {
		n, err := src.Read(buffer)
		if n > 0 {
			payload := buffer[:n]
			if tracker != nil {
				tracker.Observe(payload)
			}
			m, writeErr := dst.Write(payload)
			written += int64(m)
			if writeErr != nil {
				return written, writeErr
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return written, nil
			}
			return written, err
		}
	}
}

// Observe 闂傚倸鍊搁崐鎼佸磹閹间礁纾圭€瑰嫭鍣磋ぐ鎺戠倞鐟滃繘寮抽敃鍌涚厱妞ゎ厽鍨垫禍婵嬫煕濞嗗繒绠婚柡宀嬬秮婵偓闁靛繆鏅濋崝鍝ョ磽娴ｆ彃浜炬繝銏ｆ硾椤戝嫮鎹㈤崱娑欑厪闁割偅绻冮崳娲煕閿濆懏璐＄紒杈ㄥ浮楠炲洭顢樿閻や線姊洪崫鍕効缂佺粯绻傞悾鐑藉醇閺囩倣銊╂煏婢诡垰鍊诲Λ顖炴⒒?Observe 闂傚倸鍊搁崐鎼佸磹閹间礁纾归柟闂寸绾惧綊鏌ｉ幋锝呅撻柛銈呭閺屾盯顢曢敐鍡欘槬缂佺偓鍎冲锟犲蓟閿濆顫呴柕蹇婃櫇閸旀悂姊哄Ч鍥р偓妤呭磻閹捐埖宕叉繝闈涱儐椤ュ牊绻涢幋婵嗚埞闁告搩鍓熷娲焻閻愯尪瀚板褍澧界槐鎺楁偐閾忣偄纰嶉梺浼欑秮閺€杈╃紦閻ｅ瞼鐭欓悹鍥﹀嫎閸旀垿寮婚弴鐔虹彾妞ゆ牗纰嶉崳瑙勵殽閻愨晛浜鹃梻鍌氬€峰ù鍥х暦閻㈢绐楅柟閭﹀枛閸ㄦ繄鈧箍鍎遍幉妯衡槈閵忕姷顦ㄥ銈庡幗閸ㄩ潧鈻撻妸锔剧瘈闁汇垽娼ф牎闂佽壈顫夐崕鎶筋敊韫囨挴鏀介悗锝庡亞閸樺憡绻濋姀锝嗙【妞ゆ垵鎳橀幃姗€濡疯閸嬫挸鈻撻崹顔界亾闂佺绻戦敋閾荤偤鏌涘☉娆愬剹闁轰礁鍟撮弻鏇＄疀婵炴儳浜鹃柟棰佺劍琚╂繝鐢靛Х閺佹悂宕戝☉姗嗗殨闁割偅娲橀崑瀣節婵犲倹鍣归柛銊︾箞閺屽秹宕崟顐熷亾婵犳碍鈷旈柛鏇ㄥ灡閻撴洘绻涢幋鐐╂闁割偁鍎辩粻?
// isClosedNetworkError 闂傚倸鍊搁崐鎼佸磹閹间礁纾圭€瑰嫭鍣磋ぐ鎺戠倞鐟滃繘寮抽敃鍌涚厱妞ゎ厽鍨垫禍婵嬫煕濞嗗繒绠婚柡宀嬬秮婵偓闁靛繆鏅濋崝鍝ョ磽娴ｆ彃浜炬繝銏ｆ硾椤戝嫮鎹㈤崱娑欑厪闁割偅绻冮崳娲煕閿濆懏璐＄紒杈ㄥ浮楠炲洭顢樿閻や線姊洪崫鍕効缂佺粯绻傞悾鐑藉醇閺囩倣銊╂煏婢诡垰鍊诲Λ顖炴⒒?isClosedNetworkError 闂傚倸鍊搁崐鎼佸磹閹间礁纾归柟闂寸绾惧綊鏌ｉ幋锝呅撻柛銈呭閺屾盯顢曢敐鍡欘槬缂佺偓鍎冲锟犲蓟閿濆顫呴柕蹇婃櫇閸旀悂姊哄Ч鍥р偓妤呭磻閹捐埖宕叉繝闈涱儐椤ュ牊绻涢幋婵嗚埞闁告搩鍓熷娲焻閻愯尪瀚板褍澧界槐鎺楁偐閾忣偄纰嶉梺浼欑秮閺€杈╃紦閻ｅ瞼鐭欓悹鍥﹀嫎閸旀垿寮婚弴鐔虹彾妞ゆ牗纰嶉崳瑙勵殽閻愨晛浜鹃梻鍌氬€峰ù鍥х暦閻㈢绐楅柟閭﹀枛閸ㄦ繄鈧箍鍎遍幉妯衡槈閵忕姷顦ㄥ銈庡幗閸ㄩ潧鈻撻妸锔剧瘈闁汇垽娼ф牎闂佽壈顫夐崕鎶筋敊韫囨挴鏀介悗锝庡亞閸樺憡绻濋姀锝嗙【妞ゆ垵鎳橀幃姗€濡疯閸嬫挸鈻撻崹顔界亾闂佺绻戦敋閾荤偤鏌涘☉娆愬剹闁轰礁鍟撮弻鏇＄疀婵炴儳浜鹃柟棰佺劍琚╂繝鐢靛Х閺佹悂宕戝☉姗嗗殨闁割偅娲橀崑瀣節婵犲倹鍣归柛銊︾箞閺屽秹宕崟顐熷亾婵犳碍鈷旈柛鏇ㄥ灡閻撴洘绻涢幋鐐╂闁割偁鍎辩粻?
func isClosedNetworkError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, net.ErrClosed) {
		return true
	}
	lower := strings.ToLower(strings.TrimSpace(err.Error()))
	return strings.Contains(lower, "use of closed network connection")
}

// serializedWriter 闂傚倸鍊搁崐鎼佸磹閹间礁纾圭€瑰嫭鍣磋ぐ鎺戠倞鐟滃繘寮抽敃鍌涚厱妞ゎ厽鍨垫禍婵嬫煕濞嗗繒绠婚柡宀嬬秮婵偓闁靛繆鏅濋崝鍝ョ磽娴ｆ彃浜炬繝銏ｆ硾椤戝嫮鎹㈤崱娑欑厪闁割偅绻冮崳娲煕閿濆懏璐＄紒杈ㄥ浮楠炲洭顢樿閻や線姊洪崫鍕効缂佺粯绻傞悾鐑藉醇閺囩倣銊╂煏婢诡垰鍊诲Λ顖炴⒒?serializedWriter 闂傚倸鍊搁崐鎼佸磹閹间礁纾归柟闂寸绾惧綊鏌ｉ幋锝呅撻柛銈呭閺屾盯顢曢敐鍡欘槬缂佺偓鍎冲锟犲蓟閿濆顫呴柕蹇婃櫇閸旀悂姊哄Ч鍥р偓妤呭磻閹捐埖宕叉繝闈涱儐椤ュ牊绻涢幋婵嗚埞闁告搩鍓熷娲焻閻愯尪瀚板褍澧界槐鎺楁偐閾忣偄纰嶉梺浼欑秮閺€杈╃紦閻ｅ瞼鐭欓悹鍥﹀嫎閸旀垿寮婚弴鐔虹彾妞ゆ牗纰嶉崳瑙勵殽閻愨晛浜鹃梻鍌氬€峰ù鍥х暦閻㈢绐楅柟閭﹀枛閸ㄦ繄鈧箍鍎遍幉妯衡槈閵忕姷顦ㄥ銈庡幗閸ㄩ潧鈻撻妸锔剧瘈闁汇垽娼ф牎闂佽壈顫夐崕鎶筋敊韫囨挴鏀介悗锝庡亞閸樺憡绻濋姀锝嗙【妞ゆ垵鎳橀幃姗€濡疯閸嬫挸鈻撻崹顔界亾闂佺绻戦敋閾荤偤鏌涘☉娆愬剹闁轰礁鍟撮弻鏇＄疀婵炴儳浜鹃柟棰佺劍琚╂繝鐢靛Х閺佹悂宕戝☉姗嗗殨闁割偅娲橀崑瀣節婵犲倹鍣归柛銊︾箞閺屽秹宕崟顐熷亾婵犳碍鈷旈柛鏇ㄥ灡閻撴洘绻涢幋鐐╂闁割偁鍎辩粻?
type serializedWriter struct {
	writer io.Writer
	lock   *sync.Mutex
}

// Write 闂傚倸鍊搁崐鎼佸磹閹间礁纾圭€瑰嫭鍣磋ぐ鎺戠倞鐟滃繘寮抽敃鍌涚厱妞ゎ厽鍨垫禍婵嬫煕濞嗗繒绠婚柡宀嬬秮婵偓闁靛繆鏅濋崝鍝ョ磽娴ｆ彃浜炬繝銏ｆ硾椤戝嫮鎹㈤崱娑欑厪闁割偅绻冮崳娲煕閿濆懏璐＄紒杈ㄥ浮楠炲洭顢樿閻や線姊洪崫鍕効缂佺粯绻傞悾鐑藉醇閺囩倣銊╂煏婢诡垰鍊诲Λ顖炴⒒?Write 闂傚倸鍊搁崐鎼佸磹閹间礁纾归柟闂寸绾惧綊鏌ｉ幋锝呅撻柛銈呭閺屾盯顢曢敐鍡欘槬缂佺偓鍎冲锟犲蓟閿濆顫呴柕蹇婃櫇閸旀悂姊哄Ч鍥р偓妤呭磻閹捐埖宕叉繝闈涱儐椤ュ牊绻涢幋婵嗚埞闁告搩鍓熷娲焻閻愯尪瀚板褍澧界槐鎺楁偐閾忣偄纰嶉梺浼欑秮閺€杈╃紦閻ｅ瞼鐭欓悹鍥﹀嫎閸旀垿寮婚弴鐔虹彾妞ゆ牗纰嶉崳瑙勵殽閻愨晛浜鹃梻鍌氬€峰ù鍥х暦閻㈢绐楅柟閭﹀枛閸ㄦ繄鈧箍鍎遍幉妯衡槈閵忕姷顦ㄥ銈庡幗閸ㄩ潧鈻撻妸锔剧瘈闁汇垽娼ф牎闂佽壈顫夐崕鎶筋敊韫囨挴鏀介悗锝庡亞閸樺憡绻濋姀锝嗙【妞ゆ垵鎳橀幃姗€濡疯閸嬫挸鈻撻崹顔界亾闂佺绻戦敋閾荤偤鏌涘☉娆愬剹闁轰礁鍟撮弻鏇＄疀婵炴儳浜鹃柟棰佺劍琚╂繝鐢靛Х閺佹悂宕戝☉姗嗗殨闁割偅娲橀崑瀣節婵犲倹鍣归柛銊︾箞閺屽秹宕崟顐熷亾婵犳碍鈷旈柛鏇ㄥ灡閻撴洘绻涢幋鐐╂闁割偁鍎辩粻?
func (w *serializedWriter) Write(payload []byte) (int, error) {
	if w == nil || w.writer == nil {
		return len(payload), nil
	}
	if w.lock == nil {
		return w.writer.Write(payload)
	}
	w.lock.Lock()
	defer w.lock.Unlock()
	return w.writer.Write(payload)
}
