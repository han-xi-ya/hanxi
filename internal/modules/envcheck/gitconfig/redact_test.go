package gitconfig

import "testing"

// TestRedact 表驱动锁定脱敏词表：红（必须打码）绿（必须原样）两路用例都覆盖，
// 绿路防误杀（普通 user.name、email、credential.helper、scp 式远程地址），
// 红路防泄露（内嵌凭据 URL、GitHub token 形态、proxy/秘密键名、Authorization 头）。
func TestRedact(t *testing.T) {
	tests := []struct {
		name    string
		key     string
		value   string
		wantKey string
		wantVal string
	}{
		// ---- 绿：原样保留 ----
		{name: "user.name 普通值", key: "user.name", value: "hanxi", wantKey: "user.name", wantVal: "hanxi"},
		{name: "user.email 保留", key: "user.email", value: "hanxi@example.com", wantKey: "user.email", wantVal: "hanxi@example.com"},
		{name: "大写键 email 保留", key: "User.Email", value: "Dev@Example.COM", wantKey: "User.Email", wantVal: "Dev@Example.COM"},
		{name: "credential.helper 值原样", key: "credential.helper", value: "manager", wantKey: "credential.helper", wantVal: "manager"},
		{name: "scp 式远程地址不误杀", key: "remote.origin.url", value: "git@github.com:hanxi/hanxi.git", wantKey: "remote.origin.url", wantVal: "git@github.com:hanxi/hanxi.git"},
		{name: "无凭据 https 远程保留", key: "remote.upstream.url", value: "https://github.com/git/git.git", wantKey: "remote.upstream.url", wantVal: "https://github.com/git/git.git"},
		{name: "core.* 常规值保留", key: "core.autocrlf", value: "true", wantKey: "core.autocrlf", wantVal: "true"},
		{name: "editor 值保留", key: "core.editor", value: "vim", wantKey: "core.editor", wantVal: "vim"},
		{name: "init.defaultbranch 保留", key: "init.defaultbranch", value: "main", wantKey: "init.defaultbranch", wantVal: "main"},

		// ---- 红：值打码 ----
		{
			name: "值内嵌凭据 URL", key: "url.my.insteadof", value: "https://alice:s3cr3t-pa55@gitee.com/alice/repo.git",
			wantKey: "url.my.insteadof", wantVal: MaskedValue,
		},
		{
			name: "ghp_ 前缀 token 值", key: "github.user", value: "ghp_aBcDeFgHiJkLmNoPqRsTuVwXyZ0123456789",
			wantKey: "github.user", wantVal: MaskedValue,
		},
		{
			name: "github_pat_ 细粒度 token", key: "secrets.note", value: "see github_pat_11ABCDEFG0abcdefABCDEFG_abcdefghijklmnop",
			wantKey: "secrets.note", wantVal: MaskedValue,
		},
		{
			name: "gho_ OAuth token", key: "helper.gh", value: "gho_1234567890abcdefghij1234567890abcdefghij",
			wantKey: "helper.gh", wantVal: MaskedValue,
		},
		{
			name: "glpat GitLab token", key: "gitlab.token", value: "glpat-abcdefghij1234567890AB",
			wantKey: "gitlab.token", wantVal: MaskedValue,
		},
		{
			name: "键名 http.proxy 打码值", key: "http.proxy", value: "http://127.0.0.1:7890", wantKey: "http.proxy", wantVal: MaskedValue,
		},
		{
			name: "键名 https.proxy 打码值", key: "HTTPS.PROXY", value: "socks5://localhost:1080", wantKey: "HTTPS.PROXY", wantVal: MaskedValue,
		},
		{
			name: "键名以 password 收尾", key: "credential.https://dev.azure.com/x/.password", value: "hunter2",
			wantKey: "credential.https://dev.azure.com/x/.password", wantVal: MaskedValue,
		},
		{
			name: "键名以 token 收尾", key: "github.token", value: "anything-goes-here", wantKey: "github.token", wantVal: MaskedValue,
		},
		{
			name: "键名 apikey 收尾", key: "review.apikey", value: "abc123", wantKey: "review.apikey", wantVal: MaskedValue,
		},
		{
			name: "键名 pat 收尾", key: "github.pat", value: "abc123", wantKey: "github.pat", wantVal: MaskedValue,
		},
		{
			name: "键名 secret 收尾", key: "ci.webhook.secret", value: "s=1", wantKey: "ci.webhook.secret", wantVal: MaskedValue,
		},
		{
			name: "Authorization 头值", key: "http.extraheader", value: "Authorization: Bearer zzzzzzzzzzzzzzzzzzzzzz",
			wantKey: "http.extraheader", wantVal: MaskedValue,
		},
		{
			name: "键内嵌凭据 URL 键值双打码", key: "url.https://bob:hun7er2@github.com/.insteadof", value: "https://github.com/",
			wantKey: MaskedValue, wantVal: MaskedValue,
		},
		{
			name: "user.email 值真有凭据形态照打", key: "user.email", value: "https://e:p@x", wantKey: "user.email", wantVal: MaskedValue,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotKey, gotVal := Redact(tt.key, tt.value)
			if gotKey != tt.wantKey || gotVal != tt.wantVal {
				t.Fatalf("Redact(%q, %q) = (%q, %q), want (%q, %q)", tt.key, tt.value, gotKey, gotVal, tt.wantKey, tt.wantVal)
			}
		})
	}
}

// TestRedactIdempotent 脱敏幂等：已打码的件再过一遍词表不会被二次改动。
func TestRedactIdempotent(t *testing.T) {
	k, v := Redact("http.proxy", MaskedValue)
	if v != MaskedValue {
		t.Fatalf("value = %q", v)
	}
	k2, v2 := Redact(k, v)
	if k2 != k || v2 != v {
		t.Fatalf("not idempotent: (%q,%q) -> (%q,%q)", k, v, k2, v2)
	}
}
