package license

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// newTestKey 生成测试专用密钥对。
// 每个测试独立生成，避免依赖外部文件导致测试不可重跑。
func newTestKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("生成测试密钥失败: %v", err)
	}
	return key
}

// issueTestLicense 用测试私钥签发一份授权内容。
func issueTestLicense(t *testing.T, key *rsa.PrivateKey, d Data) []byte {
	t.Helper()
	content, err := Issue(key, d)
	if err != nil {
		t.Fatalf("签发测试授权失败: %v", err)
	}
	return content
}

// newTestManager 构造一个指向临时目录、已加载测试公钥的管理器。
func newTestManager(t *testing.T, key *rsa.PrivateKey) (*Manager, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "license.lic")
	m := &Manager{
		path:      path,
		clockPath: filepath.Join(dir, "license-clock.dat"),
		enforce:   true,
		refresh:   DefaultRefreshInterval,
		stopCh:    make(chan struct{}),
		pub:       &key.PublicKey,
	}
	return m, path
}

// TestVerifySignature_RejectsTamperedExpireTime 是整个授权体系最关键的一条防线：
// 客户把授权文件里的到期时间往后改，签名保持不变，验签必须失败。
// 这条测试守住"改日期就能白用"这个最常见的破解手法。
func TestVerifySignature_RejectsTamperedExpireTime(t *testing.T) {
	key := newTestKey(t)
	original := issueTestLicense(t, key, Data{
		Product:    "webb-api",
		ExpireTime: time.Now().UTC().Add(30 * 24 * time.Hour),
		GraceDays:  7,
	})

	// 确认原始授权是有效的，否则后面的断言没有意义。
	var licFile File
	if err := json.Unmarshal(original, &licFile); err != nil {
		t.Fatalf("解析原始授权文件失败: %v", err)
	}
	origJSON, err := base64.StdEncoding.DecodeString(licFile.Data)
	if err != nil {
		t.Fatalf("解码原始授权内容失败: %v", err)
	}
	var origData Data
	if err := json.Unmarshal(origJSON, &origData); err != nil {
		t.Fatalf("解析原始授权数据失败: %v", err)
	}
	if err := VerifySignature(&key.PublicKey, origJSON, mustDecode(t, licFile.Sign)); err != nil {
		t.Fatalf("原始授权签名应当有效，却校验失败: %v", err)
	}

	// 篡改：把到期时间改成 2099 年，签名原样保留。
	var tampered Data
	if err := json.Unmarshal(origJSON, &tampered); err != nil {
		t.Fatalf("解析授权数据失败: %v", err)
	}
	tampered.ExpireTime = time.Date(2099, 12, 31, 23, 59, 59, 0, time.UTC)
	tamperedJSON, err := json.Marshal(tampered)
	if err != nil {
		t.Fatalf("序列化篡改数据失败: %v", err)
	}

	err = VerifySignature(&key.PublicKey, tamperedJSON, mustDecode(t, licFile.Sign))
	if err == nil {
		t.Fatal("篡改到期时间后签名校验竟然通过了，授权体系失效")
	}
}

// TestVerifySignature_RejectsWrongKey 验证用另一对密钥签发的授权无法通过校验，
// 防止客户自己生成密钥对来伪造授权。
func TestVerifySignature_RejectsWrongKey(t *testing.T) {
	attackerKey := newTestKey(t)
	victimKey := newTestKey(t)

	forged := issueTestLicense(t, attackerKey, Data{
		Product:    "webb-api",
		ExpireTime: time.Now().UTC().Add(3650 * 24 * time.Hour),
	})

	var licFile File
	if err := json.Unmarshal(forged, &licFile); err != nil {
		t.Fatalf("解析伪造授权失败: %v", err)
	}
	dataJSON, err := base64.StdEncoding.DecodeString(licFile.Data)
	if err != nil {
		t.Fatalf("解码伪造授权内容失败: %v", err)
	}

	if err := VerifySignature(&victimKey.PublicKey, dataJSON, mustDecode(t, licFile.Sign)); err == nil {
		t.Fatal("用攻击者私钥签发的授权竟然通过了受害者公钥校验")
	}
}

// TestEvaluateBytes_ExpiredAndGrace 覆盖到期判断的三种情形：
// 有效、到期但在宽限期内、到期且宽限期结束。
// 宽限期是为了避免客户忘记续费导致业务瞬间中断，必须按预期工作。
func TestEvaluateBytes_ExpiredAndGrace(t *testing.T) {
	key := newTestKey(t)
	m, _ := newTestManager(t, key)
	now := time.Now().UTC()

	cases := []struct {
		name        string
		expireIn    time.Duration
		graceDays   int
		wantValid   bool
		wantInGrace bool
		wantCode    string
	}{
		{
			name:      "尚未到期，授权有效",
			expireIn:  30 * 24 * time.Hour,
			graceDays: 7,
			wantValid: true,
			wantCode:  CodeOK,
		},
		{
			name:        "已到期但在宽限期内，仍可用并提示续期",
			expireIn:    -2 * 24 * time.Hour,
			graceDays:   7,
			wantValid:   true,
			wantInGrace: true,
			wantCode:    CodeOK,
		},
		{
			name:      "已到期且宽限期结束，停用",
			expireIn:  -10 * 24 * time.Hour,
			graceDays: 7,
			wantValid: false,
			wantCode:  CodeExpired,
		},
		{
			name:      "已到期且未设宽限期，立即停用",
			expireIn:  -1 * time.Hour,
			graceDays: 0,
			wantValid: false,
			wantCode:  CodeExpired,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			content := issueTestLicense(t, key, Data{
				Product:    "webb-api",
				ExpireTime: now.Add(tc.expireIn),
				GraceDays:  tc.graceDays,
			})

			// 传 checkClock=false，本测试只关心到期判断，不测时间回拨。
			got := m.evaluateBytes(content, now, false)

			if got.Valid != tc.wantValid {
				t.Errorf("Valid = %v，期望 %v（code=%s, message=%s）", got.Valid, tc.wantValid, got.Code, got.Message)
			}
			if got.Code != tc.wantCode {
				t.Errorf("Code = %q，期望 %q", got.Code, tc.wantCode)
			}
			if got.InGrace != tc.wantInGrace {
				t.Errorf("InGrace = %v，期望 %v", got.InGrace, tc.wantInGrace)
			}
		})
	}
}

// TestEvaluateBytes_MissingFile 验证授权文件不存在时返回明确的状态码，
// 而不是崩溃或误判为有效——首次部署时客户还没上传授权，这是必经路径。
func TestEvaluateBytes_MissingFile(t *testing.T) {
	key := newTestKey(t)
	m, _ := newTestManager(t, key)

	st := m.evaluateFile()
	if st.Valid {
		t.Error("授权文件不存在时不应判定为有效")
	}
	if st.Code != CodeMissing {
		t.Errorf("Code = %q，期望 %q", st.Code, CodeMissing)
	}
}

// TestEvaluateBytes_MalformedFile 验证乱写的内容不会被当成有效授权。
func TestEvaluateBytes_MalformedFile(t *testing.T) {
	key := newTestKey(t)
	m, _ := newTestManager(t, key)

	cases := []struct {
		name    string
		content []byte
	}{
		{name: "完全不是 JSON", content: []byte("这不是授权文件")},
		{name: "空 JSON 对象", content: []byte("{}")},
		{name: "字段为空字符串", content: []byte(`{"data":"","sign":""}`)},
		{name: "base64 非法", content: []byte(`{"data":"!!!not-base64!!!","sign":"abc"}`)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			st := m.evaluateBytes(tc.content, time.Now().UTC(), false)
			if st.Valid {
				t.Errorf("非法内容 %q 被判定为有效授权", tc.content)
			}
			if st.Code == CodeOK {
				t.Errorf("非法内容返回了 CodeOK，实际 code=%q", st.Code)
			}
		})
	}
}

// TestEvaluateBytes_ClockRollback 验证防时间回拨：
// 客户把服务器时间往回调，企图让已过期的授权重新生效，应当被拒绝。
func TestEvaluateBytes_ClockRollback(t *testing.T) {
	key := newTestKey(t)
	m, _ := newTestManager(t, key)

	base := time.Now().UTC()
	content := issueTestLicense(t, key, Data{
		Product:    "webb-api",
		ExpireTime: base.Add(30 * 24 * time.Hour),
		GraceDays:  0,
	})

	// 第一次校验：写入时间基准。
	if st := m.evaluateBytes(content, base, true); !st.Valid {
		t.Fatalf("首次校验应当有效，却失败: %s", st.Message)
	}

	// 把时间往回拨 10 天（远超容忍量），应判定为回拨。
	rolledBack := base.Add(-10 * 24 * time.Hour)
	st := m.evaluateBytes(content, rolledBack, true)
	if st.Valid {
		t.Error("系统时间被回拨后授权仍判定为有效，防回拨失效")
	}
	if st.Code != CodeClockRollback {
		t.Errorf("Code = %q，期望 %q", st.Code, CodeClockRollback)
	}

	// 正常时间前进（在容忍量内）不应误判，否则 NTP 校时会把客户误锁。
	normal := base.Add(2 * time.Minute)
	if st := m.evaluateBytes(content, normal, true); !st.Valid {
		t.Errorf("正常时间前进被误判为回拨: %s", st.Message)
	}
}

// TestClockFile_TamperResistance 验证时间基准文件被手工篡改后不会被采信。
// 如果这个文件可以随意编辑，客户就能通过删改它绕过时间回拨检测。
func TestClockFile_TamperResistance(t *testing.T) {
	key := newTestKey(t)
	m, _ := newTestManager(t, key)

	now := time.Now().UTC()
	m.writeClock(now)

	if last, ok := m.readClock(); !ok {
		t.Fatal("刚写入的时间基准应当可读")
	} else if last.Unix() != now.Unix() {
		t.Errorf("读回的时间 %v 与写入的 %v 不一致", last, now)
	}

	// 手工改成一个更早的时间戳，HMAC 必然不匹配。
	if err := os.WriteFile(m.clockPath, []byte("1000000000|deadbeef"), 0o600); err != nil {
		t.Fatalf("写入篡改内容失败: %v", err)
	}
	if _, ok := m.readClock(); ok {
		t.Error("被篡改的时间基准文件仍被采信，防回拨可被绕过")
	}

	// 文件损坏时应按"无历史基准"处理，不能因此让授权失效。
	if err := os.WriteFile(m.clockPath, []byte("garbage"), 0o600); err != nil {
		t.Fatalf("写入损坏内容失败: %v", err)
	}
	if _, ok := m.readClock(); ok {
		t.Error("损坏的时间基准文件被当成有效记录")
	}
}

// TestInstall_RejectsInvalidAndKeepsExisting 验证上传流程的安全性：
// 坏文件不得覆盖已有的有效授权，否则客户传错一次就被锁在系统外。
func TestInstall_RejectsInvalidAndKeepsExisting(t *testing.T) {
	key := newTestKey(t)
	m, path := newTestManager(t, key)

	good := issueTestLicense(t, key, Data{
		Product:    "webb-api",
		ExpireTime: time.Now().UTC().Add(30 * 24 * time.Hour),
		GraceDays:  7,
	})
	if err := os.WriteFile(path, good, 0o600); err != nil {
		t.Fatalf("写入初始授权失败: %v", err)
	}

	bad := []byte(`{"data":"bm90LWEtbGljZW5zZQ==","sign":"aW52YWxpZA=="}`)
	if _, err := m.Install(bad); err == nil {
		t.Fatal("安装非法授权文件竟然成功了")
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取授权文件失败: %v", err)
	}
	if string(after) != string(good) {
		t.Error("安装失败后原有有效授权被破坏，客户会被锁在系统外")
	}

	// 合法的新授权应当能成功覆盖。
	newer := issueTestLicense(t, key, Data{
		Product:    "webb-api",
		ExpireTime: time.Now().UTC().Add(365 * 24 * time.Hour),
		GraceDays:  7,
	})
	st, err := m.Install(newer)
	if err != nil {
		t.Fatalf("安装合法授权失败: %v", err)
	}
	if !st.Valid {
		t.Errorf("安装合法授权后状态应为有效，实际: %s", st.Message)
	}
	if st.License == nil {
		t.Error("安装成功后应返回解析出的授权信息")
	}
	if _, err := os.Stat(path + ".bak"); err != nil {
		t.Error("覆盖授权前应当生成 .bak 备份，便于误操作后回退")
	}
}

// TestParseKeys_RoundTrip 验证密钥导出与解析互逆，
// 这是 genkey 命令产出能直接用于签发/验签的前提。
func TestParseKeys_RoundTrip(t *testing.T) {
	key := newTestKey(t)

	privPEM := EncodePrivateKeyPEM(key)
	parsedPriv, err := ParsePrivateKeyPEM(string(privPEM))
	if err != nil {
		t.Fatalf("解析导出的私钥失败: %v", err)
	}
	if parsedPriv.D.Cmp(key.D) != 0 {
		t.Error("私钥导出后解析结果与原始私钥不一致")
	}

	pubPEM, err := EncodePublicKeyPEM(&key.PublicKey)
	if err != nil {
		t.Fatalf("导出公钥失败: %v", err)
	}
	parsedPub, err := ParsePublicKeyPEM(string(pubPEM))
	if err != nil {
		t.Fatalf("解析导出的公钥失败: %v", err)
	}
	if parsedPub.N.Cmp(key.PublicKey.N) != 0 {
		t.Error("公钥导出后解析结果与原始公钥不一致")
	}
}

// TestParsePublicKeyPEM_RejectsInvalid 验证占位公钥和垃圾内容都会被明确拒绝，
// 避免"忘记替换公钥却编译成功"导致所有客户授权都失效。
func TestParsePublicKeyPEM_RejectsInvalid(t *testing.T) {
	cases := []struct {
		name string
		pem  string
	}{
		{name: "空字符串", pem: ""},
		{name: "普通文本", pem: "not a pem"},
		{name: "未替换的占位内容", pem: publicKeyPEM},
		{name: "PEM 头尾但内容非法", pem: "-----BEGIN PUBLIC KEY-----\naGVsbG8=\n-----END PUBLIC KEY-----"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ParsePublicKeyPEM(tc.pem); err == nil {
				t.Errorf("非法公钥 %q 竟然解析成功", tc.name)
			}
		})
	}
}

// TestIssue_NormalizesFields 验证签发时会补全默认值并统一为 UTC 秒精度，
// 保证同一份授权在不同机器上签出的字节一致，便于比对排查。
func TestIssue_NormalizesFields(t *testing.T) {
	key := newTestKey(t)

	// 故意用非 UTC 时区、带亚秒精度的时间签发。
	loc := time.FixedZone("CST", 8*3600)
	expire := time.Date(2030, 5, 20, 18, 30, 45, 123456789, loc)

	content, err := Issue(key, Data{ExpireTime: expire})
	if err != nil {
		t.Fatalf("签发失败: %v", err)
	}

	var licFile File
	if err := json.Unmarshal(content, &licFile); err != nil {
		t.Fatalf("解析签发结果失败: %v", err)
	}
	dataJSON, err := base64.StdEncoding.DecodeString(licFile.Data)
	if err != nil {
		t.Fatalf("解码授权内容失败: %v", err)
	}
	var data Data
	if err := json.Unmarshal(dataJSON, &data); err != nil {
		t.Fatalf("解析授权数据失败: %v", err)
	}

	if data.Product == "" {
		t.Error("未指定产品名时应填充默认值")
	}
	if data.IssuedAt.IsZero() {
		t.Error("未指定签发时间时应自动填充")
	}
	if data.ExpireTime.Nanosecond() != 0 {
		t.Errorf("到期时间应截断到秒，实际保留 %d 纳秒", data.ExpireTime.Nanosecond())
	}
	// 绝对时刻必须一致：18:30:45 +08:00 == 10:30:45 UTC。
	if !data.ExpireTime.Equal(expire) {
		t.Errorf("到期时刻被改变了: 期望 %v，实际 %v", expire.UTC(), data.ExpireTime)
	}
	if data.ExpireTime.Location() != time.UTC {
		t.Errorf("到期时间应统一为 UTC，实际时区 %v", data.ExpireTime.Location())
	}
}

// TestIssue_RejectsMissingExpire 验证缺少到期时间时拒绝签发，
// 防止签出一份"永不过期"或"立即过期"的意外授权。
func TestIssue_RejectsMissingExpire(t *testing.T) {
	key := newTestKey(t)
	if _, err := Issue(key, Data{Product: "webb-api"}); err == nil {
		t.Error("缺少到期时间时不应签发成功")
	}
	if _, err := Issue(nil, Data{ExpireTime: time.Now()}); err == nil {
		t.Error("私钥为空时不应签发成功")
	}
}

// TestEvaluateBytes_ExpiryIsTimezoneIndependent 验证到期判断不受服务器时区影响。
// 客户把系统时区从东八区改到西十二区，不应让已过期授权重新生效。
func TestEvaluateBytes_ExpiryIsTimezoneIndependent(t *testing.T) {
	key := newTestKey(t)
	m, _ := newTestManager(t, key)

	// 授权在"1 小时前"到期，用 UTC 表述。
	expire := time.Now().UTC().Add(-1 * time.Hour)
	content := issueTestLicense(t, key, Data{
		Product:    "webb-api",
		ExpireTime: expire,
		GraceDays:  0,
	})

	// 用不同时区的"同一绝对时刻"来校验，结论必须一致。
	zones := []struct {
		name string
		loc  *time.Location
	}{
		{name: "UTC", loc: time.UTC},
		{name: "东八区", loc: time.FixedZone("CST", 8*3600)},
		{name: "西十二区", loc: time.FixedZone("UTC-12", -12*3600)},
	}

	for _, z := range zones {
		t.Run(z.name, func(t *testing.T) {
			// nowInZone 与 time.Now().UTC() 是同一绝对时刻，只是时区表示不同。
			nowInZone := time.Now().UTC().In(z.loc)
			st := m.evaluateBytes(content, nowInZone, false)
			if st.Valid {
				t.Errorf("在 %s 时区下已过期授权被判定为有效，说明到期判断依赖了时区", z.name)
			}
			if st.Code != CodeExpired {
				t.Errorf("Code = %q，期望 %q", st.Code, CodeExpired)
			}
		})
	}
}

// TestStateMessage_NotEmpty 验证所有失败状态都带有人类可读的提示信息，
// 前端要直接把这句话展示给客户，不能出现空白错误。
func TestStateMessage_NotEmpty(t *testing.T) {
	key := newTestKey(t)
	m, _ := newTestManager(t, key)
	now := time.Now().UTC()

	invalid := [][]byte{
		[]byte("乱码"),
		[]byte(`{"data":"","sign":""}`),
	}
	for _, content := range invalid {
		st := m.evaluateBytes(content, now, false)
		if st.Valid {
			continue
		}
		if strings.TrimSpace(st.Message) == "" {
			t.Errorf("状态码 %q 缺少提示信息，前端将展示空白错误", st.Code)
		}
	}

	expired := issueTestLicense(t, key, Data{
		Product:    "webb-api",
		ExpireTime: now.Add(-30 * 24 * time.Hour),
		GraceDays:  0,
	})
	st := m.evaluateBytes(expired, now, false)
	if strings.TrimSpace(st.Message) == "" {
		t.Error("过期状态缺少提示信息")
	}
	if !strings.Contains(st.Message, "到期") {
		t.Errorf("过期提示应说明已到期，实际: %q", st.Message)
	}
}

func mustDecode(t *testing.T, s string) []byte {
	t.Helper()
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		t.Fatalf("base64 解码失败: %v", err)
	}
	return b
}
