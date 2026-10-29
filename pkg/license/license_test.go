package license

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// 测试辅助
// ---------------------------------------------------------------------------

func newTestKey(t *testing.T) (*rsa.PrivateKey, string) {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("生成测试密钥失败: %v", err)
	}
	der, err := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	if err != nil {
		t.Fatalf("导出公钥失败: %v", err)
	}
	pubPEM := string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
	return priv, pubPEM
}

func signEnvelope(t *testing.T, priv *rsa.PrivateKey, payload map[string]any) []byte {
	t.Helper()
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("序列化载荷失败: %v", err)
	}
	sum := sha256.Sum256(payloadBytes)
	sig, err := rsa.SignPKCS1v15(rand.Reader, priv, crypto.SHA256, sum[:])
	if err != nil {
		t.Fatalf("签名失败: %v", err)
	}
	env := Envelope{
		Data: base64.StdEncoding.EncodeToString(payloadBytes),
		Sign: base64.StdEncoding.EncodeToString(sig),
	}
	raw, err := json.MarshalIndent(env, "", "  ")
	if err != nil {
		t.Fatalf("序列化信封失败: %v", err)
	}
	return raw
}

func v1Payload(expire *time.Time, grace int, machine string) map[string]any {
	issued := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	p := map[string]any{
		"schema_version":  1,
		"license_id":      "a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6",
		"platform":        PlatformID,
		"product":         Product,
		"product_display": "webb-api",
		"customer":        "测试客户",
		"issued_at":       issued.Format(time.RFC3339),
		"expire_time":     nil,
		"grace_days":      grace,
		"machine_code":    machine,
		"instance_id":     "",
		"features":        map[string]any{"max_users": 10},
	}
	if expire != nil {
		p["expire_time"] = expire.UTC().Format(time.RFC3339)
	}
	return p
}

func newTestManager(t *testing.T, pubPEM string) (*Manager, string) {
	t.Helper()
	dir := t.TempDir()
	m := &Manager{
		pub:       mustParse(t, pubPEM),
		path:      filepath.Join(dir, "license.lic"),
		clockPath: filepath.Join(dir, licenseClockFileName),
		enforce:   true,
		refresh:   time.Hour,
		stopCh:    make(chan struct{}),
	}
	return m, dir
}

func mustParse(t *testing.T, pubPEM string) *rsa.PublicKey {
	t.Helper()
	pub, err := ParsePublicKeyPEM(pubPEM)
	if err != nil {
		t.Fatalf("解析公钥失败: %v", err)
	}
	return pub
}

func fixedNow() time.Time { return time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC) }

// ---------------------------------------------------------------------------
// 验签与业务字段
// ---------------------------------------------------------------------------

func TestEvaluate_ValidLicense(t *testing.T) {
	t.Setenv("LICENSE_MACHINE_CODE", "00112233445566778899aabbccddeeff")
	priv, pubPEM := newTestKey(t)
	m, _ := newTestManager(t, pubPEM)

	expire := fixedNow().Add(30 * 24 * time.Hour)
	raw := signEnvelope(t, priv, v1Payload(&expire, 7, "*"))

	st := m.evaluateBytes(raw, fixedNow(), false)
	if !st.Valid || st.Code != CodeOK {
		t.Fatalf("期望 ok/valid，实际 code=%s valid=%v msg=%s", st.Code, st.Valid, st.Message)
	}
	if st.License == nil || st.License.Customer != "测试客户" {
		t.Fatalf("载荷解析错误: %+v", st.License)
	}
	if st.RemainingDays <= 0 {
		t.Fatalf("剩余天数应大于 0，实际 %d", st.RemainingDays)
	}
}

func TestEvaluate_PermanentLicense(t *testing.T) {
	t.Setenv("LICENSE_MACHINE_CODE", "00112233445566778899aabbccddeeff")
	priv, pubPEM := newTestKey(t)
	m, _ := newTestManager(t, pubPEM)

	st := m.evaluateBytes(signEnvelope(t, priv, v1Payload(nil, 0, "*")), fixedNow(), false)
	if !st.Valid || st.Code != CodeOK || st.RemainingDays != -1 {
		t.Fatalf("永久授权应 valid/ok/-1，实际 code=%s valid=%v days=%d", st.Code, st.Valid, st.RemainingDays)
	}
}

func TestEvaluate_GracePeriod(t *testing.T) {
	t.Setenv("LICENSE_MACHINE_CODE", "00112233445566778899aabbccddeeff")
	priv, pubPEM := newTestKey(t)
	m, _ := newTestManager(t, pubPEM)

	expire := fixedNow().Add(-2 * 24 * time.Hour)
	st := m.evaluateBytes(signEnvelope(t, priv, v1Payload(&expire, 7, "*")), fixedNow(), false)
	if !st.Valid || st.Code != CodeGrace || !st.InGrace {
		t.Fatalf("宽限期应 valid/license_grace，实际 code=%s valid=%v grace=%v", st.Code, st.Valid, st.InGrace)
	}
}

func TestEvaluate_Expired(t *testing.T) {
	t.Setenv("LICENSE_MACHINE_CODE", "00112233445566778899aabbccddeeff")
	priv, pubPEM := newTestKey(t)
	m, _ := newTestManager(t, pubPEM)

	expire := fixedNow().Add(-10 * 24 * time.Hour)
	st := m.evaluateBytes(signEnvelope(t, priv, v1Payload(&expire, 7, "*")), fixedNow(), false)
	if st.Valid || st.Code != CodeExpired {
		t.Fatalf("过期应 invalid/license_expired，实际 code=%s valid=%v", st.Code, st.Valid)
	}
}

func TestEvaluate_TamperedPayloadRejected(t *testing.T) {
	t.Setenv("LICENSE_MACHINE_CODE", "00112233445566778899aabbccddeeff")
	priv, pubPEM := newTestKey(t)
	m, _ := newTestManager(t, pubPEM)

	expire := fixedNow().Add(24 * time.Hour)
	raw := signEnvelope(t, priv, v1Payload(&expire, 0, "*"))
	// 修改 data（即使改成另一个合法 JSON）必须验签失败。
	var env Envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatal(err)
	}
	payloadBytes, _ := base64.StdEncoding.DecodeString(env.Data)
	tampered := strings.Replace(string(payloadBytes), `"customer":"测试客户"`, `"customer":"黑客客户"`, 1)
	env.Data = base64.StdEncoding.EncodeToString([]byte(tampered))
	raw, _ = json.Marshal(env)

	st := m.evaluateBytes(raw, fixedNow(), false)
	if st.Valid || st.Code != CodeBadSignature {
		t.Fatalf("篡改应 license_bad_signature，实际 code=%s valid=%v", st.Code, st.Valid)
	}
}

func TestEvaluate_WrongProductRejected(t *testing.T) {
	t.Setenv("LICENSE_MACHINE_CODE", "00112233445566778899aabbccddeeff")
	priv, pubPEM := newTestKey(t)
	m, _ := newTestManager(t, pubPEM)

	payload := v1Payload(nil, 0, "*")
	payload["product"] = "another-product"
	st := m.evaluateBytes(signEnvelope(t, priv, payload), fixedNow(), false)
	if st.Valid || st.Code != CodeProductMismatch {
		t.Fatalf("产品不符应 license_product_mismatch，实际 code=%s", st.Code)
	}
}

func TestEvaluate_MachineMismatch(t *testing.T) {
	t.Setenv("LICENSE_MACHINE_CODE", "00112233445566778899aabbccddeeff")
	priv, pubPEM := newTestKey(t)
	m, _ := newTestManager(t, pubPEM)

	st := m.evaluateBytes(
		signEnvelope(t, priv, v1Payload(nil, 0, "ffffffffffffffffffffffffffffffff")), fixedNow(), false)
	if st.Valid || st.Code != CodeMachineMismatch {
		t.Fatalf("机器码不符应 license_machine_mismatch，实际 code=%s", st.Code)
	}
}

func TestEvaluate_EmptyMachineCodeIsMalformed(t *testing.T) {
	priv, pubPEM := newTestKey(t)
	m, _ := newTestManager(t, pubPEM)

	st := m.evaluateBytes(signEnvelope(t, priv, v1Payload(nil, 0, "")), fixedNow(), false)
	if st.Valid || st.Code != CodeMalformed {
		t.Fatalf("空 machine_code 应 license_malformed，实际 code=%s", st.Code)
	}
}

func TestEvaluate_MissingMachineCodeFieldIsMalformed(t *testing.T) {
	priv, pubPEM := newTestKey(t)
	m, _ := newTestManager(t, pubPEM)

	payload := v1Payload(nil, 0, "*")
	delete(payload, "machine_code")
	st := m.evaluateBytes(signEnvelope(t, priv, payload), fixedNow(), false)
	if st.Valid || st.Code != CodeMalformed {
		t.Fatalf("缺失 machine_code 应 license_malformed，实际 code=%s", st.Code)
	}
}

func TestEvaluate_UnsupportedSchemaVersionRejected(t *testing.T) {
	priv, pubPEM := newTestKey(t)
	m, _ := newTestManager(t, pubPEM)

	payload := v1Payload(nil, 0, "*")
	payload["schema_version"] = 2
	st := m.evaluateBytes(signEnvelope(t, priv, payload), fixedNow(), false)
	if st.Valid || st.Code != CodeMalformed {
		t.Fatalf("未知 schema_version 应拒绝，实际 code=%s", st.Code)
	}
}

func TestEvaluate_LegacyPayloadWithoutSchemaVersion(t *testing.T) {
	t.Setenv("LICENSE_MACHINE_CODE", "00112233445566778899aabbccddeeff")
	priv, pubPEM := newTestKey(t)
	m, _ := newTestManager(t, pubPEM)

	// 旧版格式：无 schema_version，空 machine_code 表示通用授权。
	legacy := map[string]any{
		"product":      Product,
		"customer":     "旧客户",
		"expire_time":  fixedNow().Add(48 * time.Hour).Format(time.RFC3339),
		"issued_at":    fixedNow().Format(time.RFC3339),
		"machine_code": "",
	}
	st := m.evaluateBytes(signEnvelope(t, priv, legacy), fixedNow(), false)
	if !st.Valid || st.Code != CodeOK {
		t.Fatalf("旧版通用授权应按兼容规则放行，实际 code=%s valid=%v", st.Code, st.Valid)
	}
	if st.License == nil || !st.License.Legacy || st.License.MachineCode != "*" {
		t.Fatalf("旧版载荷兼容标记错误: %+v", st.License)
	}
}

func TestEvaluate_MalformedEnvelope(t *testing.T) {
	priv, pubPEM := newTestKey(t)
	m, _ := newTestManager(t, pubPEM)
	_ = priv

	for name, raw := range map[string][]byte{
		"非法 JSON":       []byte("not-json"),
		"缺少 data":       []byte(`{"sign":"abc"}`),
		"缺少 sign":       []byte(`{"data":"abc"}`),
		"data 非 base64": []byte(`{"data":"!!!!","sign":"abc"}`),
	} {
		st := m.evaluateBytes(raw, fixedNow(), false)
		if st.Valid || st.Code != CodeMalformed {
			t.Fatalf("%s 应 license_malformed，实际 code=%s", name, st.Code)
		}
	}
}

// ---------------------------------------------------------------------------
// 时间回拨
// ---------------------------------------------------------------------------

func TestClock_RollbackDetected(t *testing.T) {
	t.Setenv("LICENSE_MACHINE_CODE", "00112233445566778899aabbccddeeff")
	priv, pubPEM := newTestKey(t)
	m, _ := newTestManager(t, pubPEM)

	raw := signEnvelope(t, priv, v1Payload(nil, 0, "*"))
	base := fixedNow()
	if st := m.evaluateBytes(raw, base, true); !st.Valid {
		t.Fatalf("首次校验应通过: %s", st.Code)
	}
	// 把时间往回调 2 小时（超过 10 分钟容差）→ 回拨。
	st := m.evaluateBytes(raw, base.Add(-2*time.Hour), true)
	if st.Valid || st.Code != CodeClockRollback {
		t.Fatalf("时间回拨应 license_clock_rollback，实际 code=%s", st.Code)
	}
	// 容差内的时钟抖动不误判。
	st = m.evaluateBytes(raw, base.Add(-time.Minute), true)
	if !st.Valid {
		t.Fatalf("容差内抖动不应判回拨，实际 code=%s", st.Code)
	}
}

func TestClock_TamperedCheckpointTreatedAsNoBaseline(t *testing.T) {
	t.Setenv("LICENSE_MACHINE_CODE", "00112233445566778899aabbccddeeff")
	priv, pubPEM := newTestKey(t)
	m, _ := newTestManager(t, pubPEM)

	raw := signEnvelope(t, priv, v1Payload(nil, 0, "*"))
	if st := m.evaluateBytes(raw, fixedNow(), true); !st.Valid {
		t.Fatalf("首次校验应通过: %s", st.Code)
	}
	// 伪造检查点为未来时间，但 HMAC 不匹配 → 视为无基准，不误判回拨。
	future := fixedNow().Add(365 * 24 * time.Hour).Unix()
	if err := os.WriteFile(m.clockPath, []byte(strconv.FormatInt(future, 10)+"|deadbeef"), 0o600); err != nil {
		t.Fatal(err)
	}
	st := m.evaluateBytes(raw, fixedNow(), true)
	if !st.Valid {
		t.Fatalf("检查点被篡改应按无基准处理，实际 code=%s", st.Code)
	}
}

// ---------------------------------------------------------------------------
// 安装（上传）不变量
// ---------------------------------------------------------------------------

func TestInstall_DoesNotOverwriteWithInvalidFile(t *testing.T) {
	t.Setenv("LICENSE_MACHINE_CODE", "00112233445566778899aabbccddeeff")
	priv, pubPEM := newTestKey(t)
	m, _ := newTestManager(t, pubPEM)

	good := signEnvelope(t, priv, v1Payload(nil, 0, "*"))
	if err := os.WriteFile(m.path, good, 0o600); err != nil {
		t.Fatal(err)
	}

	bad := []byte(`{"data":"not-base64","sign":"x"}`)
	if _, err := m.Install(bad); err == nil {
		t.Fatal("坏文件应安装失败")
	}
	onDisk, err := os.ReadFile(m.path)
	if err != nil {
		t.Fatal(err)
	}
	if string(onDisk) != string(good) {
		t.Fatal("坏文件不得覆盖原有有效授权")
	}
	if _, err := os.Stat(m.path + ".bak"); err == nil {
		t.Fatal("校验失败时不应产生 .bak 备份")
	}
}

func TestInstall_BacksUpAndReplacesAtomically(t *testing.T) {
	t.Setenv("LICENSE_MACHINE_CODE", "00112233445566778899aabbccddeeff")
	priv, pubPEM := newTestKey(t)
	m, _ := newTestManager(t, pubPEM)

	old := signEnvelope(t, priv, v1Payload(nil, 0, "*"))
	if err := os.WriteFile(m.path, old, 0o600); err != nil {
		t.Fatal(err)
	}
	newLicense := signEnvelope(t, priv, v1Payload(nil, 0, "*"))
	if _, err := m.Install(newLicense); err != nil {
		t.Fatalf("安装失败: %v", err)
	}
	if _, err := os.Stat(m.path + ".bak"); err != nil {
		t.Fatalf("应生成 .bak 备份: %v", err)
	}
	onDisk, _ := os.ReadFile(m.path)
	if string(onDisk) != string(newLicense) {
		t.Fatal("新授权未原子替换")
	}
	if _, err := os.Stat(m.path + ".tmp"); !os.IsNotExist(err) {
		t.Fatal("临时文件应已被重命名")
	}
}

// ---------------------------------------------------------------------------
// 机器码与公钥
// ---------------------------------------------------------------------------

func TestMachineCode_FormatAndOverride(t *testing.T) {
	t.Setenv("LICENSE_MACHINE_CODE", "00112233445566778899aabbccddeeff")
	if got := MachineCode(); got != "00112233445566778899aabbccddeeff" {
		t.Fatalf("环境变量覆盖失败: %s", got)
	}

	t.Setenv("LICENSE_MACHINE_CODE", "")
	t.Setenv("HOSTNAME", "")
	code := MachineCode()
	if len(code) != 32 {
		t.Fatalf("机器码应为 32 位 hex，实际 %q", code)
	}
	for _, r := range code {
		if !strings.ContainsRune("0123456789abcdef", r) {
			t.Fatalf("机器码应为小写 hex，实际 %q", code)
		}
	}
}

func TestParsePublicKey_RejectsPlaceholderAndInvalid(t *testing.T) {
	if _, err := ParsePublicKeyPEM("REPLACE_WITH_YOUR_PUBLIC_KEY"); err == nil {
		t.Fatal("占位符应解析失败")
	}
	if _, err := ParsePublicKeyPEM("not pem"); err == nil {
		t.Fatal("非 PEM 内容应解析失败")
	}
}

func TestParsePublicKey_AcceptsPKCS1(t *testing.T) {
	priv, _ := newTestKey(t)
	der := x509.MarshalPKCS1PublicKey(&priv.PublicKey)
	pemStr := string(pem.EncodeToMemory(&pem.Block{Type: "RSA PUBLIC KEY", Bytes: der}))
	if _, err := ParsePublicKeyPEM(pemStr); err != nil {
		t.Fatalf("PKCS#1 公钥应被接受: %v", err)
	}
}

// ---------------------------------------------------------------------------
// 到期判定与"到期即失效"回归测试
// ---------------------------------------------------------------------------

// TestExpiryState_Boundary 确认到期时刻本身即失效（remaining == 0 不能再放行）。
func TestExpiryState_Boundary(t *testing.T) {
	expire := fixedNow()
	payload := &Payload{
		ExpireTime:  &expire,
		GraceDays:   0,
		MachineCode: UniversalMachineCode,
	}

	if st := expiryState(payload, expire.Add(-time.Second), fixedNow()); !st.Valid || st.Code != CodeOK {
		t.Fatalf("到期前 1 秒应有效，实际 code=%s valid=%v", st.Code, st.Valid)
	}
	// remaining == 0 不能再算有效，否则正好卡在到期秒上仍能调用。
	if st := expiryState(payload, expire, fixedNow()); st.Valid || st.Code != CodeExpired {
		t.Fatalf("到达到期时刻应立即失效，实际 code=%s valid=%v", st.Code, st.Valid)
	}
	if st := expiryState(payload, expire.Add(time.Minute), fixedNow()); st.Valid || st.Code != CodeExpired {
		t.Fatalf("到期后应立即失效，实际 code=%s valid=%v", st.Code, st.Valid)
	}
}

// TestManagerCurrent_ExpiresWithoutReload 验证 Current() 会按当前时间复算缓存状态：
// 即使后台复检还没跑到，授权跨过到期时刻后也必须立即返回 license_expired。
func TestManagerCurrent_ExpiresWithoutReload(t *testing.T) {
	expire := time.Now().UTC().Add(time.Hour)
	cached := State{
		Code:      CodeOK,
		Message:   "授权有效",
		Valid:     true,
		CheckedAt: time.Now().UTC(),
		License: &Payload{
			ExpireTime:  &expire,
			MachineCode: UniversalMachineCode,
		},
	}
	m := &Manager{enforce: true}
	m.state.Store(&cached)

	if st := m.Current(); !st.Valid || st.Code != CodeOK {
		t.Fatalf("未到期时应保持有效，实际 code=%s valid=%v", st.Code, st.Valid)
	}

	// 把到期时间改到过去，模拟"时间已经走过 expire_time，但后台 ticker 尚未触发"。
	past := time.Now().UTC().Add(-time.Minute)
	cached.License.ExpireTime = &past
	m.state.Store(&cached)
	if st := m.Current(); st.Valid || st.Code != CodeExpired {
		t.Fatalf("到期后 Current() 必须立即失效，实际 code=%s valid=%v", st.Code, st.Valid)
	}
}

// TestManagerCurrent_ClockRollback 验证时钟回拨不必等后台复检就会立即失效。
func TestManagerCurrent_ClockRollback(t *testing.T) {
	expire := time.Now().UTC().Add(time.Hour)
	cached := State{
		Code:      CodeOK,
		Valid:     true,
		CheckedAt: time.Now().UTC(),
		License: &Payload{
			ExpireTime:  &expire,
			MachineCode: UniversalMachineCode,
		},
	}
	m := &Manager{enforce: true}
	m.state.Store(&cached)

	// 上次校验时间被"未来化"，相当于现在的时间被回拨。
	cached.CheckedAt = time.Now().UTC().Add(2 * time.Hour)
	m.state.Store(&cached)
	if st := m.Current(); st.Valid || st.Code != CodeClockRollback {
		t.Fatalf("时钟回拨应立即失效，实际 code=%s valid=%v", st.Code, st.Valid)
	}
}

// TestVerifyContent_MissingExpireTimeRejected 确认 v1 授权缺 expire_time 时按格式损坏处理，
// 不能被当成永久授权（否则该文件永远不过期）。
func TestVerifyContent_MissingExpireTimeRejected(t *testing.T) {
	t.Setenv("LICENSE_MACHINE_CODE", "00112233445566778899aabbccddeeff")
	priv, pubPEM := newTestKey(t)
	m, _ := newTestManager(t, pubPEM)

	payload := v1Payload(nil, 0, "*")
	delete(payload, "expire_time")
	st := m.evaluateBytes(signEnvelope(t, priv, payload), fixedNow(), false)
	if st.Valid || st.Code != CodeMalformed {
		t.Fatalf("缺少 expire_time 应判为 license_malformed，实际 code=%s valid=%v", st.Code, st.Valid)
	}

	// 显式 null 仍然表示永久授权。
	st = m.evaluateBytes(signEnvelope(t, priv, v1Payload(nil, 0, "*")), fixedNow(), false)
	if !st.Valid || st.Code != CodeOK {
		t.Fatalf("显式 null 应视为永久授权，实际 code=%s valid=%v", st.Code, st.Valid)
	}
}
