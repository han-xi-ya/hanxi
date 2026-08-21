//go:build windows

package windows_test

import (
	"context"
	"os"
	"testing"
	"time"

	"hubkit/internal/platform"
	"hubkit/internal/platform/windows"
)

func TestWindowsPlatform(t *testing.T) {
	plat, err := windows.New()
	if err != nil {
		t.Fatalf("windows.New failed: %v", err)
	}

	t.Run("Network Adapters", func(t *testing.T) {
		adapters, err := plat.Network().Adapters()
		if err != nil {
			t.Fatalf("Adapters failed: %v", err)
		}
		if len(adapters) == 0 {
			t.Log("Warning: no network adapters found")
		} else {
			t.Logf("Found %d adapters, first: %s (IPs: %v)", len(adapters), adapters[0].Name, adapters[0].IPv4)
		}
	})

	t.Run("TCP & UDP Table", func(t *testing.T) {
		tcpRows, err := plat.Port().TCPTable(platform.FamilyIPv4)
		if err != nil {
			t.Fatalf("TCPTable failed: %v", err)
		}
		t.Logf("Found %d IPv4 TCP entries", len(tcpRows))

		udpRows, err := plat.Port().UDPTable(platform.FamilyIPv4)
		if err != nil {
			t.Fatalf("UDPTable failed: %v", err)
		}
		t.Logf("Found %d IPv4 UDP entries", len(udpRows))
	})

	t.Run("Process Query Self & Protected", func(t *testing.T) {
		myPid := uint32(os.Getpid())
		info, err := plat.Process().Query(myPid)
		if err != nil {
			t.Fatalf("Query self failed: %v", err)
		}
		t.Logf("Self PID: %d, Name: %s, ExePath: %s", info.PID, info.Name, info.ExePath)

		if !plat.Process().IsProtected(myPid, info) {
			t.Errorf("Self process should be marked as protected")
		}
		if !plat.Process().IsProtected(0, platform.ProcInfo{PID: 0}) {
			t.Errorf("PID 0 should be marked as protected")
		}
	})

	t.Run("Job Object Creation", func(t *testing.T) {
		job, err := plat.Job().Create()
		if err != nil {
			t.Fatalf("Job.Create failed: %v", err)
		}
		defer job.Close()
		t.Log("Job object created successfully")
	})

	t.Run("Ping Localhost", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		rtt, ok, err := plat.Network().Ping(ctx, "127.0.0.1", 1*time.Second)
		if err != nil {
			t.Fatalf("Ping failed: %v", err)
		}
		t.Logf("Ping 127.0.0.1: ok=%v, rtt=%v", ok, rtt)
	})
}
