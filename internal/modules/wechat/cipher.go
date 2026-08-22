package wechat

import (
	"bytes"
	"crypto/aes"
	"crypto/md5"
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

// pkcs7Pad 对数据进行 PKCS#7 填充
func pkcs7Pad(data []byte, blockSize int) []byte {
	padding := blockSize - (len(data) % blockSize)
	padText := bytes.Repeat([]byte{byte(padding)}, padding)
	return append(data, padText...)
}

// pkcs7Unpad 去除 PKCS#7 填充
func pkcs7Unpad(data []byte, blockSize int) ([]byte, error) {
	length := len(data)
	if length == 0 || length%blockSize != 0 {
		return nil, fmt.Errorf("invalid padded data length")
	}
	padLen := int(data[length-1])
	if padLen == 0 || padLen > blockSize || padLen > length {
		return nil, fmt.Errorf("invalid padding size: %d", padLen)
	}
	for i := 0; i < padLen; i++ {
		if data[length-1-i] != byte(padLen) {
			return nil, fmt.Errorf("corrupted padding byte at offset %d", length-1-i)
		}
	}
	return data[:length-padLen], nil
}

// aesEcbEncrypt 使用 16 字节密钥执行 AES-128-ECB 加密（自动做 PKCS#7 填充）
func aesEcbEncrypt(data, key []byte) ([]byte, error) {
	if len(key) != 16 {
		return nil, fmt.Errorf("aes-128-ecb requires a 16-byte key, got %d", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("create aes cipher failed: %w", err)
	}

	padded := pkcs7Pad(data, block.BlockSize())
	encrypted := make([]byte, len(padded))
	for bs, be := 0, block.BlockSize(); bs < len(padded); bs, be = bs+block.BlockSize(), be+block.BlockSize() {
		block.Encrypt(encrypted[bs:be], padded[bs:be])
	}
	return encrypted, nil
}

// aesEcbDecrypt 使用 16 字节密钥执行 AES-128-ECB 解密并去除 PKCS#7 填充
func aesEcbDecrypt(data, key []byte) ([]byte, error) {
	if len(key) != 16 {
		return nil, fmt.Errorf("aes-128-ecb requires a 16-byte key, got %d", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("create aes cipher failed: %w", err)
	}
	if len(data)%block.BlockSize() != 0 {
		return nil, fmt.Errorf("ciphertext is not a multiple of the block size")
	}

	decrypted := make([]byte, len(data))
	for bs, be := 0, block.BlockSize(); bs < len(data); bs, be = bs+block.BlockSize(), be+block.BlockSize() {
		block.Decrypt(decrypted[bs:be], data[bs:be])
	}

	return pkcs7Unpad(decrypted, block.BlockSize())
}

// md5Hex 计算数据的小写 MD5 Hex 字符串
func md5Hex(data []byte) string {
	sum := md5.Sum(data)
	return hex.EncodeToString(sum[:])
}

// randomBytes 生成指定长度的安全随机字节
func randomBytes(n int) ([]byte, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return nil, fmt.Errorf("generate random bytes failed: %w", err)
	}
	return b, nil
}

// randomHex 生成 2*n 字符长度的随机 Hex 字符串
func randomHex(n int) (string, error) {
	b, err := randomBytes(n)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// aesEcbPaddedSize 计算 PKCS#7 填充后的密文尺寸
func aesEcbPaddedSize(rawSize int) int {
	return ((rawSize + 1 + 15) / 16) * 16
}
