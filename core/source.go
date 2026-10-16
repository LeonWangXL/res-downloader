package core

import (
	"context"
	"encoding/binary"
	"net"
	"net/netip"
	"res-downloader/core/shared"
)

func sourceConnectionContext(ctx context.Context, conn net.Conn) context.Context {
	return shared.WithApplication(ctx, resolveSourceApplication(conn.RemoteAddr().String(), conn.LocalAddr().String()))
}

// Match both endpoints: a client port alone can belong to an unrelated connection.
func tcpTablePID(data []byte, ipv6 bool, client, server netip.AddrPort) uint32 {
	rowSize, localAddr, localPort, remoteAddr, remotePort, pidOffset := 24, 4, 8, 12, 16, 20
	if ipv6 {
		rowSize, localAddr, localPort, remoteAddr, remotePort, pidOffset = 56, 0, 20, 24, 44, 52
	}
	if len(data) < 4 {
		return 0
	}
	count := uint64(binary.LittleEndian.Uint32(data[:4]))
	if count > uint64((len(data)-4)/rowSize) {
		return 0
	}
	for i := 0; i < int(count); i++ {
		row := data[4+i*rowSize : 4+(i+1)*rowSize]
		if binary.BigEndian.Uint16(row[localPort:]) != client.Port() || binary.BigEndian.Uint16(row[remotePort:]) != server.Port() {
			continue
		}
		length := 4
		if ipv6 {
			length = 16
		}
		local, _ := netip.AddrFromSlice(row[localAddr : localAddr+length])
		remote, _ := netip.AddrFromSlice(row[remoteAddr : remoteAddr+length])
		if local.Unmap() == client.Addr().Unmap() && remote.Unmap() == server.Addr().Unmap() {
			if pid := binary.LittleEndian.Uint32(row[pidOffset:]); pid != 0 {
				return pid
			}
		}
	}
	return 0
}
