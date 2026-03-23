package fsm

import (
	"slices"
)

type Switch struct {
	Trigger    []byte
	Delimiters Delimiters
	Escape     bool
}

type Delimiters struct {
	L []byte
	R []byte
}

func (s Switch) validate(buf []byte, i int, prevSrc []byte, prevEscs int, isEOF bool) bool {

	if len(s.Delimiters.L) > 0 {

		if b := func() bool {

			var c byte

			if i == 0 {

				if len(prevSrc) == 0 {
					return true
				}

				c = prevSrc[len(prevSrc)-1]
			} else {
				c = buf[i-1]
			}

			return slices.Contains(s.Delimiters.L, c)
		}(); !b {
			return false
		}
	}

	if len(s.Delimiters.R) > 0 {

		if b := func() bool {

			if i+len(s.Trigger) == len(buf) {
				if isEOF {
					return true
				} else {
					return false
				}
			}

			c := buf[i+len(s.Trigger)]
			return slices.Contains(s.Delimiters.R, c)
		}(); !b {
			return false
		}
	}

	if s.Escape {

		// Check if found sequence is escaped (has a leading symbol from escape set)
		// True when it has
		if b := func() bool {

			ec := escapesCount(buf[:i])
			if len(buf[:i])-ec == 0 {
				// Escapes is from previous position till beginning of buffer
				ec += prevEscs
			}

			if ec%2 == 0 {
				return false
			}
			return true
		}(); b {
			return false
		}
	}

	return true
}
