package wechat

import (
	"encoding/json"
	"testing"
)

func TestInboundFilePayloadUnmarshal(t *testing.T) {
	tests := []struct {
		name string
		len  string
	}{
		{name: "string", len: `"12345"`},
		{name: "number", len: `12345`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw := `{
				"file_name":"测试文件.txt",
				"len":` + tt.len + `,
				"media":{
					"encrypt_query_param":"query-value",
					"aes_key":"key-value",
					"encrypt_type":1
				}
			}`

			var payload InboundFilePayload
			if err := json.Unmarshal([]byte(raw), &payload); err != nil {
				t.Fatalf("Unmarshal() error = %v", err)
			}
			if payload.FileName != "测试文件.txt" {
				t.Fatalf("FileName = %q", payload.FileName)
			}
			if payload.Len != 12345 {
				t.Fatalf("Len = %d", payload.Len)
			}
			if payload.Media.EncryptQueryParam != "query-value" || payload.Media.AESKey != "key-value" || payload.Media.EncryptType != 1 {
				t.Fatalf("Media = %+v", payload.Media)
			}
		})
	}
}

func TestInboundFileSizeRejectsInvalidValues(t *testing.T) {
	for _, raw := range []string{`-1`, `"-1"`, `"abc"`, `1.5`} {
		t.Run(raw, func(t *testing.T) {
			var size InboundFileSize
			if err := json.Unmarshal([]byte(raw), &size); err == nil {
				t.Fatalf("Unmarshal(%s) unexpectedly succeeded with %d", raw, size)
			}
		})
	}
}
