package hub

type Room struct {
	Name    string
	Clients map[*Client]bool
}
