package core

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWindowsActualLoopbackConnectionOwner(t *testing.T) {
	for _, network := range []string{"tcp4", "tcp6"} {
		t.Run(network, func(t *testing.T) {
			addr := "127.0.0.1:0"
			if network == "tcp6" {
				addr = "[::1]:0"
			}
			listener, err := net.Listen(network, addr)
			if err != nil {
				if network == "tcp6" {
					t.Skip("IPv6 unavailable", err)
				}
				t.Fatal(err)
			}
			defer listener.Close()
			client, err := net.Dial(network, listener.Addr().String())
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()
			conn, err := listener.Accept()
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			info := resolveSourceApplication(conn.RemoteAddr().String(), conn.LocalAddr().String())
			executable, _ := os.Executable()
			if info.PID != uint32(os.Getpid()) || !strings.EqualFold(info.Name, filepath.Base(executable)) {
				t.Fatalf("wrong connection owner: %+v", info)
			}
		})
	}
	if info := resolveSourceApplication("192.0.2.1:50001", "127.0.0.1:8899"); info.PID != 0 || info.Name != "" {
		t.Fatal("assigned a remote client to a local process")
	}
}
