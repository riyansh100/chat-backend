package analytics

type SMAState struct {
	Window []float64
	Sum    float64
	Size   int
}

func NewSMAState(size int) *SMAState {
	return &SMAState{
		Window: make([]float64, 0, size),
		Size:   size,
	}
}

func (s *SMAState) Add(price float64) (float64, bool) {
	if len(s.Window) == s.Size {
		oldest := s.Window[0]
		s.Sum -= oldest
		s.Window = s.Window[1:]
	}

	s.Window = append(s.Window, price)
	s.Sum += price

	if len(s.Window) < s.Size {
		return 0, false
	}

	return s.Sum / float64(s.Size), true
}
