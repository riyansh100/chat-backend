package chat

// Domain-level intents (NO hub dependency)

type Event interface{}

type JoinEvent struct {
	Room string
}

type LeaveEvent struct {
	Room string
}

type MessageEvent struct {
	Room string
	Data string
}
