package app

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		name string
		b    int64
		want string
	}{
		{"zero", 0, "0 B"},
		{"bytes", 512, "512 B"},
		{"one KiB", 1024, "1.0 KiB"},
		{"KiB", 1536, "1.5 KiB"},
		{"one MiB", 1 << 20, "1.0 MiB"},
		{"MiB", 5 * (1 << 20), "5.0 MiB"},
		{"one GiB", 1 << 30, "1.0 GiB"},
		{"GiB", 3 * (1 << 30), "3.0 GiB"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, formatBytes(tt.b))
		})
	}
}

func TestConnectedStr(t *testing.T) {
	assert.Equal(t, "Connected", connectedStr(true))
	assert.Equal(t, "Disconnected", connectedStr(false))
}

func TestICMPChecksum(t *testing.T) {
	// Standard ICMP echo request: type=8, code=0, checksum=0, id=0, seq=0
	data := make([]byte, 8)
	data[0] = 8 // Echo request

	hi, lo := icmpChecksum(data)
	data[2], data[3] = hi, lo

	// Verify: recomputing checksum over the packet with checksum filled in should yield 0.
	var sum uint32
	for i := 0; i < len(data)-1; i += 2 {
		sum += uint32(data[i])<<8 | uint32(data[i+1])
	}
	for sum>>16 != 0 {
		sum = (sum & 0xFFFF) + (sum >> 16)
	}
	assert.Equal(t, uint16(0xFFFF), uint16(sum), "checksum verification should produce 0xFFFF")
}

func TestICMPChecksum_OddLength(t *testing.T) {
	data := []byte{8, 0, 0, 0, 0, 0, 0}
	hi, lo := icmpChecksum(data)

	// Verify by recomputing with checksum set
	data[2], data[3] = hi, lo
	var sum uint32
	for i := 0; i < len(data)-1; i += 2 {
		sum += uint32(data[i])<<8 | uint32(data[i+1])
	}
	sum += uint32(data[len(data)-1]) << 8
	for sum>>16 != 0 {
		sum = (sum & 0xFFFF) + (sum >> 16)
	}
	assert.Equal(t, uint16(0xFFFF), uint16(sum), "odd-length checksum verification should produce 0xFFFF")
}

func TestSortedKeys(t *testing.T) {
	m := map[string]struct{}{
		"charlie": {},
		"alpha":   {},
		"bravo":   {},
	}
	assert.Equal(t, []string{"alpha", "bravo", "charlie"}, sortedKeys(m))
}

func TestSortedKeys_Empty(t *testing.T) {
	assert.Empty(t, sortedKeys(nil))
}
