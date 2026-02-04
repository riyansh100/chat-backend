package trading

// Role defines what a connected client is allowed to do
type Role string

const (
	RoleIngestor Role = "INGESTOR" // market data, orders, feeds
	RoleConsumer Role = "CONSUMER" // traders, dashboards, algos
)
