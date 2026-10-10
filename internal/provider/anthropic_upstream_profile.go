package provider

import (
	"github.com/zerone-agents/jev-model-router/internal/routing"
	"net/url"
	"strings"
)

// BigModel's tested GLM endpoints accept thinking/tool replay without their
// response signature. Do not infer this for Claude, proxies, or other models.
func nativeSignatureOptional(t routing.Target) bool {
	if t.Provider.EffectiveProtocol() != routing.ProtocolAnthropic {
		return false
	}
	u, err := url.Parse(t.Provider.BaseURL)
	if err != nil || u.Scheme != "https" || u.Hostname() != "open.bigmodel.cn" || (u.Port() != "" && u.Port() != "443") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || strings.TrimRight(u.EscapedPath(), "/") != "/api/anthropic/v1" {
		return false
	}
	return t.Model.UpstreamName == "glm-5.3" || t.Model.UpstreamName == "glm-5.3-flash"
}
