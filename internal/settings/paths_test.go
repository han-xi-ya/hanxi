package settings

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeBindForTest 在 exeDir 落一份绑定声明文件。
func writeBindForTest(t *testing.T, exeDir, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(exeDir, bindFileName), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

// TestResolveDataRootBinding 绑定指针优先级：有绑定认绑定（含跨目录目标），
// 空白声明视为损坏并 fail loud；无效声明 fail loud，绝不静默忽略、绝不落用户目录。
func TestResolveDataRootBinding(t *testing.T) {
	t.Run("绑定生效：指针目标目录存在即用", func(t *testing.T) {
		exeDir := t.TempDir()
		target := t.TempDir()
		writeBindForTest(t, exeDir, target+"\n")

		base, mode, err := resolveDataRoot(exeDir)
		if err != nil {
			t.Fatal(err)
		}
		if mode != modeBound || filepath.Clean(base) != filepath.Clean(target) {
			t.Fatalf("应认绑定 %q, got (%q, %v)", target, base, mode)
		}
		// 绑定生效时不得旁逸出同级 hanxidata
		if _, err := os.Stat(filepath.Join(exeDir, siblingDataDirName)); !os.IsNotExist(err) {
			t.Fatal("认绑定后不应创建同级 hanxidata")
		}
	})

	t.Run("绑定目标不存在则自动创建", func(t *testing.T) {
		exeDir := t.TempDir()
		target := filepath.Join(t.TempDir(), "HanxiData", "deep")
		writeBindForTest(t, exeDir, target)

		base, mode, err := resolveDataRoot(exeDir)
		if err != nil {
			t.Fatal(err)
		}
		if mode != modeBound || base != filepath.Clean(target) {
			t.Fatalf("绑定新家应自动建目录, got (%q, %v)", base, mode)
		}
		if fi, serr := os.Stat(base); serr != nil || !fi.IsDir() {
			t.Fatalf("绑定目标未落盘: %v", serr)
		}
	})

	t.Run("BOM 与尾随空白容忍", func(t *testing.T) {
		exeDir := t.TempDir()
		target := filepath.Join(t.TempDir(), "home")
		writeBindForTest(t, exeDir, "\uFEFF \t"+target+"  \r\n")

		base, _, err := resolveDataRoot(exeDir)
		if err != nil {
			t.Fatal(err)
		}
		if base != filepath.Clean(target) {
			t.Fatalf("BOM/空白应被容忍, got %q", base)
		}
	})

	t.Run("空白绑定文件视为损坏并 fail loud", func(t *testing.T) {
		exeDir := t.TempDir()
		writeBindForTest(t, exeDir, "  \n\r\n")

		_, mode, err := resolveDataRoot(exeDir)
		if err == nil {
			t.Fatal("存在但空白的绑定文件必须报错，不得等同解绑")
		}
		if mode != modeSibling || !strings.Contains(err.Error(), "显式删除") {
			t.Fatalf("错误应说明显式解绑，got (%v, %v)", mode, err)
		}
	})

	t.Run("相对路径声明无效且 fail loud", func(t *testing.T) {
		exeDir := t.TempDir()
		writeBindForTest(t, exeDir, "HanxiData")

		_, _, err := resolveDataRoot(exeDir)
		if err == nil {
			t.Fatal("相对路径声明必须报错，不得静默")
		}
		if !strings.Contains(err.Error(), guideSuffix) {
			t.Fatalf("错误必须携带处置指引: %v", err)
		}
	})

	t.Run("绑定目标被同名文件挡住则 fail loud", func(t *testing.T) {
		exeDir := t.TempDir()
		blocker := filepath.Join(t.TempDir(), "occupied")
		if err := os.WriteFile(blocker, []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
		writeBindForTest(t, exeDir, filepath.Join(blocker, "sub"))

		_, mode, err := resolveDataRoot(exeDir)
		if err == nil {
			t.Fatal("绑定目标不可用时必须报错，不得改道同级")
		}
		if mode != modeBound {
			t.Fatalf("错误快照应保留绑定来源语义, got %v", mode)
		}
	})

	t.Run("hanxi.bind 被同名目录挡住则 fail loud", func(t *testing.T) {
		exeDir := t.TempDir()
		if err := os.Mkdir(filepath.Join(exeDir, bindFileName), 0755); err != nil {
			t.Fatal(err)
		}
		if _, _, err := resolveDataRoot(exeDir); err == nil {
			t.Fatal("指针读取失败必须报错")
		}
	})
}

// TestResolveDataRootSibling 同级默认：裸 exe 双击即活（自动建 hanxidata），
// 已有目录存在即生效；同级不可写时 fail loud，绝不落用户目录。
func TestResolveDataRootSibling(t *testing.T) {
	t.Run("裸 exe 自动创建同级 hanxidata", func(t *testing.T) {
		exeDir := t.TempDir()

		base, mode, err := resolveDataRoot(exeDir)
		if err != nil {
			t.Fatal(err)
		}
		if mode != modeSibling || base != filepath.Join(exeDir, siblingDataDirName) {
			t.Fatalf("裸 exe 应落同级 hanxidata, got (%q, %v)", base, mode)
		}
		if fi, serr := os.Stat(base); serr != nil || !fi.IsDir() {
			t.Fatalf("hanxidata 未自动创建: %v", serr)
		}
	})

	t.Run("已存在的同级 hanxidata 直接生效", func(t *testing.T) {
		exeDir := t.TempDir()
		base := filepath.Join(exeDir, siblingDataDirName)
		if err := os.MkdirAll(filepath.Join(base, "state"), 0755); err != nil {
			t.Fatal(err)
		}

		got, mode, err := resolveDataRoot(exeDir)
		if err != nil || mode != modeSibling || got != base {
			t.Fatalf("既有 hanxidata 应原样生效, got (%q, %v, %v)", got, mode, err)
		}
	})

	t.Run("同级不可写则 fail loud 不落用户目录", func(t *testing.T) {
		exeDir := t.TempDir()
		// 跨平台可造的不可写场景：同名普通文件挡住目录创建
		if err := os.WriteFile(filepath.Join(exeDir, siblingDataDirName), []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}

		_, _, err := resolveDataRoot(exeDir)
		if err == nil {
			t.Fatal("同级不可写必须报错（旧规矩会静默回退 %APPDATA%，新规矩禁止）")
		}
		if !strings.Contains(err.Error(), guideSuffix) {
			t.Fatalf("错误必须引导绑定/搬家: %v", err)
		}
	})
}

// TestLegacyDataDirZeroRecognition 旧 data/ 便携兼容正式废弃（F6 裁定③）：
// 即便携带完整数据根特征，也不得被识别为家、不得参与解析。
func TestLegacyDataDirZeroRecognition(t *testing.T) {
	exeDir := t.TempDir()
	legacy := filepath.Join(exeDir, legacyDataDirName)
	if err := os.MkdirAll(filepath.Join(legacy, "versions"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacy, "config.json"), []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}

	base, mode, err := resolveDataRoot(exeDir)
	if err != nil {
		t.Fatal(err)
	}
	if mode != modeSibling || base != filepath.Join(exeDir, siblingDataDirName) {
		t.Fatalf("旧 data/ 不得被识别为数据根, got (%q, %v)", base, mode)
	}
}

// TestParseBindContent 声明内容解析纯函数边界。
func TestParseBindContent(t *testing.T) {
	// 注意 Windows 口径：filepath.IsAbs 要求带卷名，"\HanxiData" 这类
	// 无盘符路径同样被判无效——绑定声明必须写完整绝对路径。
	abs := filepath.Join(t.TempDir(), "HanxiData")

	if got, declared, err := parseBindContent(nil); err != nil || declared || got != "" {
		t.Fatalf("空内容应判无声明, got (%q, %v, %v)", got, declared, err)
	}
	if got, declared, err := parseBindContent([]byte("\n\n \t\n")); err != nil || declared || got != "" {
		t.Fatalf("全空白应判无声明, got (%q, %v, %v)", got, declared, err)
	}
	if got, declared, err := parseBindContent([]byte(abs + "\n后续行忽略\n")); err != nil || !declared {
		t.Fatalf("首个非空行即声明, got (%q, %v, %v)", got, declared, err)
	} else if filepath.Clean(got) != filepath.Clean(abs) {
		t.Fatalf("解析结果错误: %q", got)
	}
	if _, declared, err := parseBindContent([]byte("./relative")); err == nil || !declared {
		t.Fatalf("相对路径应判无效声明, got (%v, %v)", declared, err)
	}
}

// TestBindFileRoundTrip 指针读写回环 + BindDataDir/UnbindDataDir 参数校验。
// 绑定 API 依赖 os.Executable()，此处只测可注入 exeDir 的文件层与校验层。
func TestBindFileRoundTrip(t *testing.T) {
	exeDir := t.TempDir()
	target := filepath.Join(t.TempDir(), "bound home")

	if got, declared, err := readBindFile(exeDir); err != nil || declared || got != "" {
		t.Fatalf("无指针文件应判无声明, got (%q, %v, %v)", got, declared, err)
	}
	if err := writeBindFile(exeDir, target); err != nil {
		t.Fatal(err)
	}
	got, declared, err := readBindFile(exeDir)
	if err != nil || !declared || got != filepath.Clean(target) {
		t.Fatalf("回读不一致, got (%q, %v, %v)", got, declared, err)
	}

	oldRaw, err := os.ReadFile(filepath.Join(exeDir, bindFileName))
	if err != nil {
		t.Fatal(err)
	}
	originalReplace := replaceAtomicFile
	t.Cleanup(func() { replaceAtomicFile = originalReplace })
	replaceAtomicFile = func(_, _ string) error { return errors.New("injected replace failure") }
	if err := writeBindFile(exeDir, filepath.Join(t.TempDir(), "new target")); err == nil {
		t.Fatal("原子替换故障应返回错误")
	}
	if after, err := os.ReadFile(filepath.Join(exeDir, bindFileName)); err != nil || string(after) != string(oldRaw) {
		t.Fatalf("替换失败必须保留旧绑定: %q %v", after, err)
	}

	// 校验层（不触真实 exe 目录）：空串与非绝对路径必须拒
	if err := BindDataDir("  "); err == nil {
		t.Fatal("空路径应拒绝")
	}
	if err := BindDataDir("relative/path"); err == nil {
		t.Fatal("相对路径应拒绝")
	}
}

// TestBuildPaths 数据根派生布局：同级根与绑定根共用同一形态，子目录恒在根下。
func TestBuildPaths(t *testing.T) {
	base := filepath.Join("D:", "Tools", "hanxi", siblingDataDirName)
	p := buildPaths(modeSibling, base)

	if p.Mode() != modeSibling || p.BaseDir() != base || p.DataDir() != base {
		t.Fatalf("根目录映射错误: %+v", p)
	}
	if p.InitError() != nil {
		t.Fatalf("正常布局不应带解析错误: %v", p.InitError())
	}
	for name, got := range map[string]string{
		"state":                              p.StateDir(),
		"logs":                               p.LogsDir(),
		"versions":                           p.VersionsDir(),
		"runtime":                            p.RuntimeDir(),
		"modules":                            p.ModulesDir(),
		"installers":                         p.InstallersDir(),
		filepath.Join("modules", "receipts"): p.ModulesReceiptsDir(),
		filepath.Join("modules", "journals"): p.ModulesJournalsDir(),
	} {
		if want := filepath.Join(base, name); got != want {
			t.Errorf("%s 子目录派生错误: got %q, want %q", name, got, want)
		}
	}
	if want := filepath.Join(base, "config.json"); p.ConfigFile() != want {
		t.Errorf("ConfigFile 派生错误: got %q, want %q", p.ConfigFile(), want)
	}
}

// TestEnsureDirs 布局初始化幂等。
func TestEnsureDirs(t *testing.T) {
	p := buildPaths(modeSibling, filepath.Join(t.TempDir(), siblingDataDirName))
	if err := ensureDirs(p); err != nil {
		t.Fatal(err)
	}
	if err := ensureDirs(p); err != nil {
		t.Fatalf("二跑应幂等: %v", err)
	}
	for _, d := range []string{p.StateDir(), p.LogsDir(), p.VersionsDir(), p.RuntimeDir(), p.ModulesReceiptsDir()} {
		if fi, err := os.Stat(d); err != nil || !fi.IsDir() {
			t.Errorf("目录未就绪: %s (%v)", d, err)
		}
	}
	// journals/ 与 installers/ 刻意懒建（归消费方 MkdirAll），ensureDirs 不得抢跑
	for _, d := range []string{p.ModulesJournalsDir(), p.InstallersDir()} {
		if _, err := os.Stat(d); !os.IsNotExist(err) {
			t.Errorf("懒建目录不应被 ensureDirs 预建: %s (%v)", d, err)
		}
	}
}

func TestEnsureDirsRejectsDerivedPathFiles(t *testing.T) {
	for _, name := range []string{"state", "logs", "versions"} {
		t.Run(name, func(t *testing.T) {
			base := filepath.Join(t.TempDir(), siblingDataDirName)
			if err := os.MkdirAll(base, 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(base, name), []byte("blocker"), 0644); err != nil {
				t.Fatal(err)
			}
			p := buildPaths(modeSibling, base)
			if err := ensureDirs(p); err == nil {
				t.Fatalf("%s 同名文件必须令目录初始化失败", name)
			}
			p.initErr = ensureDirs(p)
			if p.InitError() == nil {
				t.Fatalf("%s 初始化失败必须进入 InitError", name)
			}
		})
	}

	// receipts 是 Wave 1 起唯一预建的二级目录，同口径受挡必炸
	t.Run("modules/receipts", func(t *testing.T) {
		base := filepath.Join(t.TempDir(), siblingDataDirName)
		if err := os.MkdirAll(filepath.Join(base, "modules"), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(base, "modules", "receipts"), []byte("blocker"), 0644); err != nil {
			t.Fatal(err)
		}
		p := buildPaths(modeSibling, base)
		if err := ensureDirs(p); err == nil {
			t.Fatal("modules/receipts 同名文件必须令目录初始化失败")
		}
	})
}
