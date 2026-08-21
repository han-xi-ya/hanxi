package portscan

import (
	"context"
	"net"
	"testing"
	"time"
)

func TestParsePortRange(t *testing.T) {
	tests := []struct {
		input    string
		expected []int
		hasErr   bool
	}{
		{"80,443", []int{80, 443}, false},
		{"8000-8003", []int{8000, 8001, 8002, 8003}, false},
		{"80, 8080, 9000-9002", []int{80, 8080, 9000, 9001, 9002}, false},
		{"80, 80, 443", []int{80, 443}, false}, // 去重
		{"abc", nil, true},
		{"-1", nil, true},
		{"70000", nil, true},
	}

	for _, tc := range tests {
		got, err := ParsePortRange(tc.input)
		if tc.hasErr {
			if err == nil {
				t.Fatalf("expected error for input %q, got nil", tc.input)
			}
			continue
		}
		if err != nil {
			t.Fatalf("unexpected error for input %q: %v", tc.input, err)
		}
		if len(got) != len(tc.expected) {
			t.Fatalf("input %q: expected %v, got %v", tc.input, tc.expected, got)
		}
		for i := range got {
			if got[i] != tc.expected[i] {
				t.Fatalf("input %q: at index %d expected %d, got %d", tc.input, i, tc.expected[i], got[i])
			}
		}
	}
}

func TestScannerScanLocalPort(t *testing.T) {
	// 启动一个临时的 local TCP 监听端口用于测试
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("cannot listen on local port: %v", err)
	}
	defer l.Close()

	port := l.Addr().(*net.TCPAddr).Port

	scanner := NewScanner()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	summary, err := scanner.ExecuteScan(
		ctx,
		"test_1",
		"127.0.0.1",
		[]int{port, port + 1},
		"",
		500*time.Millisecond,
		10,
		0,
		false,
		nil,
	)

	if err != nil {
		t.Fatalf("scan failed: %v", err)
	}

	if len(summary.OpenPorts) != 1 {
		t.Fatalf("expected 1 open port, got %d", len(summary.OpenPorts))
	}
	if summary.OpenPorts[0].Port != port {
		t.Fatalf("expected open port %d, got %d", port, summary.OpenPorts[0].Port)
	}
}
