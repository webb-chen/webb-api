package controller

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/pkg/license"

	"github.com/gin-gonic/gin"
)

// maxLicenseFileSize 是授权文件的大小上限。
// 一份正常的 license.lic 只有几百字节，这里留足余量，
// 同时防止有人把大文件当授权上传占满磁盘。
const maxLicenseFileSize = 1 << 20

// GetLicenseStatus 返回当前授权状态，供前端「授权管理」页面展示。
//
// GET /api/license/status
//
// 注意：本接口在授权失效时依然可以访问（见 middleware/license.go 的放行清单），
// 否则客户到期后连"当前是什么状态"都查不到，更无从续期。
func GetLicenseStatus(c *gin.Context) {
	st := license.Current()
	data := licenseStateToAPI(st)
	data["checked_at"] = st.CheckedAt.Format(time.RFC3339)
	common.ApiSuccess(c, data)
}

// machineCodeCache 缓存机器指纹：指纹只依赖硬件/系统标识，进程生命周期内不会变化，
// 无需在每次请求时重复读文件和做哈希。
var (
	machineCodeOnce sync.Once
	machineCodeVal  string
)

// GetLicenseMachineCode 返回本机的机器指纹（机器码），供授权页展示、
// 便于技术支持识别具体部署实例。
//
// GET /api/license/machine-code
//
// 指纹来源（按优先级）：/etc/machine-id（主流 Linux）→
// /var/lib/dbus/machine-id（旧发行版）→ 主机名 + 首个网卡 MAC（兜底）。
// 输出为 SHA-256 截断后的 16 位十六进制，形如 A1B2-C3D4-E5F6-0789。
func GetLicenseMachineCode(c *gin.Context) {
	common.ApiSuccess(c, gin.H{"machine_code": MachineCode()})
}

// MachineCode 计算并缓存机器指纹。
func MachineCode() string {
	machineCodeOnce.Do(func() {
		machineCodeVal = formatMachineCode(rawMachineIdentity())
	})
	return machineCodeVal
}

// rawMachineIdentity 返回原始机器标识串。
func rawMachineIdentity() string {
	for _, path := range []string{"/etc/machine-id", "/var/lib/dbus/machine-id"} {
		if b, err := os.ReadFile(path); err == nil {
			if id := strings.TrimSpace(string(b)); id != "" {
				return id
			}
		}
	}
	// 兜底：主机名 + 首个非回环网卡的 MAC 地址
	host, err := os.Hostname()
	if err != nil {
		host = "unknown-host"
	}
	return host + "/" + firstMAC()
}

// firstMAC 返回首个非回环网卡的硬件地址；取不到时返回空串。
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

// formatMachineCode 对原始标识做 SHA-256，取前 16 位十六进制并按 4 位分组。
func formatMachineCode(raw string) string {
	sum := sha256.Sum256([]byte("webb-api-license:" + raw))
	hexStr := strings.ToUpper(hex.EncodeToString(sum[:8]))
	var b strings.Builder
	for i := 0; i < len(hexStr); i++ {
		if i > 0 && i%4 == 0 {
			b.WriteByte('-')
		}
		b.WriteByte(hexStr[i])
	}
	return b.String()
}

// UploadLicense 接收前端上传的 .lic 文件，校验通过后才落盘生效。
//
// POST /api/license/upload  (multipart/form-data, 字段名 file)
//
// 这里刻意采用「先完整校验、再写入」的顺序（具体实现在 license.Install 内）：
// 如果先写后校验，一个格式错误的文件就会覆盖掉当前仍然有效的授权，
// 把客户直接锁在系统外面。
func UploadLicense(c *gin.Context) {
	fileHeader, err := c.FormFile("file")
	if err != nil {
		common.ApiErrorMsg(c, "请选择要上传的授权文件（表单字段名应为 file）")
		return
	}
	if fileHeader.Size <= 0 {
		common.ApiErrorMsg(c, "授权文件为空，请重新选择")
		return
	}
	if fileHeader.Size > maxLicenseFileSize {
		common.ApiErrorMsg(c, "授权文件过大，请确认上传的是 .lic 授权文件")
		return
	}

	opened, err := fileHeader.Open()
	if err != nil {
		common.ApiErrorMsg(c, "读取上传文件失败: "+err.Error())
		return
	}
	defer opened.Close()

	content, err := io.ReadAll(io.LimitReader(opened, maxLicenseFileSize+1))
	if err != nil {
		common.ApiErrorMsg(c, "读取上传文件失败: "+err.Error())
		return
	}
	if len(content) > maxLicenseFileSize {
		common.ApiErrorMsg(c, "授权文件过大，请确认上传的是 .lic 授权文件")
		return
	}

	st, err := license.Install(content)
	if err != nil {
		logger.LogWarn(c.Request.Context(), "授权文件安装失败: "+err.Error())
		// 返回 HTTP 200 + success:false，与项目其他接口的业务失败契约保持一致，
		// 前端可统一用 handleServerError 展示具体原因。
		common.ApiErrorMsg(c, "授权文件校验失败: "+err.Error())
		return
	}

	expire := ""
	if st.License != nil {
		expire = formatLicenseDate(st.License.ExpireTime)
	}
	logger.LogInfo(c.Request.Context(), "授权文件更新成功，操作人: "+licenseOperatorName(c)+
		"，新到期时间: "+expire+"，状态码: "+st.Code)

	common.ApiSuccess(c, licenseStateToAPI(st))
}

// ReloadLicense 强制立即重新读取授权文件，不等待后台定时复检。
//
// POST /api/license/reload
//
// 适用于运维直接把新的 license.lic 放到服务器上（而不是走页面上传）的场景。
func ReloadLicense(c *gin.Context) {
	st := license.Reload()
	logger.LogInfo(c.Request.Context(), "手动触发授权重载，操作人: "+licenseOperatorName(c)+
		"，结果状态码: "+st.Code)
	common.ApiSuccess(c, licenseStateToAPI(st))
}

// licenseStateToAPI 把授权状态转换为接口响应结构。
// status / upload / reload 三个接口共用，保证前端拿到的字段始终一致，
// 不必为不同接口写三套解析逻辑。
func licenseStateToAPI(st license.State) gin.H {
	data := gin.H{
		"enforced":       license.Enforced(),
		"valid":          st.Valid,
		"code":           st.Code,
		"message":        st.Message,
		"in_grace":       st.InGrace,
		"remaining_days": st.RemainingDays,
		"path":           license.Path(),
	}

	lic := st.License
	if lic == nil {
		return data
	}

	data["license"] = gin.H{
		"product":     lic.Product,
		"customer":    lic.Customer,
		"issued_at":   formatLicenseDate(lic.IssuedAt),
		"expire_time": formatLicenseDate(lic.ExpireTime),
		"grace_days":  lic.GraceDays,
	}
	return data
}

// formatLicenseDate 以本地时区输出便于人读的日期。
// 仅用于展示；所有到期比较都使用 UTC 绝对时刻，不受服务器时区影响。
func formatLicenseDate(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Local().Format("2006-01-02 15:04")
}

// licenseOperatorName 从上下文取当前登录用户名，用于审计日志。
// 取不到用户名时回退到用户 ID，仍取不到则返回 unknown。
// 授权变更属于敏感操作，留痕比美观重要，因此任何情况下都不返回空字符串。
func licenseOperatorName(c *gin.Context) string {
	if name := c.GetString("username"); name != "" {
		return name
	}
	if id := c.GetInt("id"); id != 0 {
		return "user#" + strconv.Itoa(id)
	}
	return "unknown"
}
