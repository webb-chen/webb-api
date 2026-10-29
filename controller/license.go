package controller

import (
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/pkg/license"

	"github.com/gin-gonic/gin"
)

// maxLicenseFileSize 是授权文件的大小上限。
// 一份正常的 .lic 文件只有几百字节，这里留足余量，
// 同时防止有人把大文件当授权上传占满磁盘。
const maxLicenseFileSize = 1 << 20

// GetLicenseStatus 返回当前授权状态，供前端「授权管理」页面展示。
//
// GET /api/license/status
//
// 授权无效时仍返回 HTTP 200，用 valid:false + code 表达失败原因，
// 这样前端能拿到具体原因，而不是一个没有信息的 403。
func GetLicenseStatus(c *gin.Context) {
	st := license.Current()
	c.JSON(http.StatusOK, licenseStateBody(st, true))
}

// GetLicenseMachineCode 返回本机统一机器码（规范 §5 算法）。
// 客户把该机器码交给 ULP 管理员即可签发绑定本机的授权。
//
// GET /api/license/machine-code
func GetLicenseMachineCode(c *gin.Context) {
	code := license.MachineCode()
	c.JSON(http.StatusOK, gin.H{
		"success":      true,
		"code":         license.CodeOK,
		"machine_code": code,
		"data": gin.H{
			"machine_code": code,
		},
	})
}

// UploadLicense 接收前端上传的 .lic 文件，校验通过后才落盘生效。
//
// POST /api/license/upload  (multipart/form-data, 字段名 file)
//
// 采用「先完整校验、再备份、临时文件、原子替换」的顺序（不变量 6）：
// 如果先写后校验，一个格式错误的文件就会覆盖掉当前仍然有效的授权，
// 把客户直接锁在系统外面。
func UploadLicense(c *gin.Context) {
	fileHeader, err := c.FormFile("file")
	if err != nil {
		respondLicenseFailure(c, license.CodeMalformed, "请选择要上传的授权文件（表单字段名应为 file）")
		return
	}
	if fileHeader.Size <= 0 {
		respondLicenseFailure(c, license.CodeMalformed, "授权文件为空，请重新选择")
		return
	}
	if fileHeader.Size > maxLicenseFileSize {
		respondLicenseFailure(c, license.CodeMalformed, "授权文件过大，请确认上传的是 .lic 授权文件")
		return
	}

	opened, err := fileHeader.Open()
	if err != nil {
		respondLicenseFailure(c, license.CodeMalformed, "读取上传文件失败: "+err.Error())
		return
	}
	defer opened.Close()

	content, err := io.ReadAll(io.LimitReader(opened, maxLicenseFileSize+1))
	if err != nil {
		respondLicenseFailure(c, license.CodeMalformed, "读取上传文件失败: "+err.Error())
		return
	}
	if len(content) > maxLicenseFileSize {
		respondLicenseFailure(c, license.CodeMalformed, "授权文件过大，请确认上传的是 .lic 授权文件")
		return
	}

	st, err := license.Install(content)
	if err != nil {
		code := license.CodeMalformed
		var le *license.Error
		if errors.As(err, &le) {
			code = le.Code
		}
		logger.LogWarn(c.Request.Context(), "授权文件安装失败: "+err.Error())
		// 与项目其他接口的业务失败契约保持一致：HTTP 200 + success:false，
		// 让前端直接展示具体原因（签名不对 / 产品不符 / 机器不符等）。
		respondLicenseFailure(c, code, "授权文件校验失败: "+err.Error())
		return
	}

	logger.LogInfo(c.Request.Context(), "授权文件更新成功，操作人: "+licenseOperatorName(c)+
		"，状态码: "+st.Code)
	c.JSON(http.StatusOK, licenseStateBody(st, true))
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
	c.JSON(http.StatusOK, licenseStateBody(st, true))
}

// licenseStateBody 把授权状态转换为统一响应结构。
//
// 顶层字段与规范 §6 完全一致；同时保留 success/data 以兼容本项目既有
// {success, message, data} 契约。status / upload / reload 三个接口共用，
// 保证前端拿到的字段始终一致。
func licenseStateBody(st license.State, success bool) gin.H {
	payload := licensePayloadAPI(st.License)
	data := gin.H{
		"code":           st.Code,
		"valid":          st.Valid,
		"message":        st.Message,
		"in_grace":       st.InGrace,
		"remaining_days": st.RemainingDays,
		"checked_at":     st.CheckedAt.UTC().Format(time.RFC3339),
		"license":        payload,
	}
	body := gin.H{
		"success": success,
		"message": st.Message,
		"data":    data,
	}
	for k, v := range data {
		body[k] = v
	}
	return body
}

// respondLicenseFailure 返回 HTTP 200 + 统一状态码 + 可读原因。
// 不能直接覆盖旧授权，所以这里只报告校验失败，不落盘。
func respondLicenseFailure(c *gin.Context, code string, message string) {
	body := gin.H{
		"success":        false,
		"code":           code,
		"valid":          false,
		"message":        message,
		"in_grace":       false,
		"remaining_days": 0,
		"checked_at":     time.Now().UTC().Format(time.RFC3339),
		"license":        nil,
		"data": gin.H{
			"code":           code,
			"valid":          false,
			"message":        message,
			"in_grace":       false,
			"remaining_days": 0,
			"checked_at":     time.Now().UTC().Format(time.RFC3339),
		},
	}
	c.JSON(http.StatusOK, body)
}

// licensePayloadAPI 输出统一载荷字段。永久授权的 expire_time 为 null。
func licensePayloadAPI(p *license.Payload) gin.H {
	if p == nil {
		return nil
	}
	var issuedAt interface{}
	if p.IssuedAt != nil {
		issuedAt = p.IssuedAt.UTC().Format(time.RFC3339)
	}
	var expireAt interface{}
	if p.ExpireTime != nil {
		expireAt = p.ExpireTime.UTC().Format(time.RFC3339)
	}
	return gin.H{
		"license_id":       p.LicenseID,
		"product":          p.Product,
		"product_display":  p.ProductDisplay,
		"customer":         p.Customer,
		"customer_contact": p.CustomerContact,
		"issued_at":        issuedAt,
		"expire_time":      expireAt,
		"grace_days":       p.GraceDays,
		"machine_code":     p.MachineCode,
		"instance_id":      p.InstanceID,
		"features":         p.Features,
		"permanent":        p.Permanent(),
	}
}

// licenseOperatorName 返回触发授权操作的用户，未登录时返回 anonymous。
// 授权接口本身不走登录鉴权（恢复通路），这里只用于审计日志。
func licenseOperatorName(c *gin.Context) string {
	if name := c.GetString("username"); name != "" {
		return name
	}
	if id := c.GetInt("id"); id > 0 {
		return "user#" + strconv.Itoa(id)
	}
	return "anonymous"
}
