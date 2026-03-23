package fsm

import (
	"bytes"
	"context"
	"fmt"
	"io"
)

type FSM struct {
	ctx context.Context

	curState StateName
	states   map[StateName]State
	userCtx  any

	// Buffer for save data up to token to switch to next state
	deferredBuf bytes.Buffer

	// Buffer contains data to be send
	dst bytes.Buffer

	src        []byte
	src0, src1 int

	prevSrc  []byte
	prevSrcL int

	// Count of continuous escape bytes at the end of previous buffer
	prevEscs int

	isEOF bool

	r io.Reader
}

type Description struct {
	Ctx       context.Context
	UserCtx   any
	States    map[StateName]State
	InitState StateName
}

func Init(r io.Reader, desc Description) io.Reader {

	for k, v := range desc.States {
		v.compile()
		desc.States[k] = v
	}

	return &FSM{

		ctx: desc.Ctx,

		curState: desc.InitState,
		states:   desc.States,
		userCtx:  desc.UserCtx,

		src: make([]byte, 4096),

		// At the moment we need to store only
		// one last `src` byte to processing
		// token delimiters.
		// We may to increase amount of stored data
		// at any moment we need
		prevSrc: make([]byte, 1),

		r: r,
	}
}

func (fsm *FSM) Read(dst []byte) (int, error) {

	// Check context before reading
	select {
	case <-fsm.ctx.Done():
		return 0, fsm.ctx.Err()
	default:
	}

	return fsm.read(dst)
}

func (fsm *FSM) read(dst []byte) (int, error) {
	for {

		state := fsm.states[fsm.curState]

		// Flush dst buf
		if fsm.dst.Len() > 0 {
			n, _ := fsm.dst.Read(dst)
			return n, nil
		}

		if fsm.src1-fsm.src0 > 0 {

			// Prepare the buffer contains previous source data
			var prevSrc []byte
			if fsm.src0 == 0 {
				prevSrc = fsm.prevSrc[:fsm.prevSrcL]
			} else {
				prevSrc = fsm.src[:fsm.src0]
			}

			start := 0
			buf := fsm.src[fsm.src0:fsm.src1]
			for {
				i, ns := state.indexFrom(buf, start, prevSrc, fsm.prevEscs, fsm.isEOF)
				if i >= 0 {

					// copy everything up to the match
					if _, err := fsm.writeOutBuf(buf[start:i]); err != nil {
						return 0, err
					}

					if ns.DataHandler == nil {

						fsm.dst.Write(fsm.deferredBuf.Bytes())
						fsm.deferredBuf.Reset()

						fsm.dst.Write(ns.Switch.Trigger)
					} else {

						d, err := ns.DataHandler(fsm.userCtx, fsm.deferredBuf.Bytes(), ns.Switch.Trigger)
						if err != nil {
							return 0, fmt.Errorf("fsm read data handler: %w", err)
						}
						fsm.deferredBuf.Reset()

						fsm.dst.Write(d)
					}

					// Update fsm state and positions
					fsm.src0 += i - start + len(ns.Switch.Trigger)
					start = i + len(ns.Switch.Trigger)

					oldState := fsm.curState
					fsm.stateSwitch(ns.Name)

					// If state changed, we must restart search from current src0
					if fsm.curState != oldState {
						break
					}

					// If we have something in dst, return it
					if fsm.dst.Len() > 0 {
						n, _ := fsm.dst.Read(dst)
						return n, nil
					}

				} else {

					// If specified src sequence has no more triggers.
					//
					// Do this becasue there could be a match straddling
					// the boundary

					// If trigger for current state not found and it is last
					// source buffer (EOF), we not to get new data. So we may
					// skip rest of data in source buffer
					skip := len(buf) - start
					if ml := state.skipMaxLen(); !fsm.isEOF && ml > 0 {
						skip = skip - ml + 1
					}

					if skip > 0 {
						n, err := fsm.writeOutBuf(buf[start : start+skip])
						if err != nil {
							return 0, err
						}
						fsm.src0 += n
					}
					break // exit inner loop to read more data
				}
			}
		}

		// Copy left bytes in source buffer to begining of buffer
		if fsm.src0 > 0 {

			// Get count of continuous escape bytes at the end of previous buffer
			fsm.prevEscs = escapesCount(fsm.src[:fsm.src0])

			ls := fsm.src0
			lm := len(fsm.prevSrc)

			if ls > lm {
				// If `src` buffer has more size than `fsm.prevSrc`,
				// store in `fsm.prevSrc` last `lm` bytes from `src`
				fsm.prevSrcL = copy(fsm.prevSrc, fsm.src[fsm.src0-lm:fsm.src0])
			} else {

				// Available space size in `fsm.prevSrc` to store old
				// `fsm.prevSrc` data after `src`` will be saved in `fsm.prevSrc`
				lr := lm - ls

				if fsm.prevSrcL > lr {
					// Store in `fsm.prevSrc` only last `lr` of old data
					// if current `fsm.prevSrcL` size more than available
					// amount of bytes after the `src` is saved in `fsm.prevSrcL`
					fsm.prevSrcL = copy(fsm.prevSrc, fsm.prevSrc[fsm.prevSrcL-lr:fsm.prevSrcL])
				}

				// Store in `fsm.prevSrc` new data from `src` buffer
				fsm.prevSrcL += copy(fsm.prevSrc[fsm.prevSrcL:], fsm.src[:fsm.src0])
			}

			fsm.src0, fsm.src1 = 0, copy(fsm.src, fsm.src[fsm.src0:fsm.src1])
		}

		n, err := fsm.r.Read(fsm.src[fsm.src1:])
		if err != nil {
			switch err {
			case io.EOF:

				fsm.isEOF = true

				if fsm.src1-fsm.src0 == 0 {
					fsm.dst.Write(fsm.deferredBuf.Bytes())
					fsm.deferredBuf.Reset()
				}

				if fsm.dst.Len() == 0 && fsm.src1-fsm.src0 == 0 {
					return 0, io.EOF
				}
			default:
				return 0, err
			}
		}
		fsm.src1 += n
	}
}

func (fsm *FSM) stateSwitch(newState StateName) {
	fsm.curState = newState
}

// writeOutBuf writes specified data either to deferred buf or to dst buf
func (fsm *FSM) writeOutBuf(d []byte) (int, error) {

	cs := fsm.states[fsm.curState]

	for _, s := range cs.NextStates {
		if s.DataHandler != nil {

			// Save source data to deferred buffer if at least one
			// of the next states has handler to processing data
			return fsm.deferredBuf.Write(d)
		}
	}

	return fsm.dst.Write(d)
}

func escapesCount(b []byte) int {

	l := len(b)
	if l == 0 {
		return 0
	}

	var pe int

	for i := l - 1; i >= 0; i-- {

		if b[i] == '\\' {
			pe++
		} else {
			break
		}
	}

	return pe
}

// DataHandlerGenericVoid represents built-in
// function for skip data (deferred and token) has been read
func DataHandlerGenericVoid(usrCtx any, deferred, token []byte) ([]byte, error) {
	return []byte{}, nil
}

// DataHandlerGenericSkipToken represents built-in
// function for skip `token` data has been read
func DataHandlerGenericSkipToken(usrCtx any, deferred, token []byte) ([]byte, error) {
	return deferred, nil
}

// DataHandlerGenericSkipDeferred represents built-in
// function for skip `deferred` data has been read
func DataHandlerGenericSkipDeferred(usrCtx any, deferred, token []byte) ([]byte, error) {
	return deferred, nil
}
