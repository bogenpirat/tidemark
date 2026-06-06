package snmp

import "fmt"

const (
	// OIDIfHCInOctets is the 64-bit input octet counter per interface (RFC 2863).
	OIDIfHCInOctets = "1.3.6.1.2.1.31.1.1.1.6"
	// OIDIfHCOutOctets is the 64-bit output octet counter per interface (RFC 2863).
	OIDIfHCOutOctets = "1.3.6.1.2.1.31.1.1.1.10"
)

// BuildOID appends the interface index to an OID base string.
func BuildOID(oidBase string, interfaceIndex int) string {
	return fmt.Sprintf("%s.%d", oidBase, interfaceIndex)
}
