package provider

import (
	"github.com/zerone-agents/jev-model-router/internal/routing"
	"net/url"
	"strings"
)

// BigModel's tested GLM endpoints accept thinking/tool replay without their
// response signature. Do not infer this for Claude, proxies, or other models.
func officialBigModel(t routing.Target) bool {
	if t.Provider.EffectiveProtocol() != routing.ProtocolAnthropic {
		return false
	}
	u, err := url.Parse(t.Provider.BaseURL)
	if err != nil || u.Scheme != "https" || u.Hostname() != "open.bigmodel.cn" || (u.Port() != "" && u.Port() != "443") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || strings.TrimRight(u.EscapedPath(), "/") != "/api/anthropic/v1" {
		return false
	}
	return true
}

func nativeSignatureOptional(t routing.Target) bool {
	return officialBigModel(t) && (t.Model.UpstreamName == "glm-5.3" || t.Model.UpstreamName == "glm-5.3-flash")
}

// chatSignatureOptional applies to the lossy Chat protocol only. Native
// Messages keeps its own validation and preserves signatures on the wire.
func chatSignatureOptional(t routing.Target) bool {
	if !officialBigModel(t) {
		return false
	}
	if t.Model.ChatUnsignedThinking != nil {
		return *t.Model.ChatUnsignedThinking
	}
	if t.Provider.ChatUnsignedThinking != nil {
		return *t.Provider.ChatUnsignedThinking
	}
	return nativeSignatureOptional(t)
}
