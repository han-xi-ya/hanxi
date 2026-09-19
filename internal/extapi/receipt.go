// receipt.go 定义逻辑安装凭据(receipt)的存储契约。
//
// Phase 1-2 的"安装"是逻辑安装态:模块代码始终随宿主发布,receipt 仅表达
// "用户选择让该模块进入工作台"这一事实。UI 与文档必须如实说明:
// 逻辑卸载不会释放 hanxi.exe 体积(交付形态为 builtin-logical)。
// Wave 5+ 的 managed-declarative 与 official-sidecar 复用同一接口,
// 由各自的交付流程落不同 kind 的 receipt。
package extapi

import "time"

// ReceiptKind 与 DeliveryKind 对齐,记录凭据对应的交付形态。
type ReceiptKind string

const (
	// ReceiptBuiltinLogical 内建模块的逻辑安装凭据。
	ReceiptBuiltinLogical ReceiptKind = "builtin-logical"
	// ReceiptManagedDeclarative 声明式托管模块凭据(Wave 5+,预留)。
	ReceiptManagedDeclarative ReceiptKind = "managed-declarative"
	// ReceiptOfficialSidecar 官方 sidecar 凭据(Wave 6+,预留)。
	ReceiptOfficialSidecar ReceiptKind = "official-sidecar"
)

// Receipt 一个模块的安装凭据。字段即落盘 schema,变更须递增 Schema 并立案 ADR。
type Receipt struct {
	Schema      int         `json:"schema"`
	ModuleID    string      `json:"moduleId"`
	Kind        ReceiptKind `json:"kind"`
	InstalledAt time.Time   `json:"installedAt"`
}

// ReceiptStorage 安装凭据存储抽象:Registry 经它判定 Delivery 维度。
// 实现方要求:并发安全;写入原子(tmp+rename,对齐 settings.Store 纪律);
// MarkInstalled 幂等(重复安装保留首次 InstalledAt);磁盘缺失按未安装处理。
type ReceiptStorage interface {
	// IsInstalled 查询模块是否持有有效 receipt。
	IsInstalled(moduleID string) bool
	// MarkInstalled 登记/确认逻辑安装凭据;kind 由装配根按目录表提供。
	MarkInstalled(moduleID string, kind ReceiptKind) error
	// MarkAbsent 移除 receipt(逻辑卸载);数据目录默认保留,不在此处删除。
	MarkAbsent(moduleID string) error
	// Installed 批量返回全部已安装模块集合(投影用,键为 moduleId)。
	Installed() map[string]bool
}
