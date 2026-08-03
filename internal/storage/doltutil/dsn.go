package doltutil

import (
	"fmt"
	"time"

	mysql "github.com/go-sql-driver/mysql"
)

// maxAllowedPacketBytes mirrors go-sql-driver/mysql's unexported
// defaultMaxAllowedPacket (64 MiB). See the field comment in ServerDSN.String
// for why it has to be set explicitly.
const maxAllowedPacketBytes = 64 << 20

// ServerDSN holds connection parameters for building a MySQL DSN to a Dolt server.
// All DSNs built with this struct set parseTime=true and multiStatements=true.
type ServerDSN struct {
	Socket   string // Unix domain socket path; when set, Net="unix" and Host/Port are ignored
	Host     string
	Port     int
	User     string
	Password string        //nolint:gosec // G117: MySQL DSN password field; required by the connection-string builder, not serialized as JSON
	Database string        // optional; empty connects without selecting a database
	Timeout  time.Duration // connect timeout; 0 defaults to 5s
	TLS      bool
}

// String builds the MySQL DSN string. Always sets parseTime=true,
// multiStatements=true, allowNativePasswords=true, and a connect timeout.
func (d ServerDSN) String() string {
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
		// InterpolateParams renders bound parameters into the SQL client-side, so
		// a parameterized query is a single round-trip instead of a server-side
		// PREPARE + EXECUTE pair. Over a high-latency connection (e.g. a remote
		// TLS-fronted Dolt server), a write that issues many parameterized
		// statements — such as creating an issue and its labels, events, and
		// dependencies inside one transaction — otherwise pays a full round-trip
		// per statement for the prepare alone. The driver falls back to a
		// server-side prepare (driver.ErrSkip) whenever it cannot safely
		// interpolate an argument, so results never change; this only removes the
		// extra round-trip when interpolation is safe. Independent of
		// MultiStatements. The driver rejects it only with custom unsafe
		// collations, which this DSN never sets.
		InterpolateParams: true,
		// MaxAllowedPacket must be set explicitly because this config is built
		// as a composite literal rather than via mysql.NewConfig(), which is
		// what normally supplies the driver's defaults. Left at the zero value,
		// FormatDSN emits maxAllowedPacket=0, and the driver then runs
		// "SELECT @@max_allowed_packet" while establishing every new connection
		// (connector.go: the probe is taken whenever cfg.MaxAllowedPacket <= 0).
		// That is an extra round-trip per connection, paid on every pool
		// expansion — the cost that matters against a high-latency remote Dolt
		// server, where connections are opened far more often than the packet
		// limit ever changes.
		//
		// The value is the driver's own defaultMaxAllowedPacket (64 MiB), so
		// this pins the same limit a mysql.NewConfig() caller already gets and
		// changes no packet-size behavior. It is duplicated here rather than
		// referenced because the driver keeps the constant unexported. Note
		// that FormatDSN omits the parameter entirely when it equals that
		// default, and ParseDSN starts from NewConfig(), so the value survives
		// the round-trip through the DSN string even though it is not written
		// into it.
		MaxAllowedPacket:     maxAllowedPacketBytes,
		Timeout:              timeout,
		AllowNativePasswords: true,
	}
	if d.TLS {
		cfg.TLSConfig = "true"
	} else {
		// go-sql-driver/mysql v1.8+ defaults to tls=preferred when TLSConfig
		// is empty. Dolt servers without TLS reject preferred-mode negotiation
		// with "TLS requested but server does not support TLS". Explicitly
		// disable TLS so connections work against non-TLS Dolt instances.
		cfg.TLSConfig = "false"
	}

	return cfg.FormatDSN()
}
