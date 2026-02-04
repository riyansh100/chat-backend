package chat

import (
	"github.com/riyansh/chat-backend/internal/domain/common"
)

func ValidateAndTranslate(
	env common.Envelope,
	clientRooms map[string]bool,
) ([]Event, error) {

	switch env.Type {

	case TypeJoin:
		room, ok := env.Body["room"].(string)
		if !ok || room == "" {
			return nil, common.ErrNonFatal
		}
		return []Event{JoinEvent{Room: room}}, nil

	case TypeLeave:
		room, ok := env.Body["room"].(string)
		if !ok || room == "" {
			return nil, common.ErrNonFatal
		}
		return []Event{LeaveEvent{Room: room}}, nil

	case TypeMessage:
		room, ok := env.Body["room"].(string)
		if !ok || room == "" {
			return nil, common.ErrNonFatal
		}

		if !clientRooms[room] {
			return nil, common.ErrNonFatal
		}

		data, ok := env.Body["data"].(string)
		if !ok {
			return nil, common.ErrNonFatal
		}

		return []Event{
			MessageEvent{
				Room: room,
				Data: data,
			},
		}, nil
	}

	return nil, common.ErrNonFatal
}
