package fsm

import (
	"bytes"

	"github.com/coregx/ahocorasick"
)

type State struct {
	NextStates []NextState
	ac         *ahocorasick.Automaton
	lMasks     [][256]bool
	rMasks     [][256]bool
	maxLen     int
	single     bool
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

	s.lMasks = make([][256]bool, len(s.NextStates))
	s.rMasks = make([][256]bool, len(s.NextStates))
	s.maxLen = 0
	s.single = len(s.NextStates) == 1

	builder := ahocorasick.NewBuilder()
	for i, ns := range s.NextStates {
		builder.AddPattern(ns.Switch.Trigger)

		for _, b := range ns.Switch.Delimiters.L {
			s.lMasks[i][b] = true
		}
		for _, b := range ns.Switch.Delimiters.R {
			s.rMasks[i][b] = true
		}

		l := len(ns.Switch.Trigger)
		if len(ns.Switch.Delimiters.L) > 0 {
			l++
		}
		if len(ns.Switch.Delimiters.R) > 0 {
			l++
		}
		if l > s.maxLen {
			s.maxLen = l
		}
	}
	s.ac, _ = builder.Build()
}

func (s State) indexFrom(buf []byte, start int, prevSrc []byte, prevEscs int, isEOF bool) (int, NextState) {

	if s.ac == nil {
		return -1, NextState{}
	}

	if s.single {
		ns := s.NextStates[0]
		trigger := ns.Switch.Trigger
		for {
			offset := bytes.Index(buf[start:], trigger)
			if offset < 0 {
				return -1, NextState{}
			}
			pos := start + offset
			if s.validate(0, buf, pos, prevSrc, prevEscs, isEOF) {
				return pos, ns
			}
			start = pos + 1
		}
	}

	for {
		m := s.ac.Find(buf, start)
		if m == nil {
			break
		}

		ns := s.NextStates[m.PatternID]

		// Validate delimiters and escapes for this specific match
		if s.validate(m.PatternID, buf, m.Start, prevSrc, prevEscs, isEOF) {
			return m.Start, ns
		}

		// If not valid, move search forward
		start = m.Start + 1
	}

	return -1, NextState{}
}

func (s State) validate(patternID int, buf []byte, i int, prevSrc []byte, prevEscs int, isEOF bool) bool {

	ns := s.NextStates[patternID]

	if len(ns.Switch.Delimiters.L) > 0 {

		var c byte
		if i == 0 {
			if len(prevSrc) == 0 {
				goto skipL
			}
			c = prevSrc[len(prevSrc)-1]
		} else {
			c = buf[i-1]
		}

		if !s.lMasks[patternID][c] {
			return false
		}
	}

skipL:
	if len(ns.Switch.Delimiters.R) > 0 {

		if i+len(ns.Switch.Trigger) == len(buf) {
			if !isEOF {
				return false
			}
			goto skipR
		}

		c := buf[i+len(ns.Switch.Trigger)]
		if !s.rMasks[patternID][c] {
			return false
		}
	}

skipR:
	if ns.Switch.Escape {

		// Check if found sequence is escaped (has a leading symbol from escape set)
		// True when it has
		ec := escapesCount(buf[:i])
		if len(buf[:i])-ec == 0 {
			// Escapes is from previous position till beginning of buffer
			ec += prevEscs
		}

		if ec%2 != 0 {
			return false
		}
	}

	return true
}

func (s State) skipMaxLen() int {
	return s.maxLen
}
