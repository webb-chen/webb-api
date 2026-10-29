package middleware

import (
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/pkg/license"

	"github.com/gin-gonic/gin"
)

// 本中间件负责在授权失效时拦停业务能力，同时**始终保留**一条恢复通路：
// 客户必须还能打开管理后台、登录、查看授权状态、上传新的 license.lic。
//
// 设计要点：采用「只拦业务能力、其余一律放行」的黑名单思路，
// 而不是「全部拦截、按白名单放行」。原因是前端是单页应用（SPA），
// 页面路径由前端路由自由定义（/sign-in、/license、/dashboard/... 数量开放），
// 白名单无法枚举完整；一旦漏放行静态页面，授权到期后客户连登录页都打不开，
// 系统被彻底锁死，只能重装。宁可少拦一个接口，也不能锁死恢复通路。

// licenseGuardPrefixes 是需要受授权控制的路径前缀。
//
// 这里覆盖的是系统真正对外提供 AI 能力的入口。
// 特别注意：这些路径大多不在 /api 之下（例如 /v1/chat/completions），
// 如果只拦 /api，授权到期后客户仍能持 API Key 正常调用转发接口，授权锁形同虚设。
var licenseGuardPrefixes = []string{
	"/api",       // 管理后台全部接口
	"/v1",        // OpenAI 兼容转发、视频、异步任务、WebSocket
	"/v1beta",    // Gemini 兼容转发
	"/mj",        // Midjourney 中转
	"/pg",        // Playground
	"/dashboard", // 旧版计费查询接口
	"/sockjs",    // 实时协同（如启用）
}

// licenseRecoveryPaths 是授权失效时仍必须放行的路径（恢复通路）。
// 每一条都对应"客户自救"的必要环节，删掉任何一条都可能造成无法恢复。
var licenseRecoveryPaths = []string{
	// 授权管理自身：查状态、查机器码、上传新授权、手动重载。
	"/api/license/",

	// 登录与会话：授权到期后仍需登录进后台才能上传新授权。
	"/api/user/login",
	"/api/user/logout",
	"/api/user/auth/refresh",
	"/api/user/self",
	"/api/user/register",
	"/api/oauth/",

	// 系统状态与初始化探测：前端启动依赖这些接口渲染页面，且不含任何业务能力。
	"/api/status",
	"/api/setup",
	"/api/notice",
	"/api/about",
	"/api/health",
	"/health",
	"/healthz",
}

// LicenseAuth 是全局授权校验中间件。
//
// 它只读取内存中缓存的授权状态（由 pkg/license 的后台协程定时刷新），
// 不在请求路径上做文件 IO 或 RSA 验签，因此对高并发转发几乎没有性能影响。
func LicenseAuth() func(c *gin.Context) {
	return func(c *gin.Context) {
		path := c.Request.URL.Path

		// 不属于业务能力路径的一律放行：前端页面、静态资源等。
		if !isLicenseGuardedPath(path) {
			c.Next()
			return
		}

		// 恢复通路无条件放行，保证客户始终能上传新授权。
		if isLicenseRecoveryPath(path) {
			c.Next()
			return
		}

		st := license.Current()

		// 不变量 4：授权模块初始化失败时绝不静默放行，返回 503「服务未就绪」。
		if st.Code == license.CodeNotInit || st.Code == license.CodeNoPublicKey {
			abortWithLicenseError(c, st, http.StatusServiceUnavailable)
			return
		}

		if st.Valid {
			c.Next()
			return
		}

		abortWithLicenseError(c, st, http.StatusForbidden)
	}
}

// isLicenseGuardedPath 判断路径是否属于受授权控制的业务能力路径。
// 精确匹配前缀本身或其子路径，避免把 /v1abc 这类无关路径误判进来。
func isLicenseGuardedPath(path string) bool {
	for _, prefix := range licenseGuardPrefixes {
		if path == prefix || strings.HasPrefix(path, prefix+"/") {
			return true
		}
	}
	// Midjourney 另有一组兼容路由注册在 /:mode/mj 下（如 /fast/mj/submit/imagine、
	// /relax/mj/task/:id/fetch）。这类路径不以 /mj 开头，必须单独识别，
	// 否则授权到期后仍可经该前缀调用转发能力。
	if isMidjourneyModePath(path) {
		return true
	}
	return false
}

// isMidjourneyModePath 判断是否为 /{mode}/mj 形式的 Midjourney 兼容路径。
// 仅匹配两段式前缀，普通路径（如 /foo/bar）不会命中。
func isMidjourneyModePath(path string) bool {
	trimmed := strings.TrimPrefix(path, "/")
	slash := strings.IndexByte(trimmed, '/')
	if slash <= 0 {
		return false
	}
	rest := trimmed[slash+1:]
	return rest == "mj" || strings.HasPrefix(rest, "mj/")
}

// isLicenseRecoveryPath 判断路径是否属于授权失效时的恢复通路。
func isLicenseRecoveryPath(path string) bool {
	for _, prefix := range licenseRecoveryPaths {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

// abortWithLicenseError 按调用方类型返回对应风格的错误响应。
//
// 转发类接口（/v1、/v1beta、/mj、/pg）的调用方通常是 OpenAI SDK 或第三方程序，
// 它们只认 OpenAI 的 error 结构；若返回本项目自定义的 {success,message}，
// 对方 SDK 会报出难以理解的解析错误，客户排查时看不到真实原因。
// 管理后台接口走 /api，则沿用项目统一的 {success,message} 契约，
// 并额外带上统一状态码 code 供前端识别"这是授权问题，应跳转授权页面"。
func abortWithLicenseError(c *gin.Context, st license.State, status int) {
	code := st.Code
	if code == "" {
		code = license.CodeNotInit
	}
	if isRelayPath(c.Request.URL.Path) {
		c.AbortWithStatusJSON(status, gin.H{
			"error": gin.H{
				"message": st.Message,
				"type":    "license_error",
				"code":    code,
			},
			// 顶层也带一份 code，便于统一的错误采集与前端判据。
			"code":    code,
			"message": st.Message,
		})
		return
	}

	c.AbortWithStatusJSON(status, gin.H{
		"success":        false,
		"code":           code,
		"valid":          false,
		"message":        st.Message,
		"in_grace":       st.InGrace,
		"remaining_days": st.RemainingDays,
	})
}

// relayPathPrefixes 是对外提供 AI 能力的转发类路径。
var relayPathPrefixes = []string{"/v1", "/v1beta", "/mj", "/pg"}

// isRelayPath 判断是否为转发类路径，决定错误响应采用哪种结构。
func isRelayPath(path string) bool {
	for _, prefix := range relayPathPrefixes {
		if path == prefix || strings.HasPrefix(path, prefix+"/") {
			return true
		}
	}
	return isMidjourneyModePath(path)
}
