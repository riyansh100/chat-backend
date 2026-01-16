package hub

type RedisMessage struct {
	Room   string `json:"room"`
	Type   string `json:"type"`
	Data   string `json:"data"`
	Origin string `json:"origin"`
}
