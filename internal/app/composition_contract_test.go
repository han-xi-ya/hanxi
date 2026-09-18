package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing"
)

type compositionModule struct {
	ID    string `json:"id"`
	Route string `json:"route"`
	Group string `json:"group"`
}

type orderedCall struct {
	Before string `json:"before"`
	After  string `json:"after"`
}

type compositionContract struct {
	Modules []compositionModule `json:"modules"`
	Events  []string            `json:"events"`
	Wiring  struct {
		PreactivatedModules []string      `json:"preactivatedModules"`
		OrderedCalls        []orderedCall `json:"orderedCalls"`
	} `json:"wiring"`
	Counts struct {
		Modules int `json:"modules"`
		Events  int `json:"events"`
	} `json:"counts"`
}

func TestCompositionContractModules(t *testing.T) {
	root := repositoryRoot(t)
	contract := loadCompositionContract(t, root)
	appSource := readText(t, filepath.Join(root, "internal", "app", "app.go"))

	registered := extractRegisteredModuleConstructors(t, appSource)
	actual := make([]compositionModule, 0, len(registered))
	for _, packageName := range registered {
		moduleSource := readText(t, filepath.Join(root, "internal", "modules", packageName, "module.go"))
		actual = append(actual, extractModuleDescriptor(t, packageName, moduleSource))
	}
	sortModules(actual)

	want := append([]compositionModule(nil), contract.Modules...)
	sortModules(want)
	if diff := diffLines(formatModules(want), formatModules(actual)); diff != "" {
		t.Fatalf("后端注册模块 ID/Nav route/group 与 composition contract 不一致 (-want +got):\n%s", diff)
	}
	if len(actual) != contract.Counts.Modules {
		t.Fatalf("模块数量辅助断言: got %d, want %d", len(actual), contract.Counts.Modules)
	}
}

func TestCompositionContractEvents(t *testing.T) {
	root := repositoryRoot(t)
	contract := loadCompositionContract(t, root)
	registered := extractMatches(
		readText(t, filepath.Join(root, "internal", "app", "app.go")),
		regexp.MustCompile(`RegisterEvent[^\n]*\("([^"]+)"\)`),
	)
	sort.Strings(registered)
	want := append([]string(nil), contract.Events...)
	sort.Strings(want)
	if diff := diffLines(want, registered); diff != "" {
		t.Fatalf("RegisterEvents 关键集合与 composition contract 不一致 (-want +got):\n%s", diff)
	}
	if len(registered) != contract.Counts.Events {
		t.Fatalf("事件数量辅助断言: got %d, want %d", len(registered), contract.Counts.Events)
	}

	emitted := extractEmittedEvents(t, root)
	sort.Strings(emitted)
	if diff := diffLines(want, emitted); diff != "" {
		t.Fatalf("实际模块 emit 事件集合与 composition contract 不一致 (-want +got):\n%s", diff)
	}
}

func TestCompositionContractCriticalWiring(t *testing.T) {
	root := repositoryRoot(t)
	contract := loadCompositionContract(t, root)
	appSource := readText(t, filepath.Join(root, "internal", "app", "app.go"))

	actualPreactivated := extractMatches(appSource, regexp.MustCompile(`registry\.EnsureActive\("([^"]+)"\)`))
	sort.Strings(actualPreactivated)
	wantPreactivated := append([]string(nil), contract.Wiring.PreactivatedModules...)
	sort.Strings(wantPreactivated)
	if diff := diffLines(wantPreactivated, actualPreactivated); diff != "" {
		t.Fatalf("关键预激活集合不一致 (-want +got):\n%s", diff)
	}

	for _, ordered := range contract.Wiring.OrderedCalls {
		before := strings.Index(appSource, ordered.Before)
		after := strings.Index(appSource, ordered.After)
		if before < 0 || after < 0 {
			t.Errorf("关键装配调用缺失: before=%q (index=%d), after=%q (index=%d)", ordered.Before, before, ordered.After, after)
			continue
		}
		if before >= after {
			t.Errorf("关键装配顺序错误: %q 必须早于 %q", ordered.Before, ordered.After)
		}
	}
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("无法定位测试文件")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
}

func loadCompositionContract(t *testing.T, root string) compositionContract {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "scripts", "fixture", "composition_contract.json"))
	if err != nil {
		t.Fatalf("读取 composition contract: %v", err)
	}
	var contract compositionContract
	if err := json.Unmarshal(data, &contract); err != nil {
		t.Fatalf("解析 composition contract: %v", err)
	}
	return contract
}

func readText(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取 %s: %v", path, err)
	}
	return string(data)
}

func extractRegisteredModuleConstructors(t *testing.T, source string) []string {
	t.Helper()
	start := strings.Index(source, "modulesToRegister := []extapi.Module{")
	if start < 0 {
		t.Fatal("未找到 modulesToRegister 装配清单")
	}
	end := strings.Index(source[start:], "\n\t}")
	if end < 0 {
		t.Fatal("modulesToRegister 装配清单未闭合")
	}
	block := source[start : start+end]
	constructors := extractMatches(block, regexp.MustCompile(`(?m)^\s*([a-z][a-z0-9]*)\.New\(`))
	// 四个被装配根持有引用的模块以变量入表，不以 package.New(...) 直接出现。
	constructors = append(constructors, "ocr", "portkill", "fileshare", "quickmenu", "msgboard")
	if strings.Contains(source, "modulesToRegister = append(modulesToRegister, memoModule)") {
		constructors = append(constructors, "memo")
	}
	return constructors
}

func extractModuleDescriptor(t *testing.T, packageName, source string) compositionModule {
	t.Helper()
	id := firstMatch(t, packageName+" ID", source, regexp.MustCompile(`const ID = "([^"]+)"`))
	route := firstMatch(t, packageName+" Nav route", source, regexp.MustCompile(`Route:\s*"([^"]+)"`))
	group := strings.ToLower(firstMatch(t, packageName+" Nav group", source, regexp.MustCompile(`Group:\s*extapi\.Group([A-Za-z]+)`)))
	return compositionModule{ID: id, Route: route, Group: group}
}

func firstMatch(t *testing.T, label, source string, re *regexp.Regexp) string {
	t.Helper()
	match := re.FindStringSubmatch(source)
	if len(match) != 2 {
		t.Fatalf("未找到 %s", label)
	}
	return match[1]
}

func extractMatches(source string, re *regexp.Regexp) []string {
	matches := re.FindAllStringSubmatch(source, -1)
	out := make([]string, 0, len(matches))
	for _, match := range matches {
		out = append(out, match[1])
	}
	return out
}

func extractEmittedEvents(t *testing.T, root string) []string {
	t.Helper()
	literalEmit := regexp.MustCompile(`(?:\.Emit|emit|emitEvent)\(\s*"([a-z][a-z0-9-]*:[^"]+)"`)
	constEvent := regexp.MustCompile(`(?m)const\s+(?:\([^)]*\)|[A-Za-z][A-Za-z0-9_]*\s*=\s*"[^"]+")`)
	constAssignment := regexp.MustCompile(`([A-Za-z][A-Za-z0-9_]*)\s*=\s*"([a-z][a-z0-9-]*:[^"]+)"`)
	constEmit := regexp.MustCompile(`(?:\.Emit|emit|emitEvent)\(\s*([A-Za-z][A-Za-z0-9_]*)\b`)

	emitted := map[string]bool{}
	err := filepath.WalkDir(filepath.Join(root, "internal"), func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") || path == filepath.Join(root, "internal", "app", "app.go") {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		source := string(data)
		for _, event := range extractMatches(source, literalEmit) {
			emitted[event] = true
		}

		constants := map[string]string{}
		for _, block := range constEvent.FindAllString(source, -1) {
			for _, match := range constAssignment.FindAllStringSubmatch(block, -1) {
				constants[match[1]] = match[2]
			}
		}
		for _, match := range constEmit.FindAllStringSubmatch(source, -1) {
			if event := constants[match[1]]; event != "" {
				emitted[event] = true
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("扫描事件 emit: %v", err)
	}

	out := make([]string, 0, len(emitted))
	for event := range emitted {
		out = append(out, event)
	}
	return out
}

func sortModules(modules []compositionModule) {
	sort.Slice(modules, func(i, j int) bool { return modules[i].ID < modules[j].ID })
}

func formatModules(modules []compositionModule) []string {
	out := make([]string, 0, len(modules))
	for _, module := range modules {
		out = append(out, module.ID+"|"+module.Route+"|"+module.Group)
	}
	return out
}

func diffLines(want, got []string) string {
	wantSet := make(map[string]bool, len(want))
	gotSet := make(map[string]bool, len(got))
	for _, line := range want {
		wantSet[line] = true
	}
	for _, line := range got {
		gotSet[line] = true
	}
	var diff []string
	for _, line := range want {
		if !gotSet[line] {
			diff = append(diff, "- "+line)
		}
	}
	for _, line := range got {
		if !wantSet[line] {
			diff = append(diff, "+ "+line)
		}
	}
	return strings.Join(diff, "\n")
}
