package constants

const (
	DotSymbol   = "."
	CommaSymbol = ","
	ColonSymbol = ":"

	TcpProtocol = "tcp"
	UdpProtocol = "udp"

	SysNamespace = "polaris"

	MeshDefaultDnsAnswerIp       = "10.4.4.4"
	MeshDefaultReloadIntervalSec = 30
)

type contextKey string

const (
	ContextProtocol contextKey = "protocol"
)
