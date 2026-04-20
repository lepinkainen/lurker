package irc

// NetworkState describes the current IRC connection state for a network.
type NetworkState string

const (
	// StateConnecting means the network is attempting to connect.
	StateConnecting NetworkState = "connecting"
	// StateConnected means the network is currently connected.
	StateConnected NetworkState = "connected"
	// StateDisconnected means the network is currently disconnected.
	StateDisconnected NetworkState = "disconnected"
)

func (s NetworkState) String() string { return string(s) }
