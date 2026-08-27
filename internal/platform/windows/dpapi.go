//go:build windows

package windows

import (
	"encoding/base64"
	"fmt"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	crypt32                = windows.NewLazySystemDLL("crypt32.dll")
	procCryptProtectData   = crypt32.NewProc("CryptProtectData")
	procCryptUnprotectData = crypt32.NewProc("CryptUnprotectData")
)

// DPAPIEncrypt 使用 Windows 原生 DPAPI (CryptProtectData) 对明文数据进行硬件与当前用户绑定的透明加密
func DPAPIEncrypt(data []byte) (string, error) {
	if len(data) == 0 {
		return "", nil
	}

	var inBlob windows.DataBlob
	inBlob.Size = uint32(len(data))
	inBlob.Data = &data[0]

	var outBlob windows.DataBlob
	r1, _, err := procCryptProtectData.Call(
		uintptr(unsafe.Pointer(&inBlob)),
		0,
		0,
		0,
		0,
		0,
		uintptr(unsafe.Pointer(&outBlob)),
	)
	if r1 == 0 {
		return "", fmt.Errorf("CryptProtectData failed: %w", err)
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(outBlob.Data)))

	encryptedBytes := unsafe.Slice(outBlob.Data, outBlob.Size)
	// 返回 Base64 编码字符串
	return base64.StdEncoding.EncodeToString(encryptedBytes), nil
}

// DPAPIDecrypt 使用 Windows 原生 DPAPI (CryptUnprotectData) 解密由当前用户账户加密的 Base64 密文
func DPAPIDecrypt(cipherBase64 string) ([]byte, error) {
	if cipherBase64 == "" {
		return nil, nil
	}

	cipherBytes, err := base64.StdEncoding.DecodeString(cipherBase64)
	if err != nil {
		return nil, fmt.Errorf("invalid base64 ciphertext: %w", err)
	}
	if len(cipherBytes) == 0 {
		return nil, nil
	}

	var inBlob windows.DataBlob
	inBlob.Size = uint32(len(cipherBytes))
	inBlob.Data = &cipherBytes[0]

	var outBlob windows.DataBlob
	r1, _, unprotectErr := procCryptUnprotectData.Call(
		uintptr(unsafe.Pointer(&inBlob)),
		0,
		0,
		0,
		0,
		0,
		uintptr(unsafe.Pointer(&outBlob)),
	)
	if r1 == 0 {
		if unprotectErr != syscall.Errno(0) {
			return nil, fmt.Errorf("CryptUnprotectData failed: %w", unprotectErr)
		}
		return nil, fmt.Errorf("CryptUnprotectData returned 0")
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(outBlob.Data)))

	plainBytes := unsafe.Slice(outBlob.Data, outBlob.Size)
	result := make([]byte, len(plainBytes))
	copy(result, plainBytes)
	return result, nil
}
