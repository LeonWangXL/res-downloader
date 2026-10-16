package core

import (
	"encoding/binary"
	"net/netip"
	"testing"
)

func TestTCPTableMatchesCompleteConnection(t *testing.T) {
	for _, ipv6 := range []bool{false, true} {
		client := netip.MustParseAddrPort("127.0.0.1:50001")
		server := netip.MustParseAddrPort("127.0.0.1:8899")
		size, la, lp, ra, rp, pid := 24, 4, 8, 12, 16, 20
		if ipv6 {
			client = netip.MustParseAddrPort("[::1]:50001")
			server = netip.MustParseAddrPort("[::1]:8899")
			size, la, lp, ra, rp, pid = 56, 0, 20, 24, 44, 52
		}
		table := make([]byte, 4+size)
		binary.LittleEndian.PutUint32(table, 1)
		row := table[4:]
		copy(row[la:], client.Addr().AsSlice())
		copy(row[ra:], server.Addr().AsSlice())
		binary.BigEndian.PutUint16(row[lp:], client.Port())
		binary.BigEndian.PutUint16(row[rp:], server.Port())
		binary.LittleEndian.PutUint32(row[pid:], 1234)
		if got := tcpTablePID(table, ipv6, client, server); got != 1234 {
			t.Fatalf("IPv6=%v got PID %d", ipv6, got)
		}
		wrongServer := netip.AddrPortFrom(server.Addr(), 8900)
		if tcpTablePID(table, ipv6, client, wrongServer) != 0 {
			t.Fatal("matched only client port")
		}
		binary.LittleEndian.PutUint32(table, 999999)
		if tcpTablePID(table, ipv6, client, server) != 0 {
			t.Fatal("accepted truncated table")
		}
	}
	if tcpTablePID(nil, false, netip.AddrPort{}, netip.AddrPort{}) != 0 {
		t.Fatal("accepted empty table")
	}
}
