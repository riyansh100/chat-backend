package hub

type RedisMessage struct {
	Room       string      `json:"room"`
	Type       string      `json:"type"`
	Data       interface{} `json:"data"`
	Origin     string      `json:"origin"`
	InstanceID string      `json:"instance_id"`
}
