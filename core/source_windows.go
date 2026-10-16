package core

import (
	"golang.org/x/sys/windows"
	"net/netip"
	"path/filepath"
	"res-downloader/core/shared"
	"unsafe"
)

var extendedTCPTable = windows.NewLazySystemDLL("iphlpapi.dll").NewProc("GetExtendedTcpTable")

func resolveSourceApplication(remote, local string) shared.ApplicationInfo {
	client, err := netip.ParseAddrPort(remote)
	if err != nil || !client.Addr().IsLoopback() {
		return shared.ApplicationInfo{}
	}
	server, err := netip.ParseAddrPort(local)
	if err != nil {
		return shared.ApplicationInfo{}
	}
	ipv6 := client.Addr().Is6() && !client.Addr().Is4In6()
	family := uintptr(windows.AF_INET)
	if ipv6 {
		family = windows.AF_INET6
	}
	var size uint32
	result, _, _ := extendedTCPTable.Call(0, uintptr(unsafe.Pointer(&size)), 0, family, 5, 0)
	if result != uintptr(windows.ERROR_INSUFFICIENT_BUFFER) {
		return shared.ApplicationInfo{}
	}
	for attempt := 0; attempt < 3; attempt++ {
		if size < 4 || size > 16*1024*1024 {
			return shared.ApplicationInfo{}
		}
		data := make([]byte, size)
		result, _, _ = extendedTCPTable.Call(uintptr(unsafe.Pointer(&data[0])), uintptr(unsafe.Pointer(&size)), 0, family, 5, 0)
		if result == uintptr(windows.ERROR_INSUFFICIENT_BUFFER) {
			continue
		}
		if result != 0 {
			return shared.ApplicationInfo{}
		}
		pid := tcpTablePID(data, ipv6, client, server)
		if pid == 0 {
			return shared.ApplicationInfo{}
		}
		info := shared.ApplicationInfo{PID: pid}
		process, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
		if err != nil {
			return info
		}
		defer windows.CloseHandle(process)
		path := make([]uint16, 32768)
		length := uint32(len(path))
		if windows.QueryFullProcessImageName(process, 0, &path[0], &length) == nil {
			info.Name = filepath.Base(windows.UTF16ToString(path[:length]))
		}
		return info
	}
	return shared.ApplicationInfo{}
}
