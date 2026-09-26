package gitconfig

import (
	"regexp"
	"strings"
)

// MaskedValue 命中脱敏词表的条目值的统一替换文本（前端与复制件同口径）。
const MaskedValue = "[已脱敏]"

var (
	// credentialURLRe 匹配 URL 内嵌凭据形态 scheme://user:pass@host（userinfo 段带冒号密码）。
	// 刻意不匹配 scp 式 git@github.com:user/repo.git（无密码段），普通远程地址原样保留。
	credentialURLRe = regexp.MustCompile(`[a-zA-Z][a-zA-Z0-9+.-]*://[^/\s:@]+:[^@\s]+@`)
	// gitTokenValueRe 匹配 GitHub（ghp_/gho_/ghu_/ghs_/ghr_/github_pat_）与 GitLab（glpat-）
	// token 形态前缀 + 长随机串，防止凭据被塞进任意键的值里漏网。
	gitTokenValueRe = regexp.MustCompile(`(?:gh[pousr]_[A-Za-z0-9]{16,}|github_pat_[A-Za-z0-9_]{16,}|glpat-[A-Za-z0-9_-]{16,})`)
	// secretKeyTailRe 匹配键名以秘密语义词收尾（对应 git config --list 原始行 `…(token|password|…)=` 形态）。
	secretKeyTailRe = regexp.MustCompile(`(?i)(token|passwd|password|secret|api[_-]?key|pat)$`)
	// authorizationValueRe 匹配携带凭据的 HTTP 头值（如 http.extraheader 里的 Authorization: Bearer/Basic）。
	authorizationValueRe = regexp.MustCompile(`(?i)\bauthorization:`)
)

// Redact 按脱敏词表就地打码单条配置。返回脱敏后的键与值——两者均可安全出网与渲染：
//  1. user.email 键豁免：邮箱是标识非秘密，值原样（但键名里若内嵌凭据 URL 仍走规则 3）；
//  2. 值命中 内嵌凭据 URL / GitHub 等 token 形态 / Authorization 头，或键名命中
//     proxy / token|password|passwd|secret|apikey|pat 收尾 → 值替换为 MaskedValue；
//  3. 键名自身内嵌凭据 URL（如 url.https://user:pass@host/.insteadof 家族）→ 键也替换为 MaskedValue。
//
// credential.helper 的值（manager/osxkeychain 等）本身不是秘密，天然不命中任何规则，原样保留。
func Redact(key, value string) (string, string) {
	lowerKey := strings.ToLower(key)
	keyHasCredential := credentialURLRe.MatchString(key)
	valueHasSecret := credentialURLRe.MatchString(value) ||
		gitTokenValueRe.MatchString(value) ||
		authorizationValueRe.MatchString(value)
	keyHasSecret := keyHasCredential ||
		strings.Contains(lowerKey, "proxy") ||
		secretKeyTailRe.MatchString(lowerKey)
	if !valueHasSecret && !keyHasCredential && strings.Contains(lowerKey, "user.email") {
		return key, value // 豁免仅护住键名误伤；值里真有凭据形态照打
	}
	if valueHasSecret || keyHasSecret {
		value = MaskedValue
	}
	if keyHasCredential {
		key = MaskedValue
	}
	return key, value
}
