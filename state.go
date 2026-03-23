package fsm

import (
	"github.com/coregx/ahocorasick"
)

type State struct {
	NextStates []NextState
	ac         *ahocorasick.Automaton
}

type StateName string

type NextState struct {
	Name        StateName
	Switch      Switch
	DataHandler func(any, []byte, []byte) ([]byte, error)
}

func (s *State) compile() {
	if len(s.NextStates) == 0 {
		return
	}

	builder := ahocorasick.NewBuilder()
	for _, ns := range s.NextStates {
		builder.AddPattern(ns.Switch.Trigger)
	}
	s.ac, _ = builder.Build()
}

// index returns the minimal index within the available triggers for
// current state or -1 if any triggers were found or delimiters/escapes
// conditions was false
func (s State) index(buf, prevSrc []byte, prevEscs int, isEOF bool) (int, NextState) {

	if s.ac == nil {
		return -1, NextState{}
	}

	start := 0
	for {
		m := s.ac.Find(buf, start)
		if m == nil {
			break
		}

		ns := s.NextStates[m.PatternID]

		// Validate delimiters and escapes for this specific match
		if ns.Switch.validate(buf, m.Start, prevSrc, prevEscs, isEOF) {
			return m.Start, ns
		}

		// If not valid, move search forward
		start = m.Start + 1
	}

	return -1, NextState{}
}

func (s State) skipMaxLen() int {

	ll := 0

	for _, ns := range s.NextStates {

		l := len(ns.Switch.Trigger)

		if len(ns.Switch.Delimiters.L) > 0 {
			l++
		}
		if len(ns.Switch.Delimiters.R) > 0 {
			l++
		}

		if l > ll {
			ll = l
		}
	}

	return ll
}
