package hub

type Message struct {
	Type string `json:"type"` // join | leave | message

	Room string      `json:"room"`
	Data interface{} `json:"data"`
}
