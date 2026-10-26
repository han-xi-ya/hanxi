package settings_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"hanxi/internal/settings"
)

// ── saveLocked 收口到 jsonstore.Save 的不变量锁（A2 2026-10-26）───────────
//
// 背景一（收口）：Store.saveLocked 曾是 jsonstore.Save 的无 Sync 克隆，掉电
// 场景 rename 元数据先行、数据页滞留缓存可留下 0 字节配置。收口后落盘强度
// 对齐 jsonstore（tmp+Sync+rename）。Sync 无法在无掉电台架下直接断言，本
// 文件锁定收口不得破坏的外部不变量：原子性、零残骸、格式逐字节兼容。
//
// 背景二（实测抓获的 Windows 真问题）：并发测试首跑即 24 路 Update 近半
// "Access is denied"——MoveFileEx REPLACE 遇目标被外部句柄打开（Go 的
// os.Open 无 FILE_SHARE_DELETE；杀毒/NAS 同步/编辑器读 config.json 都算，
// 机主以飞牛同步整个数据根为常态用法）直接失败。jsonstore.renameAtomic
// 已加有界退避重试；本文件第一测试即该修复的回归锁：读方在场时全部保存
// 必须最终成功。

// TestConcurrentUpdatesKeepFileAlwaysValidJSON 24 路并发 Update 与持续读盘
// 校验器对打：读侧任何时刻打开 config.json 都必须拿到合法 JSON；写侧全部
// 落定后文件与内存终态一致、每一路写过的键都在场、目录零 tmp 残骸。
func TestConcurrentUpdatesKeepFileAlwaysValidJSON(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.json")
	store, err := settings.NewStore(cfgFile)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	stop := make(chan struct{})
	var readMu sync.Mutex
	badReads := 0
	var readers sync.WaitGroup
	for range 3 {
		readers.Go(func() {
			for {
				select {
				case <-stop:
					return
				default:
				}
				raw, err := os.ReadFile(cfgFile)
				time.Sleep(200 * time.Microsecond) // 模拟真实外部读方（NAS 同步/扫描器）的持柄节奏
				if err != nil {
					continue // 首次落盘前文件尚不存在属合法形态
				}
				var probe map[string]any
				if uerr := json.Unmarshal(raw, &probe); uerr != nil {
					readMu.Lock()
					badReads++
					readMu.Unlock()
				}
			}
		})
	}

	var wg sync.WaitGroup
	for idx := range 24 {
		wg.Go(func() {
			if uerr := store.Update(func(c *settings.AppSettings) {
				c.Modules["probe-module"] = idx%2 == 0
				c.Modules[fmtKey(idx)] = true
			}); uerr != nil {
				t.Errorf("并发 Update #%d 失败（Windows 瞬时占用未被重试吸收）: %v", idx, uerr)
			}
		})
	}
	wg.Wait()
	close(stop)
	readers.Wait()

	if badReads != 0 {
		t.Fatalf("并发写窗口内读到 %d 次非法 JSON（原子替换被破坏）", badReads)
	}

	raw, err := os.ReadFile(cfgFile)
	if err != nil {
		t.Fatal(err)
	}
	var disk map[string]any
	if err := json.Unmarshal(raw, &disk); err != nil {
		t.Fatalf("终态文件非法 JSON: %v", err)
	}
	// 所有并发写都落定后，每一路写入过的键必在磁盘终态在场。
	mods, _ := disk["modules"].(map[string]any)
	if mods["probe-module"] == nil {
		t.Fatalf("并发 Update 的键未完整落盘: %v", disk["modules"])
	}
	for i := range 24 {
		if _, ok := mods[fmtKey(i)]; !ok {
			t.Fatalf("并发 Update #%d 的键在终态缺失（该次保存实际未落盘）", i)
		}
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp") {
			t.Fatalf("并发保存后残留临时文件（清理或命名协议破裂）: %s", e.Name())
		}
	}
}

func fmtKey(i int) string { return "probe-" + string(rune('a'+i%26)) + string(rune('a'+i/26)) }

// TestSavedConfigFormatByteCompatible 钉死存量兼容格式：2 空格缩进的
// MarshalIndent 输出、根对象 theme 居首——jsonstore 收口前后逐字节同形，
// 数据根里的 config.json 形态即契约。
func TestSavedConfigFormatByteCompatible(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.json")
	store, err := settings.NewStore(cfgFile)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	if err := store.SetModuleEnabled("fmt-probe", true); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(cfgFile)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(raw, []byte("{\n  \"theme\":")) {
		t.Fatalf("落盘格式漂移（须为 2 空格缩进、theme 居首）: %q", raw[:min(24, len(raw))])
	}
	if strings.Contains(string(raw), "\t") {
		t.Fatal("配置不得使用制表符缩进")
	}
}

// TestUpdateRollbackLeavesNoTmpDebris 目标被不可用形态占位（非空目录）时，
// Update 必须在有界重试后如实报错，且不留下半路 tmp——内存回滚由既有
// TestUpdateRollbackOnSaveFailure 锁定，本处补断言失败路径的磁盘清洁。
func TestUpdateRollbackLeavesNoTmpDebris(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.json")
	store, err := settings.NewStore(cfgFile)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	if err := store.SetQuickMenuTwoTier(true); err != nil {
		t.Fatal(err) // 先留下合法一代配置
	}

	// 故障注入：撤掉文件、用非空目录占位 → rename 必败（重试也救不回）。
	if err := os.Remove(cfgFile); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(cfgFile, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfgFile, "blocker"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if err := store.Update(func(c *settings.AppSettings) { c.Theme = "dark" }); err == nil {
		t.Fatal("持续占用形态下 Update 必须在重试后报错，不得谎称成功")
	}
	if dur := time.Since(start); dur > 3*time.Second {
		t.Fatalf("重试封顶失度（应秒级内收敛）: %v", dur)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp") {
			t.Fatalf("保存失败路径残留 tmp 未清理: %s", e.Name())
		}
	}
}
