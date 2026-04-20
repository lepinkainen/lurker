package irc

type NetworkState string

const (
	StateConnecting   NetworkState = "connecting"
	StateConnected    NetworkState = "connected"
	StateDisconnected NetworkState = "disconnected"
)

func (s NetworkState) String() string { return string(s) }
