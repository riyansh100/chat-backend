package trading

import (
	"errors"

	"github.com/riyansh/chat-backend/internal/domain/common"
)

func ValidateAndTranslate(
	env common.Envelope,
	role Role,
) ([]Event, error) {

	// Only INGESTORs can publish trading data
	if role != RoleIngestor {
		return nil, common.ErrNonFatal
	}

	switch env.Type {

	case TypePriceUpdate:
		instrument, ok := env.Body["instrument"].(string)
		if !ok || instrument == "" {
			return nil, common.ErrNonFatal
		}

		price, ok := env.Body["price"].(float64)
		if !ok {
			return nil, common.ErrNonFatal
		}

		ts, ok := env.Body["ts"].(float64) // JSON numbers decode as float64
		if !ok {
			return nil, common.ErrNonFatal
		}

		// --- NEW STEP: lookup InstrumentID from metadata ---
		id, ok := SymbolToID[instrument]
		if !ok {
			return nil, errors.New("unknown instrument")
		}

		//fmt.Println("VALIDATED:", instrument)

		return []Event{
			PriceUpdateEvent{
				Instrument:   instrument,
				InstrumentID: id, // <-- NEW FIELD
				Price:        price,
				Timestamp:    int64(ts),
			},
		}, nil
	}

	return nil, common.ErrNonFatal
}
