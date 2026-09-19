// catalog_test.go 对 Wave 0 冻结契约做穷举验证：Project() 状态机优先级表、
// 四维全笛卡尔积摘要派生、入口可见性裁决、状态转换门，以及枚举字面量稳定性。
// 枚举冻结表与前端 frontend/src/constants/status.ts 词表逐字对齐——
// 改任何枚举值或状态机表，必须同步改本测试与前端词表，禁止静默漂移。
package extapi

import (
	"fmt"
	"slices"
	"testing"
)

// ---------------------------------------------------------------------------
// 枚举全集（穷举测试与稳定性冻结测试共用同一份口径）
// ---------------------------------------------------------------------------

var (
	deliveryKindValues = []DeliveryKind{
		DeliveryBuiltinLogical, DeliveryManagedDeclarative, DeliveryOfficialSidecar,
	}
	deliveryStateValues = []DeliveryState{
		DeliveryAbsent, DeliveryInstalling, DeliveryInstalled,
		DeliveryUpdating, DeliveryRemoving, DeliveryRepairing, DeliveryOrphaned,
	}
	policyStateValues = []PolicyState{
		PolicyEnabled, PolicyDisabled, PolicyBlocked, PolicyPendingConsent, PolicyMandatory,
	}
	runtimeStateValues = []RuntimeState{
		RuntimeInactive, RuntimeActivating, RuntimeActive,
		RuntimeBusy, RuntimeStopping, RuntimeCrashed, RuntimeFailed,
	}
	healthStateValues = []HealthState{
		HealthCurrent, HealthUpdateAvailable, HealthPinned, HealthIncompatible,
		HealthCorrupt, HealthRevoked, HealthUnverified, HealthDegraded, HealthOfflineStale,
	}
	entrypointValues = []Entrypoint{
		EntryRPC, EntryNavigation, EntrySearch, EntryTray,
		EntryHotkey, EntryMCP, EntryWindow, EntryBackground,
	}
	primaryActionValues = []PrimaryAction{
		ActionInstall, ActionEnable, ActionDisable, ActionOpen, ActionUpdate,
		ActionRetry, ActionRepair, ActionUninstall, ActionNone,
	}
	summaryKeyValues = []SummaryKey{
		SummaryNotInstalled, SummaryInstalledIdle, SummaryRunning, SummaryRunningUpdate,
		SummaryBlocked, SummaryFaulted, SummaryBusyOperation, SummaryIdleEnabled,
	}
)

func enumStrings[T ~string](vals []T) []string {
	out := make([]string, 0, len(vals))
	for _, v := range vals {
		out = append(out, string(v))
	}
	return out
}

// ---------------------------------------------------------------------------
// Project() 主操作优先级表（每条规则至少 1 例，含优先级压制例）
// ---------------------------------------------------------------------------

func TestProjectPrimaryAction(t *testing.T) {
	cases := []struct {
		name string
		in   StateInput
		want PrimaryAction
		// wantReason 非空：断言精确文案（契约含 UI 话术锚点的条目）。
		wantReason string
		// reasonAny：仅断言 Reason 非空（none/repair 分支必须有原因）。
		reasonAny bool
	}{
		// 规则 1：进行中的交付事务 → none + 进度语义。
		{
			name:       "installing",
			in:         StateInput{Delivery: DeliveryInstalling, Policy: PolicyEnabled, Runtime: RuntimeInactive, Health: HealthCurrent},
			want:       ActionNone,
			wantReason: "正在安装",
		},
		{
			// 优先级压制：installing 盖过 disabled，主操作不得是 enable。
			name:       "installing 压制 disabled（不得出 enable）",
			in:         StateInput{Delivery: DeliveryInstalling, Policy: PolicyDisabled, Runtime: RuntimeActive, Health: HealthCurrent},
			want:       ActionNone,
			wantReason: "正在安装",
		},
		{
			name:      "updating",
			in:        StateInput{Delivery: DeliveryUpdating, Policy: PolicyEnabled, Runtime: RuntimeActive, Health: HealthUpdateAvailable},
			want:      ActionNone,
			reasonAny: true,
		},
		{
			name:      "removing",
			in:        StateInput{Delivery: DeliveryRemoving, Policy: PolicyEnabled, Runtime: RuntimeInactive, Health: HealthCurrent},
			want:      ActionNone,
			reasonAny: true,
		},
		{
			name:      "repairing",
			in:        StateInput{Delivery: DeliveryRepairing, Policy: PolicyBlocked, Runtime: RuntimeCrashed, Health: HealthCorrupt},
			want:      ActionNone,
			reasonAny: true,
		},
		// 规则 2：orphaned/corrupt → repair。
		{
			// orphaned 判定先于健康维度：即使同时 corrupt/crashed 也出 repair。
			name:      "orphaned 压制 corrupt/crashed",
			in:        StateInput{Delivery: DeliveryOrphaned, Policy: PolicyMandatory, Runtime: RuntimeCrashed, Health: HealthCorrupt},
			want:      ActionRepair,
			reasonAny: true,
		},
		{
			// corrupt 判定先于 disabled。
			name:      "corrupt → repair（压制 disabled）",
			in:        StateInput{Delivery: DeliveryInstalled, Policy: PolicyDisabled, Runtime: RuntimeInactive, Health: HealthCorrupt},
			want:      ActionRepair,
			reasonAny: true,
		},
		// 规则 3：revoked/incompatible/unverified/blocked/pending-consent → none + 原因。
		{
			name:      "revoked 压制 enabled+active（不得出 open）",
			in:        StateInput{Delivery: DeliveryInstalled, Policy: PolicyEnabled, Runtime: RuntimeActive, Health: HealthRevoked},
			want:      ActionNone,
			reasonAny: true,
		},
		{
			name:      "incompatible",
			in:        StateInput{Delivery: DeliveryInstalled, Policy: PolicyEnabled, Runtime: RuntimeInactive, Health: HealthIncompatible},
			want:      ActionNone,
			reasonAny: true,
		},
		{
			name:      "unverified",
			in:        StateInput{Delivery: DeliveryInstalled, Policy: PolicyEnabled, Runtime: RuntimeActive, Health: HealthUnverified},
			want:      ActionNone,
			reasonAny: true,
		},
		{
			name:      "blocked 策略",
			in:        StateInput{Delivery: DeliveryInstalled, Policy: PolicyBlocked, Runtime: RuntimeInactive, Health: HealthCurrent},
			want:      ActionNone,
			reasonAny: true,
		},
		{
			name:      "pending-consent 策略",
			in:        StateInput{Delivery: DeliveryInstalled, Policy: PolicyPendingConsent, Runtime: RuntimeActive, Health: HealthCurrent},
			want:      ActionNone,
			reasonAny: true,
		},
		// 规则 4：absent → install。
		{
			name: "absent 基本例",
			in:   StateInput{Delivery: DeliveryAbsent, Policy: PolicyDisabled, Runtime: RuntimeInactive, Health: HealthCurrent},
			want: ActionInstall,
		},
		{
			// 优先级压制：absent 判定先于运行失败滞留，install 优先于 retry。
			name: "absent+failed 滞留 → install 优先于 retry",
			in:   StateInput{Delivery: DeliveryAbsent, Policy: PolicyEnabled, Runtime: RuntimeFailed, Health: HealthCurrent},
			want: ActionInstall,
		},
		// 规则 5：disabled → enable。
		{
			name: "disabled → enable",
			in:   StateInput{Delivery: DeliveryInstalled, Policy: PolicyDisabled, Runtime: RuntimeInactive, Health: HealthCurrent},
			want: ActionEnable,
		},
		// 规则 6：mandatory/enabled + crashed/failed → retry。
		{
			name: "crashed → retry",
			in:   StateInput{Delivery: DeliveryInstalled, Policy: PolicyEnabled, Runtime: RuntimeCrashed, Health: HealthCurrent},
			want: ActionRetry,
		},
		{
			name: "mandatory+failed → retry",
			in:   StateInput{Delivery: DeliveryInstalled, Policy: PolicyMandatory, Runtime: RuntimeFailed, Health: HealthDegraded},
			want: ActionRetry,
		},
		// 规则 7：其余（installed + enabled/mandatory）→ open。
		{
			name: "installed+enabled+inactive → open（懒激活）",
			in:   StateInput{Delivery: DeliveryInstalled, Policy: PolicyEnabled, Runtime: RuntimeInactive, Health: HealthCurrent},
			want: ActionOpen,
		},
		{
			name: "installed+mandatory+active → open",
			in:   StateInput{Delivery: DeliveryInstalled, Policy: PolicyMandatory, Runtime: RuntimeActive, Health: HealthUpdateAvailable},
			want: ActionOpen,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := tc.in.Project()
			if s.PrimaryAction != tc.want {
				t.Fatalf("PrimaryAction = %q, want %q (input %+v)", s.PrimaryAction, tc.want, tc.in)
			}
			switch {
			case tc.wantReason != "":
				if s.Reason != tc.wantReason {
					t.Fatalf("Reason = %q, want %q", s.Reason, tc.wantReason)
				}
			case tc.reasonAny:
				if s.Reason == "" {
					t.Fatalf("Reason must be non-empty for action %q", s.PrimaryAction)
				}
			default:
				// install/enable/open/retry 不得携带原因（Reason 仅在 none/repair 时出现）。
				if s.Reason != "" {
					t.Fatalf("Reason = %q, want empty for action %q", s.Reason, s.PrimaryAction)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// 全笛卡尔积摘要派生 + §6.5 具名例
// ---------------------------------------------------------------------------

func TestProjectSummaryExhaustive(t *testing.T) {
	actions := enumStrings(primaryActionValues)
	summaries := enumStrings(summaryKeyValues)

	total := 0
	for _, d := range deliveryStateValues {
		for _, p := range policyStateValues {
			for _, rt := range runtimeStateValues {
				for _, h := range healthStateValues {
					total++
					in := StateInput{ModuleID: "mod-42", Delivery: d, Policy: p, Runtime: rt, Health: h}
					s := in.Project() // 契约：任何组合都不得 panic
					combo := fmt.Sprintf("%s|%s|%s|%s", d, p, rt, h)

					if s.Schema != ModuleContractSchema {
						t.Fatalf("[%s] Schema = %d, want %d", combo, s.Schema, ModuleContractSchema)
					}
					if s.ModuleID != in.ModuleID {
						t.Fatalf("[%s] ModuleID = %q, want %q", combo, s.ModuleID, in.ModuleID)
					}
					if !slices.Contains(actions, string(s.PrimaryAction)) {
						t.Fatalf("[%s] PrimaryAction = %q 不在冻结枚举集内", combo, s.PrimaryAction)
					}
					if !slices.Contains(summaries, string(s.Summary)) {
						t.Fatalf("[%s] Summary = %q 不在冻结枚举集内", combo, s.Summary)
					}
					// Reason 非空性不变量：none/repair 必带原因，其余动作不得带。
					if s.PrimaryAction == ActionNone || s.PrimaryAction == ActionRepair {
						if s.Reason == "" {
							t.Fatalf("[%s] action %q 必须携带非空 Reason", combo, s.PrimaryAction)
						}
					} else if s.Reason != "" {
						t.Fatalf("[%s] action %q 不得携带 Reason（%q）", combo, s.PrimaryAction, s.Reason)
					}
				}
			}
		}
	}
	if want := len(deliveryStateValues) * len(policyStateValues) * len(runtimeStateValues) * len(healthStateValues); total != want || want != 2205 {
		t.Fatalf("组合覆盖 = %d, want %d", total, want)
	}

	// 模块分发专项 §6.5 的 6 条具名派生例。
	named := []struct {
		name string
		in   StateInput
		want SummaryKey
	}{
		{
			name: "not-installed",
			in:   StateInput{Delivery: DeliveryAbsent, Policy: PolicyDisabled, Runtime: RuntimeInactive, Health: HealthCurrent},
			want: SummaryNotInstalled,
		},
		{
			name: "installed-disabled",
			in:   StateInput{Delivery: DeliveryInstalled, Policy: PolicyDisabled, Runtime: RuntimeInactive, Health: HealthCurrent},
			want: SummaryInstalledIdle,
		},
		{
			name: "running-update",
			in:   StateInput{Delivery: DeliveryInstalled, Policy: PolicyEnabled, Runtime: RuntimeActive, Health: HealthUpdateAvailable},
			want: SummaryRunningUpdate,
		},
		{
			name: "blocked",
			in:   StateInput{Delivery: DeliveryInstalled, Policy: PolicyBlocked, Runtime: RuntimeInactive, Health: HealthRevoked},
			want: SummaryBlocked,
		},
		{
			name: "faulted",
			in:   StateInput{Delivery: DeliveryInstalled, Policy: PolicyEnabled, Runtime: RuntimeCrashed, Health: HealthDegraded},
			want: SummaryFaulted,
		},
		{
			// updating 特例：旧版本仍可用（runtime=active）时按运行中呈现。
			name: "updating 仍在跑 → running",
			in:   StateInput{Delivery: DeliveryUpdating, Policy: PolicyEnabled, Runtime: RuntimeActive, Health: HealthCurrent},
			want: SummaryRunning,
		},
	}
	for _, tc := range named {
		t.Run("§6.5/"+tc.name, func(t *testing.T) {
			if got := tc.in.Project().Summary; got != tc.want {
				t.Fatalf("Summary = %q, want %q (input %+v)", got, tc.want, tc.in)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Visible()：installed ∧ (enabled ∨ mandatory)
// ---------------------------------------------------------------------------

func TestVisibleRule(t *testing.T) {
	cases := []struct {
		name string
		st   ModuleState
		want bool
	}{
		{"installed+enabled", ModuleState{Delivery: DeliveryInstalled, Policy: PolicyEnabled}, true},
		{"installed+mandatory", ModuleState{Delivery: DeliveryInstalled, Policy: PolicyMandatory}, true},
		{"installed+enabled+crashed（运行态不影响可见性）", ModuleState{Delivery: DeliveryInstalled, Policy: PolicyEnabled, Runtime: RuntimeCrashed}, true},
		{"absent+enabled", ModuleState{Delivery: DeliveryAbsent, Policy: PolicyEnabled}, false},
		{"installing+enabled（事务中也不算 installed）", ModuleState{Delivery: DeliveryInstalling, Policy: PolicyEnabled}, false},
		{"installed+disabled", ModuleState{Delivery: DeliveryInstalled, Policy: PolicyDisabled}, false},
		{"installed+blocked", ModuleState{Delivery: DeliveryInstalled, Policy: PolicyBlocked}, false},
		{"installed+pending-consent", ModuleState{Delivery: DeliveryInstalled, Policy: PolicyPendingConsent}, false},
		{"removing+mandatory", ModuleState{Delivery: DeliveryRemoving, Policy: PolicyMandatory}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.st.Visible(); got != tc.want {
				t.Fatalf("Visible() = %v, want %v (state %+v)", got, tc.want, tc.st)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// 转换表：对冻结的期望边集做全交叉验证（正向 true、非法 false、同态幂等 true）
// ---------------------------------------------------------------------------

// 以下三张期望表是 catalog.go 转换表的独立复述（契约锚点）：
// 有人改转换表时必须同步改这里，逼出显式评审。
var (
	wantDeliveryEdges = map[DeliveryState][]DeliveryState{
		DeliveryAbsent:     {DeliveryInstalling},
		DeliveryInstalling: {DeliveryInstalled, DeliveryAbsent},
		DeliveryInstalled:  {DeliveryUpdating, DeliveryRemoving, DeliveryRepairing, DeliveryOrphaned},
		DeliveryUpdating:   {DeliveryInstalled, DeliveryOrphaned},
		DeliveryRemoving:   {DeliveryAbsent, DeliveryOrphaned},
		DeliveryRepairing:  {DeliveryInstalled, DeliveryOrphaned},
		DeliveryOrphaned:   {DeliveryRepairing, DeliveryRemoving, DeliveryAbsent},
	}
	wantPolicyEdges = map[PolicyState][]PolicyState{
		PolicyEnabled:        {PolicyDisabled, PolicyBlocked, PolicyPendingConsent},
		PolicyDisabled:       {PolicyEnabled, PolicyBlocked, PolicyPendingConsent},
		PolicyBlocked:        {PolicyEnabled, PolicyDisabled},
		PolicyPendingConsent: {PolicyEnabled, PolicyDisabled, PolicyBlocked},
		PolicyMandatory:      {}, // Core 不允许策略迁移
	}
	wantRuntimeEdges = map[RuntimeState][]RuntimeState{
		RuntimeInactive:   {RuntimeActivating},
		RuntimeActivating: {RuntimeActive, RuntimeFailed, RuntimeInactive},
		RuntimeActive:     {RuntimeBusy, RuntimeStopping, RuntimeInactive, RuntimeCrashed},
		RuntimeBusy:       {RuntimeActive, RuntimeStopping, RuntimeCrashed},
		RuntimeStopping:   {RuntimeInactive, RuntimeFailed, RuntimeCrashed},
		RuntimeCrashed:    {RuntimeActivating, RuntimeInactive},
		RuntimeFailed:     {RuntimeActivating, RuntimeInactive},
	}
)

func checkTransitionTable[T comparable](t *testing.T, name string, values []T, want map[T][]T, can func(T, T) bool) {
	t.Helper()
	for _, v := range values {
		if _, ok := want[v]; !ok {
			t.Errorf("%s: 状态 %v 缺少期望边集条目（新增枚举值必须同步冻结表）", name, v)
		}
	}
	for _, from := range values {
		// 同态幂等：任何状态自环必须放行。
		if !can(from, from) {
			t.Errorf("%s: Can(%v → %v) = false, want true（同态幂等）", name, from, from)
		}
		for _, to := range values {
			expected := from == to || slices.Contains(want[from], to)
			if got := can(from, to); got != expected {
				t.Errorf("%s: Can(%v → %v) = %v, want %v", name, from, to, got, expected)
			}
		}
	}
}

func TestTransitionTables(t *testing.T) {
	t.Run("delivery", func(t *testing.T) {
		checkTransitionTable(t, "delivery", deliveryStateValues, wantDeliveryEdges, CanDeliveryTransition)
	})
	t.Run("policy", func(t *testing.T) {
		checkTransitionTable(t, "policy", policyStateValues, wantPolicyEdges, CanPolicyTransition)
	})
	t.Run("runtime", func(t *testing.T) {
		checkTransitionTable(t, "runtime", runtimeStateValues, wantRuntimeEdges, CanRuntimeTransition)
	})

	// 代表非法边显式点名（防止期望表与判定函数同时被误改时互相印证掩盖）。
	illegal := []struct {
		name     string
		delivery [][2]DeliveryState
		policy   [][2]PolicyState
		runtime  [][2]RuntimeState
	}{
		{
			delivery: [][2]DeliveryState{
				{DeliveryAbsent, DeliveryInstalled}, // 必须经 installing
				{DeliveryInstalled, DeliveryAbsent}, // 必须经 removing
				{DeliveryInstalling, DeliveryRemoving},
				{DeliveryOrphaned, DeliveryInstalled}, // 必须先 repair/remove
			},
			policy: [][2]PolicyState{
				{PolicyEnabled, PolicyMandatory}, // mandatory 只能由装配根注入，非迁移
				{PolicyBlocked, PolicyPendingConsent},
			},
			runtime: [][2]RuntimeState{
				{RuntimeInactive, RuntimeActive}, // 必须经 activating
				{RuntimeActive, RuntimeFailed},   // 失败只出现在启动/停止路径
				{RuntimeCrashed, RuntimeFailed},
				{RuntimeFailed, RuntimeActive}, // 重试必须重走 activating
			},
		},
	}
	for _, grp := range illegal {
		for _, e := range grp.delivery {
			if CanDeliveryTransition(e[0], e[1]) {
				t.Errorf("delivery: 非法边 %v → %v 被判为允许", e[0], e[1])
			}
		}
		for _, e := range grp.policy {
			if CanPolicyTransition(e[0], e[1]) {
				t.Errorf("policy: 非法边 %v → %v 被判为允许", e[0], e[1])
			}
		}
		for _, e := range grp.runtime {
			if CanRuntimeTransition(e[0], e[1]) {
				t.Errorf("runtime: 非法边 %v → %v 被判为允许", e[0], e[1])
			}
		}
	}

	// mandatory 策略：除自环外无任何出边。
	for _, to := range policyStateValues {
		if to == PolicyMandatory {
			continue
		}
		if CanPolicyTransition(PolicyMandatory, to) {
			t.Errorf("mandatory → %v 必须被禁止（Core 不允许策略迁移）", to)
		}
	}
}

// ---------------------------------------------------------------------------
// 枚举字面量稳定性冻结（与前端 status.ts 词表对齐的锚点）
// ---------------------------------------------------------------------------

func TestEntryEnumStability(t *testing.T) {
	frozen := []struct {
		name string
		got  []string
		want []string
	}{
		{"DeliveryKind", enumStrings(deliveryKindValues), []string{
			"builtin-logical", "managed-declarative", "official-sidecar",
		}},
		{"DeliveryState", enumStrings(deliveryStateValues), []string{
			"absent", "installing", "installed", "updating", "removing", "repairing", "orphaned",
		}},
		{"PolicyState", enumStrings(policyStateValues), []string{
			"enabled", "disabled", "blocked", "pending-consent", "mandatory",
		}},
		{"RuntimeState", enumStrings(runtimeStateValues), []string{
			"inactive", "activating", "active", "busy", "stopping", "crashed", "failed",
		}},
		{"HealthState", enumStrings(healthStateValues), []string{
			"current", "update-available", "pinned", "incompatible", "corrupt",
			"revoked", "unverified", "degraded", "offline-stale",
		}},
		{"Entrypoint", enumStrings(entrypointValues), []string{
			"rpc", "navigation", "search", "tray", "hotkey", "mcp", "window", "background",
		}},
		{"PrimaryAction", enumStrings(primaryActionValues), []string{
			"install", "enable", "disable", "open", "update", "retry", "repair", "uninstall", "none",
		}},
		{"SummaryKey", enumStrings(summaryKeyValues), []string{
			"not-installed", "installed-disabled", "installed-enabled", "running",
			"running-update", "blocked", "faulted", "in-progress",
		}},
	}
	for _, tc := range frozen {
		t.Run(tc.name, func(t *testing.T) {
			seen := make(map[string]bool, len(tc.got))
			for _, v := range tc.got {
				if seen[v] {
					t.Fatalf("%s: 枚举值 %q 重复", tc.name, v)
				}
				seen[v] = true
			}
			a := slices.Clone(tc.got)
			b := slices.Clone(tc.want)
			slices.Sort(a)
			slices.Sort(b)
			if !slices.Equal(a, b) {
				t.Fatalf("%s 枚举集合漂移：代码侧 %v，冻结表 %v（改值必须同步前端 status.ts 并更新本测试）",
					tc.name, tc.got, tc.want)
			}
		})
	}

	// 四维规模锚点：7×5×7×9=2205 与穷举测试的期望一致。
	if n := len(deliveryStateValues) * len(policyStateValues) * len(runtimeStateValues) * len(healthStateValues); n != 2205 {
		t.Fatalf("四维笛卡尔积规模 = %d, want 2205（枚举增删需评审契约影响）", n)
	}
}
