package peicon

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

func TestDbgTmp(t *testing.T) {
	matches, _ := filepath.Glob(filepath.FromSlash("../../../bin/hanxidata/versions/rammap_*/RAMMap64.exe"))
	if len(matches) == 0 {
		t.Skip()
	}
	data, _ := os.ReadFile(matches[0])
	p, err := parsePE(data)
	if err != nil {
		t.Fatal(err)
	}
	root, err := p.resourceRoot()
	if err != nil {
		t.Fatal(err)
	}
	l1, _ := readDir(root, 0, 0)
	t.Logf("types: %+v", l1)
	for _, ty := range l1 {
		l2, err := readDir(root, ty.childOff, 1)
		t.Logf("type %d: n=%d err=%v", ty.id, len(l2), err)
		for _, nm := range l2 {
			pl, ok := p.resolvePayload(root, nm)
			if !ok {
				t.Logf("  name %d: payload FAIL", nm.id)
				continue
			}
			f := frameFromPayload(pl)
			if f == nil {
				t.Logf("  name %d: len=%d head=% x (frame nil)", nm.id, len(pl), pl[:min(len(pl), 16)])
				continue
			}
			t.Logf("  name %d: %dx%d len=%d png=%v", nm.id, f.width, f.height, len(pl), f.isPNG())
		}
	}
	_ = binary.LittleEndian
}
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
