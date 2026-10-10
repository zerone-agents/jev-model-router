package routing

import "errors"

// Diagnostic contains only bounded, locally defined summaries, never provider
// messages, SDK diagnostics, URLs, credentials or request content.
type Diagnostic struct {
	Code           string `json:"code"`
	Message        string `json:"message"`
	UpstreamStatus int    `json:"upstream_status,omitempty"`
	UpstreamCode   string `json:"upstream_code,omitempty"`
}

func Diagnose(err error) Diagnostic {
	d := Diagnostic{Code: "internal_error", Message: "Request failed"}
	var up *UpstreamError
	if errors.As(err, &up) {
		d.Code, d.Message = "upstream_error", "Generation provider failed"
		if up.Status >= 400 && up.Status <= 599 {
			d.UpstreamStatus = up.Status
		}
		switch up.Status {
		case 400, 413, 422:
			d.Code, d.Message = "upstream_invalid_request", "Provider rejected the request"
		case 401:
			d.Code, d.Message = "upstream_authentication", "Provider authentication failed"
		case 402:
			d.Code, d.Message = "upstream_quota", "Provider billing or quota limit reached"
		case 403:
			d.Code, d.Message = "upstream_permission", "Provider denied access"
		case 404:
			d.Code, d.Message = "upstream_not_found", "Provider model or endpoint unavailable"
		case 429:
			d.Code, d.Message = "upstream_rate_limit", "Provider rate limit reached"
		case 500, 502, 503, 529:
			d.Code, d.Message = "upstream_unavailable", "Provider service unavailable"
		case 504:
			d.Code, d.Message = "timeout", "Provider request timed out"
		case 499:
			d.Code, d.Message = "cancelled", "Request cancelled"
		}
		for _, field := range []string{"code", "type"} {
			v := up.Body[field]
			value, _ := v.(string)
			if ptr, ok := v.(*string); ok && ptr != nil {
				value = *ptr
			}
			switch value {
			case "insufficient_quota", "billing_error":
				d.Code, d.Message = "upstream_quota", "Provider billing or quota limit reached"
			case "content_filter", "content_policy_violation":
				d.Code, d.Message = "upstream_content_rejected", "Provider rejected the content"
			case "authentication_error", "invalid_api_key":
				d.Code, d.Message = "upstream_authentication", "Provider authentication failed"
			case "rate_limit_error", "rate_limit_exceeded":
				d.Code, d.Message = "upstream_rate_limit", "Provider rate limit reached"
			default:
				continue
			}
			d.UpstreamCode = value
			break
		}
		return d
	}
	var known *Error
	if errors.As(err, &known) {
		d.Code = known.Code
		d.Message = "Request failed (" + known.Code + ")"
	}
	return d
}
