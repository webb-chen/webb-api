// Package license 提供统一授权平台（ULP）License Format v1 的离线验签与状态管理。
//
// 设计要点：
//   - 只内置公钥，私钥永不进入本项目（签发由统一授权平台完成）。
//   - 验签对象是信封 data 字段 base64 解码后的**原始字节**，绝不重新序列化。
//   - 校验顺序严格遵循《统一授权文件格式规范》§6：读文件 → 解信封 → 验签 →
//     解载荷 → schema/platform/product → machine_code → 时间回拨 → 到期。
//   - 授权失效时保留完整的恢复通路（状态、机器码、上传、重载、登录、健康检查）。
//
// 本包只依赖标准库，不引入项目内其他包，避免循环依赖。
package license

import (
	"crypto"
	"crypto/hmac"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// ============================================================================
// 统一状态码词表（《统一授权文件格式规范》§7），必须原样返回给前端。
// ============================================================================

const (
	CodeOK              = "ok"                       // 授权有效
	CodeGrace           = "license_grace"            // 已到期，处于宽限期（仍有效）
	CodeDisabled        = "license_disabled"         // 强制校验已关闭（仅开发态）
	CodeNotInit         = "license_not_init"         // 授权模块未初始化
	CodeMissing         = "license_missing"          // 找不到授权文件
	CodeMalformed       = "license_malformed"        // 文件结构或编码损坏
	CodeBadSignature    = "license_bad_signature"    // 签名校验失败
	CodeExpired         = "license_expired"          // 已到期且宽限期结束
	CodeClockRollback   = "license_clock_rollback"   // 检测到系统时间回拨
	CodeMachineMismatch = "license_machine_mismatch" // 与本机不匹配
	CodeProductMismatch = "license_product_mismatch" // 与当前程序产品不符
	CodeNoPublicKey     = "license_no_public_key"    // 内置公钥未配置或为占位符
)

// Product 是本项目在统一授权平台中的 product 标识。
// 必须与平台「项目管理」中的 product 完全一致，否则授权会被判为
// license_product_mismatch。修改时请同步更新 web 端文案与部署文档。
const Product = "api"

// SchemaVersion 是本实现支持的唯一授权文件版本。
const SchemaVersion = 1

const (
	// DefaultLicensePath 是授权文件的默认位置（相对程序工作目录）。
	// Docker 部署时应通过环境变量 LICENSE_PATH 指向持久化卷，例如 /data/license.lic。
	DefaultLicensePath = "./license.lic"

	// DefaultRefreshInterval 是后台复检间隔。到期时间跨过零点、或运维手工替换了
	// 授权文件，都会在下一个复检周期内被感知，无需重启服务。
	DefaultRefreshInterval = 60 * time.Second

	// clockTolerance 是时间回拨检测的容忍量。留足余量是为了避免 NTP 校时、
	// 虚拟机时钟漂移造成误判；真正要拦的是"把系统时间往回调几个月"。
	clockTolerance = 10 * time.Minute

	// PlatformID 是统一授权平台的签发方标识。
	PlatformID = "unified-license-platform"

	// UniversalMachineCode 是通用授权的显式取值。空串/缺失按格式损坏处理。
	UniversalMachineCode = "*"

	// licenseClockFileName 是时间回拨检查点文件名，与授权文件放在同一持久化目录。
	licenseClockFileName = "license-clock.dat"

	// clockSecret 用于给时间回拨检查点的落盘记录加 HMAC，防止客户直接编辑
	// 该文件伪造"上次检查时间"。
	clockSecret = "webb-api-license-clock-v1"
)

// publicKeyPEM 是内置验签公钥，对应统一授权平台（ULP）
// http://111.170.146.212:7630 的「api」项目。
// 离线副本见 授权信息/api-public.pem，两者必须逐字一致；
// 平台轮换密钥后需同步替换本文件并重新构建镜像。
//
// 公钥是公开信息，可以进代码仓库；私钥只存在于 ULP 服务端，绝不下发。
// 可用环境变量 LICENSE_PUBLIC_KEY 覆盖（仅用于测试或多环境构建，默认不设置）。
const publicKeyPEM = `-----BEGIN PUBLIC KEY-----
MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEA1/GN15hOhgNRsu6PfK8V
bQk1d4ekA7naBZT4cR5FXFJ2grlznsh/QRDHonII7Ud4Q0z0exHXWvL0z2vVKAL7
+ivHJrIso7pInb0FxH0Ewmxx1TbdyhS7tbquL5ZNUCi7DM+cIil0+7Fwg/U/QNs7
qDi/w6KPZMCDCoudaWTvXrpWQXIwwsyfsrHkfcE3vGjbtxSomwxuoyJs5qA21AdZ
VT5GXjn67B4uUJ1Tf34bbdokN+g3c1t2dVi9zFVHIDOR65+TIOfZOPSLXEvPuZZO
rA+GUpRGO2kjAYEMh8vjUwaOETDdq+jPkMU/LULGvX449VnepnD9pRD8358cvC2P
6QIDAQAB
-----END PUBLIC KEY-----`

// publicKeyPlaceholder 用于判断内置公钥是否还没被替换。
const publicKeyPlaceholder = "REPLACE_WITH_YOUR_PUBLIC_KEY"

// ============================================================================
// 数据结构
// ============================================================================

// Envelope 是 .lic 文件的外层结构：
// data = base64(payload 原始字节)，sign = base64(RSA PKCS#1 v1.5 签名)。
type Envelope struct {
	Data string `json:"data"`
	Sign string `json:"sign"`
}

// Payload 是 v1 授权载荷。字段与《统一授权文件格式规范》§3.1 一致。
// 验签端不使用重新序列化，因此字段顺序仅影响可读性。
type Payload struct {
	SchemaVersion   int                    `json:"schema_version"`
	LicenseID       string                 `json:"license_id"`
	Platform        string                 `json:"platform"`
	Product         string                 `json:"product"`
	ProductDisplay  string                 `json:"product_display,omitempty"`
	Customer        string                 `json:"customer,omitempty"`
	CustomerContact string                 `json:"customer_contact,omitempty"`
	IssuedAt        *time.Time             `json:"issued_at,omitempty"`
	ExpireTime      *time.Time             `json:"expire_time"`
	GraceDays       int                    `json:"grace_days"`
	MachineCode     string                 `json:"machine_code"`
	InstanceID      string                 `json:"instance_id,omitempty"`
	Features        map[string]interface{} `json:"features,omitempty"`
	IssuedBy        string                 `json:"issued_by,omitempty"`
	Note            string                 `json:"note,omitempty"`

	// Legacy 标记该载荷来自旧版格式（缺少 schema_version），仅供过渡期兼容。
	Legacy bool `json:"-"`

	// expireTimePresent 记录 JSON 中是否显式出现 expire_time 字段。
	// 规范 §3.1 要求该字段必填：null 才表示永久授权，字段缺失属于格式损坏，
	// 不能被当成"永久有效"，否则一个漏写字段的授权文件就永远不过期。
	expireTimePresent bool
}

// UnmarshalJSON 在常规反序列化之外记录 expire_time 是否显式存在。
// 用于区分"显式 null（永久授权）"与"字段缺失（格式损坏）"。
func (p *Payload) UnmarshalJSON(data []byte) error {
	type payloadAlias Payload
	if err := json.Unmarshal(data, (*payloadAlias)(p)); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	_, p.expireTimePresent = fields["expire_time"]
	return nil
}

// Permanent 表示永久授权（expire_time == null）。
func (p *Payload) Permanent() bool { return p != nil && p.ExpireTime == nil }

// State 是一次授权检查的结论，中间件与接口都读它。
type State struct {
	Code          string    `json:"code"`
	Message       string    `json:"message"`
	Valid         bool      `json:"valid"`
	InGrace       bool      `json:"in_grace"`
	RemainingDays int       `json:"remaining_days"`
	CheckedAt     time.Time `json:"checked_at"`
	License       *Payload  `json:"license,omitempty"`
}

// Error 携带统一状态码的错误，便于调用方区分失败原因而不是靠字符串匹配。
type Error struct {
	Code string
	Msg  string
}

func (e *Error) Error() string { return e.Msg }

// Config 是 Init 的入参。Enforce 显式传给 InitWithConfig；Init 使用默认配置
// （由 LICENSE_ENFORCE 推导，默认 true）。
type Config struct {
	Path      string        // 授权文件路径，留空则取 LICENSE_PATH，再退回 DefaultLicensePath
	PublicKey string        // 验签公钥 PEM，留空则用内置 publicKeyPEM
	Refresh   time.Duration // 后台复检间隔，留空则用 DefaultRefreshInterval
	Enforce   bool          // 是否强制校验
}

// Manager 持有公钥与最新授权状态。状态用 atomic.Pointer 存放，
// 中间件读取时无需加锁，避免高并发转发场景下成为瓶颈。
type Manager struct {
	mu        sync.Mutex // 保护授权文件写入与时间回拨检查点
	state     atomic.Pointer[State]
	pub       *rsa.PublicKey
	path      string
	clockPath string
	enforce   bool
	refresh   time.Duration
	initErr   error
	stopCh    chan struct{}
	stopOnce  sync.Once
}

// defaultManager 是进程内唯一的授权管理器，在服务开始监听前完成初始化。
var defaultManager *Manager

// DefaultConfig 从环境变量推导默认配置。
//
//	LICENSE_PATH         授权文件路径，默认 ./license.lic
//	LICENSE_PUBLIC_KEY   公钥 PEM，默认使用内置公钥
//	LICENSE_ENFORCE      设为 false/0/off/no 时关闭强制校验，仅供本地开发调试
func DefaultConfig() Config {
	return Config{
		Path:      resolvePath(),
		PublicKey: resolvePublicKey(),
		Refresh:   DefaultRefreshInterval,
		Enforce:   resolveEnforce(),
	}
}

// Init 用默认配置初始化授权模块。
func Init() error {
	return InitWithConfig(DefaultConfig())
}

// InitWithConfig 初始化授权模块：解析公钥、做一次立即校验、启动后台复检。
// 返回错误时调用方必须终止启动——授权模块没起来就对外服务，等于没有授权保护。
func InitWithConfig(cfg Config) error {
	if defaultManager != nil {
		return errors.New("授权模块已初始化，不能重复初始化")
	}
	if cfg.Path == "" {
		cfg.Path = resolvePath()
	}
	if cfg.PublicKey == "" {
		cfg.PublicKey = resolvePublicKey()
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
		clockPath: filepath.Join(filepath.Dir(absPath), licenseClockFileName),
		enforce:   cfg.Enforce,
		refresh:   cfg.Refresh,
		stopCh:    make(chan struct{}),
	}

	if m.enforce {
		if strings.Contains(cfg.PublicKey, publicKeyPlaceholder) || strings.TrimSpace(cfg.PublicKey) == "" {
			m.initErr = fmt.Errorf("内置公钥尚未配置（状态码 %s）", CodeNoPublicKey)
			return m.initErr
		}
		pub, err := ParsePublicKeyPEM(cfg.PublicKey)
		if err != nil {
			m.initErr = fmt.Errorf("加载内置公钥失败（状态码 %s）: %w", CodeNoPublicKey, err)
			return m.initErr
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

// Current 返回当前授权状态，读取无锁、可安全用于每个请求。
//
// 除后台复检外，这里还会用 time.Now() 对缓存状态做一次纯内存的到期复算：
// 授权一旦跨过 expire_time，下一个请求就会立刻被拦截，而不必等待最长
// DefaultRefreshInterval（60s）的复检周期。否则会出现"授权已到期、系统仍可
// 使用最多 60 秒"的空窗，这正是"到期后还能正常使用"的直接原因之一。
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
	return m.Current()
}

// Current 是 Manager 上的状态读取实现。
func (m *Manager) Current() State {
	st := m.state.Load()
	if st == nil {
		return m.Reload()
	}
	return m.revalidate(*st)
}

// revalidate 用当前时刻复算缓存状态中的时间敏感字段（到期 / 宽限期 / 剩余天数）。
// 这里只做纯内存计算，不读文件、不验签、不加锁，因此可以放在每个请求的路径上。
// 验签结果、机器码、产品等静态结论仍然只由 Reload 产出，不会被这里改写。
func (m *Manager) revalidate(st State) State {
	if !m.enforce || st.License == nil {
		return st
	}
	now := time.Now().UTC()
	// 时间回拨的快速判据：当前时间比上次校验时间还早一个容忍量以上，
	// 说明系统时钟被往回调过。等不到下一次后台复检也必须立即失效，
	// 否则把时钟调回到期前就能继续使用。
	if !st.CheckedAt.IsZero() && now.Add(clockTolerance).Before(st.CheckedAt) {
		return State{
			Code:      CodeClockRollback,
			Message:   fmt.Sprintf("检测到系统时间被回拨（上次校验时间 %s，当前 %s），授权校验失败", formatLocal(st.CheckedAt), formatLocal(now)),
			Valid:     false,
			CheckedAt: st.CheckedAt,
			License:   st.License,
		}
	}
	switch st.Code {
	case CodeOK, CodeGrace:
		return expiryState(st.License, now, st.CheckedAt)
	default:
		return st
	}
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

// ValidateContent 只校验一段授权内容是否合法，不读写授权文件、不写时间回拨检查点。
// 上传接口用它做"先验证后落盘"，避免用一个坏文件覆盖掉当前仍然有效的授权。
func ValidateContent(content []byte) (*Payload, error) {
	if defaultManager == nil {
		return nil, &Error{CodeNotInit, "授权模块尚未初始化"}
	}
	return defaultManager.ValidateContent(content)
}

// ValidateContent 是 Manager 上的内容校验实现。
func (m *Manager) ValidateContent(content []byte) (*Payload, error) {
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
// 写入前备份原授权为 .bak，写盘采用临时文件 + 原子重命名（不变量 6）。
func Install(content []byte) (State, error) {
	if defaultManager == nil {
		return State{Code: CodeNotInit, Valid: false, Message: "授权模块尚未初始化", CheckedAt: time.Now().UTC()},
			&Error{CodeNotInit, "授权模块尚未初始化"}
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

	// 写盘期间持锁，避免并发上传互相覆盖；写完后先释放锁再重载，
	// 因为重载会做时间回拨检测并重新获取同一把锁（避免自死锁）。
	if err := func() error {
		m.mu.Lock()
		defer m.mu.Unlock()

		if old, err := os.ReadFile(m.path); err == nil && len(old) > 0 {
			// 备份失败不阻断安装：旧授权本来就已经失效或不合法时才需要上传新文件。
			_ = os.WriteFile(m.path+".bak", old, 0o600)
		}

		if err := os.MkdirAll(filepath.Dir(m.path), 0o755); err != nil {
			return fmt.Errorf("创建授权目录失败: %w", err)
		}

		tmp := m.path + ".tmp"
		if err := os.WriteFile(tmp, content, 0o600); err != nil {
			return fmt.Errorf("写入授权文件失败: %w", err)
		}
		if err := os.Rename(tmp, m.path); err != nil {
			_ = os.Remove(tmp)
			return fmt.Errorf("替换授权文件失败: %w", err)
		}
		return nil
	}(); err != nil {
		return st, err
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
	if m.initErr != nil {
		return State{Code: CodeNoPublicKey, Message: m.initErr.Error(), Valid: false, CheckedAt: now}
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

// evaluateBytes 是完整的授权评估流程，顺序不可随意调整（规范 §6）。
//
// checkClock 为 true 时会做时间回拨检测并更新检查点；
// 上传前的预校验传 false，避免一次失败的上传污染时间基准。
func (m *Manager) evaluateBytes(content []byte, now time.Time, checkClock bool) State {
	payload, err := m.verifyContent(content)
	if err != nil {
		var le *Error
		if errors.As(err, &le) {
			return State{Code: le.Code, Message: le.Msg, Valid: false, CheckedAt: now}
		}
		return State{Code: CodeMalformed, Message: err.Error(), Valid: false, CheckedAt: now}
	}

	if checkClock {
		if err := m.checkClock(now); err != nil {
			return State{
				Code:      CodeClockRollback,
				Message:   err.Error(),
				Valid:     false,
				CheckedAt: now,
				License:   payload,
			}
		}
	}

	return expiryState(payload, now, now)
}

// expiryState 根据载荷里的 expire_time / grace_days 判定"当前是否仍有效"。
//
// 独立成函数是为了让 Current() 能在每次请求时用最新时间复算同一套规则，
// 保证"后台复检"与"请求路径"永远得到一致结论。
func expiryState(payload *Payload, now, checkedAt time.Time) State {
	if payload.Permanent() {
		return State{
			Code:          CodeOK,
			Message:       "授权有效（永久授权）",
			Valid:         true,
			RemainingDays: -1,
			CheckedAt:     checkedAt,
			License:       payload,
		}
	}

	remaining := payload.ExpireTime.Sub(now)
	if remaining > 0 {
		return State{
			Code:          CodeOK,
			Message:       fmt.Sprintf("授权有效，到期时间 %s", formatLocal(*payload.ExpireTime)),
			Valid:         true,
			RemainingDays: int(remaining.Hours() / 24),
			CheckedAt:     checkedAt,
			License:       payload,
		}
	}

	grace := time.Duration(payload.GraceDays) * 24 * time.Hour
	if payload.GraceDays > 0 && -remaining <= grace {
		return State{
			Code: CodeGrace,
			Message: fmt.Sprintf("授权已于 %s 到期，当前处于 %d 天宽限期，请尽快联系供应商续期",
				formatLocal(*payload.ExpireTime), payload.GraceDays),
			Valid:         true,
			InGrace:       true,
			RemainingDays: int(remaining.Hours() / 24),
			CheckedAt:     checkedAt,
			License:       payload,
		}
	}

	return State{
		Code: CodeExpired,
		Message: fmt.Sprintf("授权已于 %s 到期，系统功能已停用，请上传新的授权文件",
			formatLocal(*payload.ExpireTime)),
		Valid:         false,
		RemainingDays: int(remaining.Hours() / 24),
		CheckedAt:     checkedAt,
		License:       payload,
	}
}

// verifyContent 完成"解信封 → 验签（原始字节）→ 解载荷 → 业务字段"全流程。
func (m *Manager) verifyContent(content []byte) (*Payload, error) {
	if m.pub == nil {
		return nil, &Error{CodeNoPublicKey, "内置公钥未配置，无法校验授权"}
	}

	var env Envelope
	if err := json.Unmarshal(content, &env); err != nil {
		return nil, &Error{CodeMalformed, "授权文件格式错误，请确认上传的是 .lic 授权文件"}
	}
	if strings.TrimSpace(env.Data) == "" || strings.TrimSpace(env.Sign) == "" {
		return nil, &Error{CodeMalformed, "授权文件内容不完整（缺少 data 或 sign）"}
	}

	payloadBytes, err := base64.StdEncoding.DecodeString(env.Data)
	if err != nil {
		return nil, &Error{CodeMalformed, "授权内容 base64 解码失败"}
	}
	signature, err := base64.StdEncoding.DecodeString(env.Sign)
	if err != nil {
		return nil, &Error{CodeMalformed, "授权签名 base64 解码失败"}
	}

	// ★ 不变量 1：对 data 解码后的原始字节验签，绝不重新序列化。
	sum := sha256.Sum256(payloadBytes)
	if err := rsa.VerifyPKCS1v15(m.pub, crypto.SHA256, sum[:], signature); err != nil {
		return nil, &Error{CodeBadSignature, "授权文件签名校验失败，文件可能已被篡改或非统一授权平台签发"}
	}

	// 验签通过后才解析业务字段。
	var payload Payload
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		return nil, &Error{CodeMalformed, "授权载荷 JSON 解析失败"}
	}

	// 过渡期兼容：旧版载荷没有 schema_version/新字段。
	// 新签发一律由 ULP 产出 schema_version=1，旧格式仅做只读兼容。
	if payload.SchemaVersion == 0 {
		payload.Legacy = true
		if payload.GraceDays < 0 {
			payload.GraceDays = 0
		}
		// 旧版用零值时间表示永久授权；统一格式用 null 表示。
		if payload.ExpireTime != nil && payload.ExpireTime.IsZero() {
			payload.ExpireTime = nil
		}
		if strings.TrimSpace(payload.MachineCode) == "" {
			// 旧版用空串表示通用授权；新规范要求显式 "*"。
			payload.MachineCode = UniversalMachineCode
		}
		return &payload, nil
	}
	if payload.SchemaVersion != SchemaVersion {
		return nil, &Error{CodeMalformed,
			fmt.Sprintf("不支持的授权文件版本 schema_version=%d（本程序支持 %d）", payload.SchemaVersion, SchemaVersion)}
	}

	// v1 严格校验。
	// expire_time 必填：null = 永久授权；字段缺失 = 格式损坏（不能隐式当成永久）。
	if !payload.expireTimePresent {
		return nil, &Error{CodeMalformed,
			"授权文件缺少 expire_time 字段（永久授权必须显式写 null）"}
	}
	if payload.Platform != PlatformID {
		return nil, &Error{CodeProductMismatch, "授权签发方标识不匹配，非统一授权平台签发的文件"}
	}
	if payload.Product != Product {
		return nil, &Error{CodeProductMismatch,
			fmt.Sprintf("授权文件属于产品 %q，与当前程序产品 %q 不符", payload.Product, Product)}
	}
	if strings.TrimSpace(payload.MachineCode) == "" {
		return nil, &Error{CodeMalformed, "授权文件缺少 machine_code 字段（通用授权必须显式写 \"*\"）"}
	}
	if payload.MachineCode != UniversalMachineCode {
		current := MachineCode()
		if payload.MachineCode != current && payload.MachineCode != LegacyMachineCode() {
			return nil, &Error{CodeMachineMismatch, "授权文件绑定了其他机器，与本机机器码不匹配"}
		}
	}
	if payload.GraceDays < 0 {
		payload.GraceDays = 0
	}
	return &payload, nil
}

// ============================================================================
// 时间回拨检测
// ============================================================================

// checkClock 在单把锁内原子完成"读取检查点 → 判断回拨 → 单调更新"。
// 永久授权同样参与检测，否则把系统时间调回去就能让永久授权"续命"。
func (m *Manager) checkClock(now time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	last, ok := readClock(m.clockPath)
	if ok && now.Add(clockTolerance).Before(last) {
		return fmt.Errorf("检测到系统时间被回拨（上次校验时间 %s，当前 %s），授权校验失败",
			formatLocal(last), formatLocal(now))
	}
	if !ok || now.After(last) {
		writeClock(m.clockPath, now)
	}
	return nil
}

// readClock 读取上次成功校验的时间；文件缺失或 HMAC 不匹配时返回 false，
// 此时按"无历史基准"处理，不误判为回拨。
func readClock(path string) (time.Time, bool) {
	raw, err := os.ReadFile(path)
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

func writeClock(path string, t time.Time) {
	sec := t.Unix()
	body := strconv.FormatInt(sec, 10) + "|" + hex.EncodeToString(clockMAC(sec))
	_ = os.WriteFile(path, []byte(body), 0o600)
}

func clockMAC(sec int64) []byte {
	h := hmac.New(sha256.New, []byte(clockSecret))
	h.Write([]byte(strconv.FormatInt(sec, 10)))
	return h.Sum(nil)
}

// ============================================================================
// 机器码（规范 §5）：SHA-256("ULP-LICENSE-V1:"+raw) 前 16 字节 → 32 位小写 hex
// ============================================================================

var (
	machineOnce  sync.Once
	machineValue string
	legacyOnce   sync.Once
	legacyValue  string
)

// MachineCode 返回本机统一机器码。支持环境变量 LICENSE_MACHINE_CODE 覆盖。
func MachineCode() string {
	if v := strings.TrimSpace(os.Getenv("LICENSE_MACHINE_CODE")); v != "" {
		return v
	}
	machineOnce.Do(func() {
		raw := rawMachineIdentity()
		sum := sha256.Sum256([]byte("ULP-LICENSE-V1:" + raw))
		machineValue = hex.EncodeToString(sum[:16])
	})
	return machineValue
}

// rawMachineIdentity 按规范 §5.1 的优先级取第一个非空原始标识。
func rawMachineIdentity() string {
	for _, path := range []string{"/etc/machine-id", "/var/lib/dbus/machine-id"} {
		if b, err := os.ReadFile(path); err == nil {
			if id := strings.TrimSpace(string(b)); id != "" {
				return id
			}
		}
	}
	if host, err := os.Hostname(); err == nil && strings.TrimSpace(host) != "" {
		return strings.TrimSpace(host)
	}
	if mac := firstMAC(); mac != "" {
		return mac
	}
	return "unknown"
}

// firstMAC 返回首个非回环网卡的 MAC 地址；取不到时返回空串。
func firstMAC() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		if addr := iface.HardwareAddr.String(); addr != "" {
			return addr
		}
	}
	return ""
}

// LegacyMachineCode 返回旧版机器码（A1B2-C3D4-... 形式），仅用于过渡期兼容。
// 新签发授权一律使用统一格式；命中旧格式时调用方应记录告警。
func LegacyMachineCode() string {
	legacyOnce.Do(func() {
		// 旧版算法：machine-id 优先，否则 hostname + "/" + 首个非回环 MAC。
		raw := ""
		for _, path := range []string{"/etc/machine-id", "/var/lib/dbus/machine-id"} {
			if b, err := os.ReadFile(path); err == nil {
				if id := strings.TrimSpace(string(b)); id != "" {
					raw = id
					break
				}
			}
		}
		if raw == "" {
			host, err := os.Hostname()
			if err != nil || strings.TrimSpace(host) == "" {
				host = "unknown-host"
			}
			raw = host + "/" + firstMAC()
		}
		sum := sha256.Sum256([]byte("webb-api-license:" + raw))
		hexStr := strings.ToUpper(hex.EncodeToString(sum[:8]))
		var b strings.Builder
		for i := 0; i < len(hexStr); i++ {
			if i > 0 && i%4 == 0 {
				b.WriteByte('-')
			}
			b.WriteByte(hexStr[i])
		}
		legacyValue = b.String()
	})
	return legacyValue
}

// ============================================================================
// 公钥解析
// ============================================================================

// ParsePublicKeyPEM 解析 RSA 公钥 PEM，同时兼容 SPKI（BEGIN PUBLIC KEY）
// 与 PKCS#1（BEGIN RSA PUBLIC KEY）两种格式。
func ParsePublicKeyPEM(pemStr string) (*rsa.PublicKey, error) {
	trimmed := strings.TrimSpace(pemStr)
	if trimmed == "" || strings.Contains(trimmed, publicKeyPlaceholder) {
		return nil, errors.New("公钥未配置或仍是占位符")
	}
	block, _ := pem.Decode([]byte(trimmed))
	if block == nil {
		return nil, errors.New("不是有效的 PEM 数据，请确认包含 BEGIN/END PUBLIC KEY 两行")
	}
	if pub, err := x509.ParsePKIXPublicKey(block.Bytes); err == nil {
		rsaPub, ok := pub.(*rsa.PublicKey)
		if !ok {
			return nil, errors.New("公钥类型错误：需要 RSA 公钥")
		}
		return rsaPub, nil
	}
	if rsaPub, err := x509.ParsePKCS1PublicKey(block.Bytes); err == nil {
		return rsaPub, nil
	}
	return nil, errors.New("解析公钥失败：既不是 PKIX 也不是 PKCS#1 格式")
}

// ============================================================================
// 环境变量
// ============================================================================

func resolvePath() string {
	if p := strings.TrimSpace(os.Getenv("LICENSE_PATH")); p != "" {
		return p
	}
	return DefaultLicensePath
}

func resolvePublicKey() string {
	if p := strings.TrimSpace(os.Getenv("LICENSE_PUBLIC_KEY")); p != "" {
		return p
	}
	return publicKeyPEM
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
// 比较一律用绝对时刻（不受时区影响），只有给人看的文案才转本地时区。
func formatLocal(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Local().Format("2006-01-02 15:04")
}
