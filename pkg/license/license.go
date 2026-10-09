// Package license 提供离线授权文件（license.lic）的签名校验与全局授权状态管理。
//
// 授权文件由维护者用私钥在本地离线签发，程序内只内置公钥用于验签。
// 客户拿到的 .lic 文件即使被文本编辑器打开，也无法在不持有私钥的情况下
// 修改到期时间——任何改动都会导致 RSA 验签失败，授权立即失效。
//
// 本包只依赖标准库，不引入项目内其他包，避免循环依赖。
package license

import (
	"crypto"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// 授权状态码，前端可据此区分不同的失效原因并展示对应引导。
const (
	CodeOK            = "ok"                  // 授权有效
	CodeDisabled      = "license_disabled"    // 未启用强制校验（本地开发用）
	CodeNotInit       = "license_not_init"    // 授权模块未初始化，属于程序缺陷
	CodeMissing       = "license_missing"     // 找不到授权文件
	CodeMalformed     = "license_malformed"   // 文件结构或编码损坏
	CodeBadSignature  = "license_bad_sign"    // 签名校验失败，文件被篡改或非本方签发
	CodeExpired       = "license_expired"     // 已过期且宽限期结束
	CodeClockRollback = "license_clock_rollback" // 检测到系统时间被回拨
	CodeNoPublicKey   = "license_no_pubkey"   // 内置公钥未替换，无法验签
)

const (
	// DefaultLicensePath 是授权文件的默认位置（相对程序工作目录）。
	// Docker 部署时应通过环境变量 LICENSE_PATH 指向已挂载的数据卷，例如 /data/license.lic。
	DefaultLicensePath = "./license.lic"

	// DefaultRefreshInterval 是后台复检间隔。到期时间跨过零点、或运维手工替换了
	// 授权文件，都会在下一个复检周期内被感知，无需重启服务。
	DefaultRefreshInterval = 60 * time.Second

	// clockTolerance 是时间回拨检测的容忍量。留足余量是为了避免 NTP 校时、
	// 虚拟机时钟漂移造成误判；真正要拦的是"把系统时间往回调几个月"这种绕过行为。
	clockTolerance = 10 * time.Minute

	// defaultGraceDays 是签发授权时未指定宽限期天数所用的默认值。
	defaultGraceDays = 7

	// clockSecret 用于给时间回拨检测的落盘记录加 HMAC 校验，
	// 防止客户直接编辑该文件来伪造"上次检查时间"。
	clockSecret = "webb-api-license-clock-v1"
)

// publicKeyPEM 是内置验签公钥。
//
// 部署前必须替换：用 cmd/license-tool 的 genkey 子命令生成密钥对，
// 再把 public.key 的完整内容（含首尾 BEGIN/END 行）粘贴到这里，然后重新编译。
// 私钥只保存在维护者本地，绝不进入代码仓库、也绝不随程序交付给客户。
const publicKeyPEM = `-----BEGIN PUBLIC KEY-----
MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEAw6LGjIIJjYPSmeAdEtvx
sN4uN9GKDDj5KJAWZxpPmJsTYGlvX0T8DQWy+jDv1xBRnO14P2J/PrYR4wGc9VHO
3an3TH4t5ynWmrCFz69/iLhmPZ2F77fTjTZ8IodhkD0M8fqAeGEkbexnPMhA7mXo
KG9Scfkmwaw1+qEWY8mLYE1sjsginHw3ov737aeS5JKU0+oQgC2HIneZrISmPs8f
kQHtr2HS6+hw2S2iwI78C4NiEpRIryhoR3vJ06/BjcRdpnplYkF7cvG+UE4suAhw
M+o4xUysV2hjTTDzJxqEYF2LqrEPZFYmADjjHhzhJlfmiFtTt8ZX3mILHxXKJIde
0wIDAQAB
-----END PUBLIC KEY-----`

// publicKeyPlaceholder 用于判断内置公钥是否还没被替换。
const publicKeyPlaceholder = "REPLACE_WITH_YOUR_PUBLIC_KEY"

// Data 是一份授权的实际内容，签发端与校验端共用同一结构。
// 注意：这里的字段会被 JSON 序列化后整体签名，因此增删字段会让旧授权文件失效，
// 升级时需要考虑向后兼容。
type Data struct {
	Product    string    `json:"product"`               // 产品名，仅作展示与区分
	Customer   string    `json:"customer,omitempty"`    // 客户标识，便于追溯授权发给了谁
	IssuedAt   time.Time `json:"issued_at"`             // 签发时间（UTC）
	ExpireTime time.Time `json:"expire_time"`           // 到期时间（UTC 绝对时刻）
	GraceDays  int       `json:"grace_days"`            // 到期后的宽限天数，随授权一起签名
}

// File 是 license.lic 的落盘格式：base64 编码的原始数据 + base64 编码的 RSA 签名。
type File struct {
	Data string `json:"data"`
	Sign string `json:"sign"`
}

// State 是一次授权检查的结论，中间件与接口都读它。
type State struct {
	Code          string    `json:"code"`
	Message       string    `json:"message"`
	Valid         bool      `json:"valid"`
	InGrace       bool      `json:"in_grace"`
	RemainingDays int       `json:"remaining_days"`
	CheckedAt     time.Time `json:"checked_at"`
	License       *Data     `json:"license,omitempty"`
}

// Error 携带状态码的错误，便于调用方区分失败原因而不是靠字符串匹配。
type Error struct {
	Code string
	Msg  string
}

func (e *Error) Error() string { return e.Msg }

// Config 是 Init 的入参，零值表示全部走默认与环境变量。
type Config struct {
	Path      string        // 授权文件路径，留空则取 LICENSE_PATH，再退回 DefaultLicensePath
	PublicKey string        // 验签公钥 PEM，留空则用内置 publicKeyPEM
	Refresh   time.Duration // 后台复检间隔，留空则用 DefaultRefreshInterval
	Enforce   bool          // 是否强制校验，由 DefaultConfig 根据 LICENSE_ENFORCE 决定
}

// Manager 持有公钥与最新授权状态。状态用 atomic.Pointer 存放，
// 中间件读取时无需加锁，避免高并发转发场景下成为瓶颈。
type Manager struct {
	mu        sync.Mutex // 仅保护授权文件的写入（安装新授权）
	state     atomic.Pointer[State]
	pub       *rsa.PublicKey
	path      string
	clockPath string
	enforce   bool
	refresh   time.Duration
	stopCh    chan struct{}
	stopOnce  sync.Once
}

// defaultManager 是进程内唯一的授权管理器，在服务开始监听前完成初始化。
var defaultManager *Manager

// DefaultConfig 从环境变量推导默认配置。
//
//	LICENSE_PATH     授权文件路径，默认 ./license.lic
//	LICENSE_ENFORCE  设为 false/0/off/no 时关闭强制校验，仅供本地开发调试
func DefaultConfig() Config {
	return Config{
		Path:      resolvePath(),
		PublicKey: publicKeyPEM,
		Refresh:   DefaultRefreshInterval,
		Enforce:   resolveEnforce(),
	}
}

// Init 用默认配置初始化授权模块。
func Init() error {
	return InitWithConfig(DefaultConfig())
}

// InitWithConfig 初始化授权模块：解析公钥、做一次立即校验、启动后台复检。
// 返回错误时调用方应当终止启动——授权模块没起来就对外服务，等于没有授权保护。
func InitWithConfig(cfg Config) error {
	if defaultManager != nil {
		return errors.New("授权模块已初始化，不能重复初始化")
	}
	if cfg.Path == "" {
		cfg.Path = resolvePath()
	}
	if cfg.PublicKey == "" {
		cfg.PublicKey = publicKeyPEM
	}
	if cfg.Refresh <= 0 {
		cfg.Refresh = DefaultRefreshInterval
	}

	absPath, err := filepath.Abs(cfg.Path)
	if err != nil {
		return fmt.Errorf("解析授权文件路径失败: %w", err)
	}

	m := &Manager{
		path:      absPath,
		clockPath: filepath.Join(filepath.Dir(absPath), "license-clock.dat"),
		enforce:   cfg.Enforce,
		refresh:   cfg.Refresh,
		stopCh:    make(chan struct{}),
	}

	if m.enforce {
		if strings.Contains(cfg.PublicKey, publicKeyPlaceholder) {
			return fmt.Errorf("内置公钥尚未替换：请用 cmd/license-tool genkey 生成密钥对，并把 public.key 内容填入 pkg/license/license.go 的 publicKeyPEM（状态码 %s）", CodeNoPublicKey)
		}
		pub, err := ParsePublicKeyPEM(cfg.PublicKey)
		if err != nil {
			return fmt.Errorf("加载内置公钥失败: %w", err)
		}
		m.pub = pub
	}

	// 确保授权文件所在目录存在，否则客户上传授权时会因目录缺失而失败。
	if dir := filepath.Dir(absPath); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("创建授权目录 %s 失败: %w", dir, err)
		}
	}

	m.Reload()
	defaultManager = m
	m.startLoop()
	return nil
}

// Current 返回最近一次校验得到的授权状态，读取无锁、可安全用于每个请求。
func Current() State {
	m := defaultManager
	if m == nil {
		return State{
			Code:      CodeNotInit,
			Message:   "授权模块尚未初始化",
			Valid:     false,
			CheckedAt: time.Now().UTC(),
		}
	}
	if st := m.state.Load(); st != nil {
		return *st
	}
	return m.Reload()
}

// Enforced 报告当前是否启用了强制校验。
func Enforced() bool {
	m := defaultManager
	return m != nil && m.enforce
}

// Path 返回授权文件的绝对路径，便于运维确认文件落在哪里。
func Path() string {
	if defaultManager == nil {
		return ""
	}
	return defaultManager.path
}

// Reload 重新读取并校验授权文件，返回最新状态。
// 上传新授权、或希望立即生效（不等后台复检）时调用。
func Reload() State {
	if defaultManager == nil {
		return Current()
	}
	return defaultManager.Reload()
}

// Reload 是 Manager 上的重载实现。
func (m *Manager) Reload() State {
	st := m.evaluateFile()
	m.state.Store(&st)
	return st
}

// ValidateContent 只校验一段授权内容是否合法，不读写授权文件、不更新时间回拨记录。
// 上传接口用它做"先验证后落盘"，避免用一个坏文件覆盖掉当前仍然有效的授权。
func ValidateContent(content []byte) (*Data, error) {
	if defaultManager == nil {
		return nil, &Error{CodeNotInit, "授权模块尚未初始化"}
	}
	return defaultManager.ValidateContent(content)
}

// ValidateContent 是 Manager 上的内容校验实现。
func (m *Manager) ValidateContent(content []byte) (*Data, error) {
	if !m.enforce {
		return nil, nil
	}
	st := m.evaluateBytes(content, time.Now().UTC(), false)
	if !st.Valid {
		return st.License, &Error{Code: st.Code, Msg: st.Message}
	}
	return st.License, nil
}

// Install 校验通过后把新授权写入磁盘并立即生效。
// 写入前会备份原授权为 .bak，写盘采用临时文件 + 重命名，避免出现半截文件。
func Install(content []byte) (State, error) {
	if defaultManager == nil {
		return State{Code: CodeNotInit, Valid: false, Message: "授权模块尚未初始化"}, &Error{CodeNotInit, "授权模块尚未初始化"}
	}
	return defaultManager.Install(content)
}

// Install 是 Manager 上的安装实现。
func (m *Manager) Install(content []byte) (State, error) {
	// 先完整校验，任何一步不过都不碰现有文件。
	st := m.evaluateBytes(content, time.Now().UTC(), false)
	if !st.Valid {
		return st, &Error{Code: st.Code, Msg: st.Message}
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if old, err := os.ReadFile(m.path); err == nil && len(old) > 0 {
		// 备份失败不阻断安装：旧授权本来就已经失效或不合法时才需要上传新文件。
		_ = os.WriteFile(m.path+".bak", old, 0o600)
	}

	if err := os.MkdirAll(filepath.Dir(m.path), 0o755); err != nil {
		return st, fmt.Errorf("创建授权目录失败: %w", err)
	}

	tmp := m.path + ".tmp"
	if err := os.WriteFile(tmp, content, 0o600); err != nil {
		return st, fmt.Errorf("写入授权文件失败: %w", err)
	}
	if err := os.Rename(tmp, m.path); err != nil {
		_ = os.Remove(tmp)
		return st, fmt.Errorf("替换授权文件失败: %w", err)
	}

	return m.Reload(), nil
}

// Stop 停止后台复检协程，主要供测试使用。
func Stop() {
	if defaultManager == nil {
		return
	}
	defaultManager.Stop()
}

// Stop 是 Manager 上的停止实现。
func (m *Manager) Stop() {
	m.stopOnce.Do(func() { close(m.stopCh) })
}

// startLoop 启动后台定时复检。授权到期是"时间到了就自动失效"，
// 不能只在请求进来时才判断，否则空闲服务会一直用着过期授权。
func (m *Manager) startLoop() {
	go func() {
		ticker := time.NewTicker(m.refresh)
		defer ticker.Stop()
		for {
			select {
			case <-m.stopCh:
				return
			case <-ticker.C:
				m.Reload()
			}
		}
	}()
}

// evaluateFile 读取磁盘上的授权文件并评估。
func (m *Manager) evaluateFile() State {
	now := time.Now().UTC()
	if !m.enforce {
		return State{
			Code:      CodeDisabled,
			Message:   "授权校验已关闭（LICENSE_ENFORCE=false），仅供本地开发使用",
			Valid:     true,
			CheckedAt: now,
		}
	}
	content, err := os.ReadFile(m.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return State{
				Code:      CodeMissing,
				Message:   "未找到授权文件，请在「授权管理」页面上传授权文件",
				Valid:     false,
				CheckedAt: now,
			}
		}
		return State{
			Code:      CodeMalformed,
			Message:   fmt.Sprintf("读取授权文件失败: %v", err),
			Valid:     false,
			CheckedAt: now,
		}
	}
	return m.evaluateBytes(content, now, true)
}

// evaluateBytes 是完整的授权评估流程，顺序不可随意调整：
// 先验签再谈业务字段，签名不过时不解析任何内容，避免被伪造数据误导。
//
// checkClock 为 true 时会做时间回拨检测并更新检测记录；
// 上传前的预校验传 false，避免一次失败的上传污染时间基准。
func (m *Manager) evaluateBytes(content []byte, now time.Time, checkClock bool) State {
	data, err := m.verifyContent(content)
	if err != nil {
		var le *Error
		if errors.As(err, &le) {
			return State{Code: le.Code, Message: le.Msg, Valid: false, CheckedAt: now}
		}
		return State{Code: CodeMalformed, Message: err.Error(), Valid: false, CheckedAt: now}
	}

	if checkClock {
		if last, ok := m.readClock(); ok && now.Add(clockTolerance).Before(last) {
			return State{
				Code:      CodeClockRollback,
				Message:   fmt.Sprintf("检测到系统时间被回拨（上次校验时间 %s，当前 %s），授权校验失败", formatLocal(last), formatLocal(now)),
				Valid:     false,
				CheckedAt: now,
				License:   data,
			}
		}
		m.writeClock(now)
	}

	remaining := data.ExpireTime.Sub(now)
	if remaining > 0 {
		return State{
			Code:          CodeOK,
			Message:       fmt.Sprintf("授权有效，到期时间 %s", formatLocal(data.ExpireTime)),
			Valid:         true,
			RemainingDays: int(remaining.Hours() / 24),
			CheckedAt:     now,
			License:       data,
		}
	}

	grace := time.Duration(data.GraceDays) * 24 * time.Hour
	if data.GraceDays > 0 && -remaining <= grace {
		return State{
			Code: CodeOK,
			Message: fmt.Sprintf("授权已于 %s 到期，当前处于 %d 天宽限期，请尽快联系供应商续期",
				formatLocal(data.ExpireTime), data.GraceDays),
			Valid:         true,
			InGrace:       true,
			RemainingDays: int(remaining.Hours() / 24),
			CheckedAt:     now,
			License:       data,
		}
	}

	return State{
		Code: CodeExpired,
		Message: fmt.Sprintf("授权已于 %s 到期，系统功能已停用，请上传新的授权文件",
			formatLocal(data.ExpireTime)),
		Valid:         false,
		RemainingDays: int(remaining.Hours() / 24),
		CheckedAt:     now,
		License:       data,
	}
}

// verifyContent 完成"解格式 → 验签名 → 解字段"三步，全部通过才返回授权数据。
func (m *Manager) verifyContent(content []byte) (*Data, error) {
	if m.pub == nil {
		return nil, &Error{CodeNoPublicKey, "内置公钥未配置，无法校验授权"}
	}

	var f File
	if err := json.Unmarshal(content, &f); err != nil {
		return nil, &Error{CodeMalformed, "授权文件格式错误，请确认上传的是 .lic 授权文件"}
	}
	if strings.TrimSpace(f.Data) == "" || strings.TrimSpace(f.Sign) == "" {
		return nil, &Error{CodeMalformed, "授权文件内容不完整"}
	}

	dataJSON, err := base64.StdEncoding.DecodeString(f.Data)
	if err != nil {
		return nil, &Error{CodeMalformed, "授权内容无法解码"}
	}
	sign, err := base64.StdEncoding.DecodeString(f.Sign)
	if err != nil {
		return nil, &Error{CodeMalformed, "授权签名无法解码"}
	}

	// 核心防线：签名不匹配说明内容被改过，或根本不是本方签发的授权。
	if err := VerifySignature(m.pub, dataJSON, sign); err != nil {
		return nil, &Error{CodeBadSignature, "授权文件签名校验失败，文件可能已被篡改或非本方签发"}
	}

	var data Data
	if err := json.Unmarshal(dataJSON, &data); err != nil {
		return nil, &Error{CodeMalformed, "授权数据无法解析"}
	}
	if data.ExpireTime.IsZero() {
		return nil, &Error{CodeMalformed, "授权文件缺少到期时间"}
	}
	if data.GraceDays < 0 {
		data.GraceDays = 0
	}
	data.ExpireTime = data.ExpireTime.UTC()
	data.IssuedAt = data.IssuedAt.UTC()
	return &data, nil
}

// readClock 读取上次成功校验的时间；文件缺失或 HMAC 不匹配时返回 false，
// 此时按"无历史基准"处理，不误判为回拨。
func (m *Manager) readClock() (time.Time, bool) {
	raw, err := os.ReadFile(m.clockPath)
	if err != nil {
		return time.Time{}, false
	}
	parts := strings.SplitN(strings.TrimSpace(string(raw)), "|", 2)
	if len(parts) != 2 {
		return time.Time{}, false
	}
	sec, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return time.Time{}, false
	}
	mac, err := hex.DecodeString(parts[1])
	if err != nil {
		return time.Time{}, false
	}
	if !hmac.Equal(mac, clockMAC(sec)) {
		return time.Time{}, false
	}
	return time.Unix(sec, 0).UTC(), true
}

// writeClock 记录本次校验时间，写入失败只影响回拨检测的连续性，不影响授权判断。
func (m *Manager) writeClock(t time.Time) {
	sec := t.Unix()
	body := strconv.FormatInt(sec, 10) + "|" + hex.EncodeToString(clockMAC(sec))
	_ = os.WriteFile(m.clockPath, []byte(body), 0o600)
}

func clockMAC(sec int64) []byte {
	h := hmac.New(sha256.New, []byte(clockSecret))
	h.Write([]byte(strconv.FormatInt(sec, 10)))
	return h.Sum(nil)
}

// Issue 用私钥签发一份授权文件内容，供 cmd/license-tool 调用。
// 主程序不会用到它，公钥侧只负责验签。
func Issue(priv *rsa.PrivateKey, d Data) ([]byte, error) {
	if priv == nil {
		return nil, errors.New("私钥不能为空")
	}
	if d.ExpireTime.IsZero() {
		return nil, errors.New("到期时间不能为空")
	}
	if d.IssuedAt.IsZero() {
		d.IssuedAt = time.Now()
	}
	if strings.TrimSpace(d.Product) == "" {
		d.Product = "webb-api"
	}
	d.IssuedAt = d.IssuedAt.UTC().Truncate(time.Second)
	d.ExpireTime = d.ExpireTime.UTC().Truncate(time.Second)

	dataJSON, err := json.Marshal(d)
	if err != nil {
		return nil, fmt.Errorf("序列化授权数据失败: %w", err)
	}
	sign, err := SignData(priv, dataJSON)
	if err != nil {
		return nil, fmt.Errorf("签名失败: %w", err)
	}
	return json.MarshalIndent(File{
		Data: base64.StdEncoding.EncodeToString(dataJSON),
		Sign: base64.StdEncoding.EncodeToString(sign),
	}, "", "  ")
}

// DefaultGraceDays 返回签发授权时的默认宽限天数。
func DefaultGraceDays() int { return defaultGraceDays }

// SignData 对数据做 SHA-256 摘要后用 RSA PKCS#1 v1.5 签名。
func SignData(priv *rsa.PrivateKey, dataJSON []byte) ([]byte, error) {
	sum := sha256.Sum256(dataJSON)
	return rsa.SignPKCS1v15(rand.Reader, priv, crypto.SHA256, sum[:])
}

// VerifySignature 用公钥校验签名，返回 nil 表示签名与数据匹配。
func VerifySignature(pub *rsa.PublicKey, dataJSON, sign []byte) error {
	sum := sha256.Sum256(dataJSON)
	return rsa.VerifyPKCS1v15(pub, crypto.SHA256, sum[:], sign)
}

// ParsePublicKeyPEM 解析 PKIX 格式的 RSA 公钥。
func ParsePublicKeyPEM(pemStr string) (*rsa.PublicKey, error) {
	block, _ := pem.Decode([]byte(strings.TrimSpace(pemStr)))
	if block == nil {
		return nil, errors.New("不是有效的 PEM 数据，请确认包含 BEGIN/END PUBLIC KEY 两行")
	}
	pubAny, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("解析公钥失败: %w", err)
	}
	rsaPub, ok := pubAny.(*rsa.PublicKey)
	if !ok {
		return nil, errors.New("公钥类型错误：需要 RSA 公钥")
	}
	return rsaPub, nil
}

// ParsePrivateKeyPEM 解析 RSA 私钥，同时兼容 PKCS#1 与 PKCS#8 两种编码。
func ParsePrivateKeyPEM(pemStr string) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(strings.TrimSpace(pemStr)))
	if block == nil {
		return nil, errors.New("不是有效的 PEM 数据，请确认包含 BEGIN/END PRIVATE KEY 两行")
	}
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	keyAny, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("解析私钥失败: %w", err)
	}
	rsaKey, ok := keyAny.(*rsa.PrivateKey)
	if !ok {
		return nil, errors.New("私钥类型错误：需要 RSA 私钥")
	}
	return rsaKey, nil
}

// EncodePrivateKeyPEM 把私钥导出为 PKCS#1 PEM 文本。
func EncodePrivateKeyPEM(priv *rsa.PrivateKey) []byte {
	return pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(priv),
	})
}

// EncodePublicKeyPEM 把公钥导出为 PKIX PEM 文本，即需要粘贴进源码的那一段。
func EncodePublicKeyPEM(pub *rsa.PublicKey) ([]byte, error) {
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return nil, fmt.Errorf("导出公钥失败: %w", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}), nil
}

func resolvePath() string {
	if p := strings.TrimSpace(os.Getenv("LICENSE_PATH")); p != "" {
		return p
	}
	return DefaultLicensePath
}

// resolveEnforce 默认开启强制校验，只有显式设置为关闭值时才放行，
// 避免因为环境变量拼错而意外失去授权保护。
func resolveEnforce() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("LICENSE_ENFORCE"))) {
	case "false", "0", "off", "no":
		return false
	default:
		return true
	}
}

// formatLocal 把 UTC 时刻转成本地时区展示。
// 比较一律用绝对时刻（不受时区影响），只有给人看的文案才转本地时区，
// 这样既不会被改时区绕过，又不会出现"到期日期差一天"的困惑。
func formatLocal(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Local().Format("2006-01-02 15:04")
}
