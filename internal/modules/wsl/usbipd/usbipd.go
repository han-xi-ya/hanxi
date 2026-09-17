// Package usbipd usbipd-win 命令行胶水：设备态解析（usbipd state JSON）、
// 命令构造（busid/GUID 白名单校验、前端零拼接面）与错误中文化。
//
// 与上游实况对齐（2026-09 对 dorssel/usbipd-win master 逐文件核对）：
//   - JSON 状态源是 `usbipd state`（"Output state in JSON"）；`usbipd list` 只有
//     人读表格、没有 --json 选项（BACKLOG F9 卡片记的 "list --json" 系笔误，
//     本包以 state 为准，解析端兼容 {Devices:[...]} 对象与裸数组两种形态）；
//   - state JSON 的 Device 只有 InstanceId/Description/IsForced/BusId/PersistedGuid/
//     StubInstanceId/ClientIPAddress 七个字段（HardwareId/IsBound/IsAttached 等是
//     [JsonIgnore] 的计算属性），故 VID/PID 从 InstanceId 的 VID_xxxx&PID_xxxx 段
//     提取、共享态从可空性推导（与上游 list 命令自身的推导口径一致）；
//   - bind/unbind 必提权（usbipd 自查 CheckWriteAccess）、attach/detach 用户态即可；
//     attach 的发行版参数语法是 `--wsl <DISTRO>`（可选值），没有 `-d`。
//
// 边界：本包不执行提权（bind/unbind 参数在此构造，由 wsl 模块的白名单提权通道
// 下发）；不做远程主机的 usbip；不做驱动安装（Windows PnP 的事）。
package usbipd

import (
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// 设备共享态归一枚举（上游 JSON 不给状态字符串，全部由可空性推导）。
const (
	StateAttached     = "attached"     // 已附加给客户端（WSL）
	StateShared       = "shared"       // 已共享（bind），未附加
	StateNotShared    = "notshared"    // 未共享
	StateIncompatible = "incompatible" // 不兼容的集线器（上游拒绝共享）
)

// Device usbipd state 的一行设备（归一化视图，直出前端）。
type Device struct {
	BusID       string `json:"busId"`       // 空串=设备当前不在场（已共享但已拔出）
	Description string `json:"description"` // Windows PnP 设备描述
	InstanceID  string `json:"instanceId"`
	Vid         string `json:"vid,omitempty"` // 归一小写 4 位十六进制
	Pid         string `json:"pid,omitempty"`
	Guid        string `json:"guid,omitempty"`     // 绑定持久化 GUID（非空=已 bind）
	ClientIP    string `json:"clientIp,omitempty"` // 附加客户端 IP
	Serial      string `json:"serial,omitempty"`   // InstanceId 末段（形如真序列号才给）
	State       string `json:"state"`
	Forced      bool   `json:"forced"` // 以 --force 绑定（独占、需 VBox 驱动）
}

// Connected 设备当前是否在总线上（插着）。
func (d Device) Connected() bool { return d.BusID != "" }

// vidPidRe 从设备实例路径提取 VID/PID（usbipd-win 的 HardwareId 同源口径）。
var vidPidRe = regexp.MustCompile(`(?i)VID_([0-9a-f]{4})&PID_([0-9a-f]{4})`)

// busIDRe 总线路设备标识：`<hub>-<port>[.<interface>]`（上游 BusId.Parse 同形）。
var busIDRe = regexp.MustCompile(`^\d{1,4}-\d{1,4}(\.\d{1,3})?$`)

// guidRe 绑定 GUID（unbind --guid 通道）：标准 8-4-4-4-12 十六进制。
var guidRe = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// CheckBusID 白名单校验总线路标识；命令参数一律先过这道门，杜绝任意字符串进命令。
func CheckBusID(s string) (string, error) {
	s = strings.TrimSpace(s)
	if !busIDRe.MatchString(s) {
		return "", fmt.Errorf("总线路标识 %q 不合法（应为形如 2-3 或 1-4.1 的「集线器-端口」）", s)
	}
	return s, nil
}

// CheckGUID 白名单校验绑定 GUID。
func CheckGUID(s string) (string, error) {
	s = strings.TrimSpace(s)
	if !guidRe.MatchString(s) {
		return "", fmt.Errorf("设备 GUID %q 不合法", s)
	}
	return strings.ToLower(s), nil
}

// ---- 命令构造（参数面全部固定字面量 + 已过白名单的值） ----

// StateArgs 设备表取数命令。
func StateArgs() []string { return []string{"state"} }

// VersionArgs 存在性探测/版本命令。
func VersionArgs() []string { return []string{"--version"} }

// BindArgs 共享（bind）命令；执行须提权，由上层走白名单提权通道。
func BindArgs(busID string, force bool) ([]string, error) {
	id, err := CheckBusID(busID)
	if err != nil {
		return nil, err
	}
	args := []string{"bind", "--busid", id}
	if force {
		args = append(args, "--force")
	}
	return args, nil
}

// UnbindArgs 取消共享（bind 的反操作）；执行须提权。
func UnbindArgs(busID string) ([]string, error) {
	id, err := CheckBusID(busID)
	if err != nil {
		return nil, err
	}
	return []string{"unbind", "--busid", id}, nil
}

// UnbindGUIDArgs 对"已共享但不在场"的设备取消共享（unbind --guid）；执行须提权。
func UnbindGUIDArgs(guid string) ([]string, error) {
	g, err := CheckGUID(guid)
	if err != nil {
		return nil, err
	}
	return []string{"unbind", "--guid", g}, nil
}

// AttachArgs 附加到 WSL 发行版（用户态命令）。distro 空串=交给 usbipd 用系统默认
// 发行版；非空时的合法性由上层用 `wsl -l -q` 实时名单白名单校验（同 InstallDistro
// 纪律），这里只拦控制字符与空白拼接面。
func AttachArgs(distro, busID string) ([]string, error) {
	id, err := CheckBusID(busID)
	if err != nil {
		return nil, err
	}
	distro = strings.TrimSpace(distro)
	if distro != "" {
		if strings.ContainsAny(distro, "\x00\r\n\t") || len(distro) > 120 {
			return nil, fmt.Errorf("发行版名 %q 不合法", distro)
		}
		return []string{"attach", "--wsl", distro, "--busid", id}, nil
	}
	return []string{"attach", "--wsl", "--busid", id}, nil
}

// DetachArgs 从客户端卸下（用户态命令）。
func DetachArgs(busID string) ([]string, error) {
	id, err := CheckBusID(busID)
	if err != nil {
		return nil, err
	}
	return []string{"detach", "--busid", id}, nil
}

// ---- state JSON 解析 ----

// rawDevice usbipd state 的 JSON 行（.NET 序列化形态，可空字段用指针）。
// 字段名靠 encoding/json 的大小写不敏感匹配同时吃 PascalCase（state 命令）
// 与 camelCase 变体；State/Vid/Pid 为旧版兼容字段（新版 JSON 不给，缺失即推导）。
type rawDevice struct {
	BusID          *string `json:"busid"`
	Vid            *string `json:"vid"`
	Pid            *string `json:"pid"`
	InstanceID     string  `json:"instanceid"`
	Description    string  `json:"description"`
	State          string  `json:"state"`
	PersistedGuid  *string `json:"persistedguid"`
	StubInstanceId *string `json:"stubinstanceid"`
	ClientIPAddr   *string `json:"clientipaddress"`
	IsForced       bool    `json:"isforced"`
}

// ParseState 解析 `usbipd state` 的 JSON 输出为归一化设备表。
// 容忍命令在前置告警行后输出 JSON（截取首个 '{'/'[' 起的片段）；
// 兼容 {"Devices":[...]} 与裸数组两种形态；两形态都解不动时报错（旧版或输出被劫持）。
// 结果按 BusID 自然序排定（2-1 在 2-10 前），不在场设备沉底按 GUID 序。
func ParseState(out string) ([]Device, error) {
	body, ok := extractJSON(out)
	if !ok {
		return nil, fmt.Errorf("usbipd 状态输出中没有可识别的 JSON——请确认 usbipd-win 为 4.0 以上版本（若被其它程序劫持输出，请修复 PATH）")
	}
	var raws []rawDevice
	if body[0] == '[' {
		if err := json.Unmarshal([]byte(body), &raws); err != nil {
			return nil, fmt.Errorf("解析 usbipd 设备表失败: %w", err)
		}
	} else {
		var doc struct {
			Devices []rawDevice `json:"devices"`
		}
		if err := json.Unmarshal([]byte(body), &doc); err != nil {
			return nil, fmt.Errorf("解析 usbipd 设备表失败: %w", err)
		}
		raws = doc.Devices
	}
	devices := make([]Device, 0, len(raws))
	for _, r := range raws {
		devices = append(devices, normalize(r))
	}
	slices.SortStableFunc(devices, func(a, b Device) int {
		if ca, cb := a.Connected(), b.Connected(); ca != cb {
			if !ca {
				return 1
			}
			return -1
		}
		if !a.Connected() {
			return strings.Compare(a.Guid, b.Guid)
		}
		return compareBusID(a.BusID, b.BusID)
	})
	return devices, nil
}

// extractJSON 截取输出中从首个 {（或 [）到其配对的末个 }（或 ]）的 JSON 片段。
func extractJSON(out string) (string, bool) {
	out = strings.TrimSpace(strings.TrimPrefix(out, "\xEF\xBB\xBF"))
	start := strings.IndexAny(out, "{[")
	if start < 0 {
		return "", false
	}
	closer := byte('}')
	if out[start] == '[' {
		closer = ']'
	}
	end := strings.LastIndexByte(out, closer)
	if end <= start {
		return "", false
	}
	return out[start : end+1], true
}

// normalize 单行归一：VID/PID 双通道（字段在就用、不在从 InstanceId 抠），
// 状态按显式 State（旧版）→ 可空性推导（新版）的顺序落定。
func normalize(r rawDevice) Device {
	d := Device{
		Description: strings.TrimSpace(r.Description),
		InstanceID:  strings.TrimSpace(r.InstanceID),
		Forced:      r.IsForced,
	}
	if r.BusID != nil {
		d.BusID = strings.TrimSpace(*r.BusID)
	}
	if r.PersistedGuid != nil {
		d.Guid = strings.ToLower(strings.TrimSpace(*r.PersistedGuid))
	}
	if r.ClientIPAddr != nil {
		d.ClientIP = strings.TrimSpace(*r.ClientIPAddr)
	}
	if d.Vid, d.Pid = lookupVidPid(r, d.InstanceID); d.Description == "" {
		d.Description = "（未知设备）"
	}
	d.Serial = lastInstancePart(d.InstanceID)
	switch {
	case r.State != "":
		applyStateText(&d, r.State)
	case r.ClientIPAddr != nil && *r.ClientIPAddr != "" || r.StubInstanceId != nil && *r.StubInstanceId != "":
		d.State = StateAttached
	case d.Guid != "":
		d.State = StateShared
	default:
		d.State = StateNotShared
	}
	return d
}

// applyStateText 解析旧版 JSON 自带的状态文案（上游 list 表格用语）；未知文案回推导。
func applyStateText(d *Device, s string) {
	low := strings.ToLower(s)
	switch {
	case strings.Contains(low, "attached"):
		d.State = StateAttached
	case strings.Contains(low, "forced"):
		d.State, d.Forced = StateShared, true
	case strings.Contains(low, "incompatible"):
		d.State = StateIncompatible
	case strings.Contains(low, "shared"):
		d.State = StateShared
	default:
		d.State = StateNotShared
	}
}

func lookupVidPid(r rawDevice, instanceID string) (vid, pid string) {
	grab := func(s string) string {
		s = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(s), "0x"))
		if len(s) == 4 && vidHexRe.MatchString(s) {
			return s
		}
		return ""
	}
	if r.Vid != nil {
		vid = grab(*r.Vid)
	}
	if r.Pid != nil {
		pid = grab(*r.Pid)
	}
	if vid == "" || pid == "" {
		if m := vidPidRe.FindStringSubmatch(instanceID); m != nil {
			if vid == "" {
				vid = strings.ToLower(m[1])
			}
			if pid == "" {
				pid = strings.ToLower(m[2])
			}
		}
	}
	return
}

var vidHexRe = regexp.MustCompile(`^[0-9a-f]{4}$`)

// lastInstancePart 取实例路径末段作序列号；含 '&' 的是硬件生成分段、不是真序列号
// （wsl-dashboard 同款判据）。
func lastInstancePart(instanceID string) string {
	if instanceID == "" {
		return ""
	}
	last := instanceID
	if i := strings.LastIndexByte(instanceID, '\\'); i >= 0 {
		last = instanceID[i+1:]
	}
	if last == "" || strings.Contains(last, "&") {
		return ""
	}
	return last
}

// compareBusID 自然序比较："1-2" < "1-10" < "2-1"；数字段按数值、非数字段按字典序。
func compareBusID(a, b string) int {
	as, bs := busIDParts(a), busIDParts(b)
	for i := 0; i < len(as) && i < len(bs); i++ {
		if an, aerr := strconv.Atoi(as[i]); aerr == nil {
			if bn, berr := strconv.Atoi(bs[i]); berr == nil {
				if an != bn {
					return an - bn
				}
				continue
			}
		}
		if c := strings.Compare(as[i], bs[i]); c != 0 {
			return c
		}
	}
	return len(as) - len(bs)
}

func busIDParts(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool { return r == '-' || r == '.' })
}
