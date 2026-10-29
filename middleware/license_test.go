package middleware

import "testing"

// TestIsLicenseGuardedPath_RelayPaths 覆盖所有对外转发入口，
// 包括 /:mode/mj 形式的 Midjourney 兼容路由（如 /fast/mj/submit/imagine）。
func TestIsLicenseGuardedPath_RelayPaths(t *testing.T) {
	guarded := []string{
		"/api",
		"/api/channel/",
		"/v1/chat/completions",
		"/v1beta/models/gemini-pro:generateContent",
		"/mj/submit/imagine",
		"/fast/mj/submit/imagine",
		"/relax/mj/task/123/fetch",
		"/pg/chat/completions",
		"/dashboard/billing/usage",
	}
	for _, path := range guarded {
		if !isLicenseGuardedPath(path) {
			t.Fatalf("路径 %q 应受授权保护", path)
		}
	}
}

// TestIsLicenseGuardedPath_PublicPaths 确保前端页面与静态资源不会被误拦。
// 注意 /api/license/status、/api/status 等虽然命中了 /api 前缀（guarded），
// 但会由 isLicenseRecoveryPath 放行，属于另一层判断。
func TestIsLicenseGuardedPath_PublicPaths(t *testing.T) {
	public := []string{
		"/",
		"/license",
		"/sign-in",
		"/static/js/index.js",
		"/favicon.ico",
		"/healthz",
	}
	for _, path := range public {
		if isLicenseGuardedPath(path) {
			t.Fatalf("路径 %q 不应被当作业务路径", path)
		}
	}
	for _, path := range []string{"/api/license/status", "/api/status", "/api/user/login"} {
		if !isLicenseRecoveryPath(path) {
			t.Fatalf("路径 %q 应属于恢复通路", path)
		}
	}
}

// TestIsRelayPath_MidjourneyMode 确认 /:mode/mj 的 403 也使用 OpenAI 风格错误体。
func TestIsRelayPath_MidjourneyMode(t *testing.T) {
	for _, path := range []string{"/fast/mj/submit/imagine", "/relax/mj/task/1/fetch"} {
		if !isRelayPath(path) {
			t.Fatalf("路径 %q 应按转发类错误体返回", path)
		}
	}
	if isRelayPath("/api/channel/") {
		t.Fatal("/api/channel/ 不是转发类路径")
	}
}
