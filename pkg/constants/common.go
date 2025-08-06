package constants

const (
	DotSymbol   = "."
	CommaSymbol = ","
	ColonSymbol = ":"

	TcpProtocol = "tcp"
	UdpProtocol = "udp"

	SysNamespace = "polaris"
)

type contextKey string

const (
	ContextProtocol contextKey = "protocol"
)
