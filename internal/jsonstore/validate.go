package jsonstore

import "fmt"

// ValidateListenPort 托管服务 web/服务监听端口合法性：1024~65535
// （避开特权端口段，上游 ddns-go 首次设置页同规则），非法即拒。
// ddnsgo 与 ocr 两模块的 store/service 原各持一份逐字拷贝，收口于此共用。
func ValidateListenPort(port int) error {
	if port < 1024 || port > 65535 {
		return fmt.Errorf("端口需在 1024~65535 范围内，当前 %d", port)
	}
	return nil
}
