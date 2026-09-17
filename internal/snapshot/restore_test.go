package snapshot

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// fakeEngine 只测服务层路由与门卫，不触真 git。
type fakeEngine struct {
	data    []byte
	calls   []string
	fileErr error
}

func (f *fakeEngine) mode() string { return ModeGit }
func (f *fakeEngine) changes(context.Context) ([]string, error) {
	return nil, nil
}
func (f *fakeEngine) commit(context.Context, []string) error { return nil }
func (f *fakeEngine) revisions(context.Context, int) ([]Revision, error) {
	return nil, nil
}
func (f *fakeEngine) revisionFiles(context.Context, string) ([]RevisionFile, error) {
	return nil, nil
}
func (f *fakeEngine) file(ctx context.Context, id, rel string) ([]byte, error) {
	f.calls = append(f.calls, "file:"+id+":"+rel)
	if f.fileErr != nil {
		return nil, f.fileErr
	}
	return f.data, nil
}
func (f *fakeEngine) heal(context.Context) {}

func TestRestoreFileRoutesMemoToHotRestorer(t *testing.T) {
	var gotID, gotContent string
	svc := &CheckpointService{eng: &fakeEngine{data: []byte("---\nid: memo_9\n---\n回滚内容")}}
	svc.SetMemoRestorer(func(id, content string) error {
		gotID, gotContent = id, content
		return nil
	})

	if err := svc.RestoreFile("aaaabbbb1234", "memo/memo_9.md"); err != nil {
		t.Fatal(err)
	}
	if gotID != "memo_9" || gotContent != "---\nid: memo_9\n---\n回滚内容" {
		t.Fatalf("hook 入参 = %q %q", gotID, gotContent)
	}

	// 未注入钩子时退回直写盘语义（paths 为 nil 会炸——只验证门卫顺序不 panic：
	// 注入错误返回的钩子即可）
	svc.SetMemoRestorer(func(string, string) error { return errors.New("回落模式") })
	if err := svc.RestoreFile("aaaabbbb1234", "memo/memo_9.md"); err == nil {
		t.Fatal("钩子错误应透传")
	}
}

func TestRestoreFileGuards(t *testing.T) {
	svc := &CheckpointService{eng: &fakeEngine{}}
	if err := svc.RestoreFile("zzz", "config.json"); err == nil {
		t.Error("非法版本标识应拒")
	}
	if err := svc.RestoreFile("aaaabbbb", "../evil.json"); err == nil {
		t.Error("穿越路径应拒")
	}
	if err := svc.RestoreFile("aaaabbbb", "runtime/frpc/x.toml"); err == nil {
		t.Error("白名单外路径应拒")
	}
	svc2 := &CheckpointService{}
	if _, err := svc2.ListRevisions(); err == nil {
		t.Error("未启动服务应给可读错误")
	}
	if err := svc2.CheckpointNow(); err == nil {
		t.Error("未启动服务手动快照应给可读错误")
	}
}

func TestWriteRestoredFileAtomic(t *testing.T) {
	dir := t.TempDir()
	if err := writeRestoredFile(dir, "state/new/deep.json", []byte(`{"v":2}`)); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "state", "new", "deep.json"))
	if err != nil || string(data) != `{"v":2}` {
		t.Fatalf("%q %v", data, err)
	}
	// 不残留 tmp 文件
	entries, _ := os.ReadDir(filepath.Join(dir, "state", "new"))
	for _, e := range entries {
		if filepath.Ext(e.Name()) != ".json" {
			t.Fatalf("残留临时文件: %s", e.Name())
		}
	}
}
