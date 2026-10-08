package pprofconfig

import "net"

const (
	defaultHost         = "127.0.0.1"
	serverPortEnv       = "SERVER_PPROF_PORT"
	ingestorPortEnv     = "INGESTOR_PPROF_PORT"
	defaultServerPort   = "6060"
	defaultIngestorPort = "6061"
)

// Enabled reports whether pprof was explicitly enabled with the exact value "true".
func Enabled(getenv func(string) string) bool {
	return getenv("ENABLE_PPROF") == "true"
}

// ServerAddress returns the server's pprof listen address.
func ServerAddress(getenv func(string) string) string {
	return address(getenv, serverPortEnv, defaultServerPort)
}

// IngestorAddress returns the ingestor's pprof listen address.
func IngestorAddress(getenv func(string) string) string {
	return address(getenv, ingestorPortEnv, defaultIngestorPort)
}

func address(getenv func(string) string, portEnv, defaultPort string) string {
	host := getenv("PPROF_HOST")
	if host == "" {
		host = defaultHost
	}
	port := getenv(portEnv)
	if port == "" {
		port = defaultPort
	}
	return net.JoinHostPort(host, port)
}
