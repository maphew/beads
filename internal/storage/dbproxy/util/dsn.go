package util

import (
	"fmt"
	"time"

	mysql "github.com/go-sql-driver/mysql"
)

// maxAllowedPacketBytes mirrors go-sql-driver/mysql's unexported
// defaultMaxAllowedPacket (64 MiB).
const maxAllowedPacketBytes = 64 << 20

type DoltServerDSN struct {
	Socket          string
	Host            string
	Port            int
	User            string
	Password        string //nolint:gosec // G117: MySQL DSN password field; required by the connection-string builder, not serialized as JSON
	Database        string
	Timeout         time.Duration
	TLSRequired     bool
	TLSCert         string
	TLSKey          string
	TLSConfigName   string
	ClientFoundRows bool
}

func (d DoltServerDSN) String() string {
	timeout := d.Timeout
	if timeout == 0 {
		timeout = 5 * time.Second
	}

	net := "tcp"
	addr := fmt.Sprintf("%s:%d", d.Host, d.Port)
	if d.Socket != "" {
		net = "unix"
		addr = d.Socket
	}

	cfg := mysql.Config{
		User:            d.User,
		Passwd:          d.Password,
		Net:             net,
		Addr:            addr,
		DBName:          d.Database,
		ParseTime:       true,
		MultiStatements: true,
		// Same reason as internal/storage/doltutil.ServerDSN: this config is a
		// composite literal, so without an explicit value FormatDSN emits
		// maxAllowedPacket=0 and the driver spends an extra round-trip on
		// "SELECT @@max_allowed_packet" for every new connection. The value is
		// the driver's own default (64 MiB), so the packet limit is unchanged.
		MaxAllowedPacket:     maxAllowedPacketBytes,
		Timeout:              timeout,
		AllowNativePasswords: true,
		ClientFoundRows:      d.ClientFoundRows,
	}
	switch {
	case d.TLSConfigName != "":
		cfg.TLSConfig = d.TLSConfigName
	case d.TLSRequired:
		cfg.TLSConfig = "true"
	default:
		cfg.TLSConfig = "false"
	}

	return cfg.FormatDSN()
}
