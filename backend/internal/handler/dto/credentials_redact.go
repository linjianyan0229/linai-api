// Package dto provides data transfer objects for HTTP handlers.
package dto

import "github.com/Wei-Shaw/sub2api/internal/service"

// RedactCredentials 复制一份 in，剥离 service.SensitiveCredentialKeys 列出的所有敏感子键，
// 并产出 has_<key> 存在状态及 api_key_count，不返回任何 Key 原文。
//
// 输入 nil 时返回 nil, nil（避免响应里出现空对象）。
// 不修改入参；调用方拿到的 out 可安全序列化进 JSON 返回前端。
func RedactCredentials(in map[string]any) (out map[string]any, status map[string]any) {
	if in == nil {
		return nil, nil
	}
	out = make(map[string]any, len(in))
	for k, v := range in {
		if k == "api_keys_mode" {
			continue
		}
		if service.IsSensitiveCredentialKey(k) {
			if isCredentialValuePresent(v) {
				if status == nil {
					status = make(map[string]any, 4)
				}
				status["has_"+k] = true
			}
			continue
		}
		out[k] = v
	}
	_, hasSingleKey := in["api_key"]
	_, hasKeyPool := in["api_keys"]
	if hasSingleKey || hasKeyPool {
		keyCount := len(service.AccountAPIKeys(in))
		if status == nil {
			status = make(map[string]any, 3)
		}
		status["has_api_key"] = keyCount > 0
		status["api_key_count"] = keyCount
	}
	return out, status
}

// isCredentialValuePresent 判断值是否"存在且非零"。空字符串、nil、false 均视为未配置；
// 其余非零类型（数字、对象、字符串等）视为已配置。
func isCredentialValuePresent(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case string:
		return x != ""
	case bool:
		return x
	default:
		return true
	}
}
