package fsm_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	fsm "github.com/nixys/nxs-go-fsm"
)

func TestReadmeExample(t *testing.T) {
	const somePgSQLDummyPlainDump = `_Some previous data_

---
--- Columns separated by the space!
---

COPY public.names (id, name) FROM stdin;
12 alice
34 bob
\.

_Some following data_

COPY public.comments (id, comment) FROM stdin;
45 foo
78 bar
\.

_Some other following data_
`
	var (
		stateCopySearch  = fsm.StateName("copy search")
		stateCopyTail    = fsm.StateName("copy tail")
		stateTableValues = fsm.StateName("table values")
	)

	dhTableValueColumn1 := func(usrCtx any, data, trigger []byte) ([]byte, error) {
		return append([]byte("000"), trigger...), nil
	}

	dhTableValueColumn2 := func(usrCtx any, data, trigger []byte) ([]byte, error) {
		return append([]byte("abc"), trigger...), nil
	}

	r := strings.NewReader(somePgSQLDummyPlainDump)

	fsmR := fsm.Init(
		r,
		fsm.Description{
			Ctx:       context.TODO(),
			UserCtx:   nil,
			InitState: stateCopySearch,
			States: map[fsm.StateName]fsm.State{

				stateCopySearch: {
					NextStates: []fsm.NextState{
						{
							Name: stateCopyTail,
							Switch: fsm.Switch{
								Trigger: []byte("COPY"),
								Delimiters: fsm.Delimiters{
									L: []byte{'\n'},
									R: []byte{' '},
								},
							},
							DataHandler: nil,
						},
					},
				},
				stateCopyTail: {
					NextStates: []fsm.NextState{
						{
							Name: stateTableValues,
							Switch: fsm.Switch{
								Trigger: []byte(";\n"),
							},
							DataHandler: nil,
						},
					},
				},
				stateTableValues: {
					NextStates: []fsm.NextState{
						{
							Name: stateCopySearch,
							Switch: fsm.Switch{
								Trigger: []byte("\\."),
								Delimiters: fsm.Delimiters{
									L: []byte{'\n'},
									R: []byte{'\n'},
								},
							},
							DataHandler: nil,
						},
						{
							Name: stateTableValues,
							Switch: fsm.Switch{
								Trigger: []byte{' '},
							},
							DataHandler: dhTableValueColumn1,
						},
						{
							Name: stateTableValues,
							Switch: fsm.Switch{
								Trigger: []byte{'\n'},
							},
							DataHandler: dhTableValueColumn2,
						},
					},
				},
			},
		},
	)

	var out bytes.Buffer
	_, err := io.Copy(&out, fsmR)
	if err != nil {
		t.Fatalf("copy error: %v", err)
	}

	expected := `_Some previous data_

---
--- Columns separated by the space!
---

COPY public.names (id, name) FROM stdin;
000 abc
000 abc
\.

_Some following data_

COPY public.comments (id, comment) FROM stdin;
000 abc
000 abc
\.

_Some other following data_
`
	if out.String() != expected {
		t.Errorf("Expected:\n%s\nGot:\n%s", expected, out.String())
	}
}

func TestEscapes(t *testing.T) {
	var (
		stateInit = fsm.StateName("init")
		stateEnd  = fsm.StateName("end")
	)

	r := strings.NewReader(`foo bar \bar \\bar \\\bar`)

	fsmR := fsm.Init(
		r,
		fsm.Description{
			Ctx:       context.TODO(),
			InitState: stateInit,
			States: map[fsm.StateName]fsm.State{
				stateInit: {
					NextStates: []fsm.NextState{
						{
							Name: stateEnd,
							Switch: fsm.Switch{
								Trigger: []byte("bar"),
								Escape:  true,
							},
							DataHandler: func(usrCtx any, data, trigger []byte) ([]byte, error) {
								return append(data, []byte("REPLACED")...), nil
							},
						},
					},
				},
				stateEnd: {
					NextStates: []fsm.NextState{
						{
							Name: stateEnd,
							Switch: fsm.Switch{
								Trigger: []byte("bar"),
								Escape:  true,
							},
							DataHandler: func(usrCtx any, data, trigger []byte) ([]byte, error) {
								return append(data, []byte("REPLACED")...), nil
							},
						},
					},
				},
			},
		},
	)

	var out bytes.Buffer
	_, err := io.Copy(&out, fsmR)
	if err != nil {
		t.Fatalf("copy error: %v", err)
	}

	expected := `foo REPLACED \bar \\REPLACED \\\bar`
	if out.String() != expected {
		t.Errorf("Expected: %s, Got: %s", expected, out.String())
	}
}

func TestEmptyReader(t *testing.T) {
	r := strings.NewReader("")
	fsmR := fsm.Init(r, fsm.Description{
		Ctx:       context.Background(),
		InitState: "init",
		States: map[fsm.StateName]fsm.State{
			"init": {
				NextStates: []fsm.NextState{
					{
						Name: "end",
						Switch: fsm.Switch{
							Trigger: []byte("foo"),
						},
					},
				},
			},
		},
	})

	out, err := io.ReadAll(fsmR)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out) != 0 {
		t.Errorf("expected empty output, got %q", out)
	}
}

func TestNoMatch(t *testing.T) {
	content := "this is some content without triggers"
	r := strings.NewReader(content)
	fsmR := fsm.Init(r, fsm.Description{
		Ctx:       context.Background(),
		InitState: "init",
		States: map[fsm.StateName]fsm.State{
			"init": {
				NextStates: []fsm.NextState{
					{
						Name: "end",
						Switch: fsm.Switch{
							Trigger: []byte("MISSING"),
						},
					},
				},
			},
		},
	})

	out, err := io.ReadAll(fsmR)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(out) != content {
		t.Errorf("expected %q, got %q", content, string(out))
	}
}

func TestBoundaryTriggers(t *testing.T) {
	// The internal buffer is 4096. Let's try to put a trigger at the boundary.
	trigger := "TRIGGER"
	prefix := strings.Repeat("a", 4095)
	content := prefix + trigger + "after"

	r := strings.NewReader(content)
	fsmR := fsm.Init(r, fsm.Description{
		Ctx:       context.Background(),
		InitState: "init",
		States: map[fsm.StateName]fsm.State{
			"init": {
				NextStates: []fsm.NextState{
					{
						Name: "found",
						Switch: fsm.Switch{
							Trigger: []byte(trigger),
						},
						DataHandler: func(usrCtx any, data, trigger []byte) ([]byte, error) {
							// Return data + REPLACED to keep the prefix
							return append(data, []byte("REPLACED")...), nil
						},
					},
				},
			},
		},
	})

	out, err := io.ReadAll(fsmR)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := prefix + "REPLACED" + "after"
	if string(out) != expected {
		t.Errorf("output mismatch\nExpected length: %d\nGot length: %d", len(expected), len(out))
	}
}

func TestBoundaryDelimiters(t *testing.T) {
	trigger := "COPY"
	// Trigger at boundary with delimiter just after boundary
	prefix := strings.Repeat("a", 4096-len(trigger))
	content := prefix + trigger + " " + "after"

	r := strings.NewReader(content)
	fsmR := fsm.Init(r, fsm.Description{
		Ctx:       context.Background(),
		InitState: "init",
		States: map[fsm.StateName]fsm.State{
			"init": {
				NextStates: []fsm.NextState{
					{
						Name: "found",
						Switch: fsm.Switch{
							Trigger: []byte(trigger),
							Delimiters: fsm.Delimiters{
								R: []byte{' '},
							},
						},
						DataHandler: func(usrCtx any, data, trigger []byte) ([]byte, error) {
							// Trigger is replaced, but delimiter is NOT consumed
							return append(data, []byte("REPLACED")...), nil
						},
					},
				},
			},
		},
	})

	out, err := io.ReadAll(fsmR)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Delimiter is preserved in current implementation
	expected := prefix + "REPLACED" + " " + "after"
	if string(out) != expected {
		t.Errorf("output mismatch\nExpected length: %d\nGot length: %d", len(expected), len(out))
	}
}

func TestContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	// A reader that never ends
	r := &infiniteReader{}

	fsmR := fsm.Init(r, fsm.Description{
		Ctx:       ctx,
		InitState: "init",
		States: map[fsm.StateName]fsm.State{
			"init": {
				NextStates: []fsm.NextState{
					{
						Name: "init",
						Switch: fsm.Switch{
							Trigger: []byte("foo"),
						},
					},
				},
			},
		},
	})

	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	_, err := io.ReadAll(fsmR)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

type infiniteReader struct{}

func (r *infiniteReader) Read(p []byte) (n int, err error) {
	for i := range p {
		p[i] = 'a'
	}
	return len(p), nil
}

func TestDataHandlerError(t *testing.T) {
	errBoom := errors.New("boom")
	r := strings.NewReader("some data trigger some more")
	fsmR := fsm.Init(r, fsm.Description{
		Ctx:       context.Background(),
		InitState: "init",
		States: map[fsm.StateName]fsm.State{
			"init": {
				NextStates: []fsm.NextState{
					{
						Name: "end",
						Switch: fsm.Switch{
							Trigger: []byte("trigger"),
						},
						DataHandler: func(usrCtx any, data, trigger []byte) ([]byte, error) {
							return nil, errBoom
						},
					},
				},
			},
		},
	})

	_, err := io.ReadAll(fsmR)
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Errorf("expected error containing 'boom', got %v", err)
	}
}

func TestMultipleTriggers(t *testing.T) {
	// Should pick the earliest trigger
	r := strings.NewReader("abc")
	fsmR := fsm.Init(r, fsm.Description{
		Ctx:       context.Background(),
		InitState: "init",
		States: map[fsm.StateName]fsm.State{
			"init": {
				NextStates: []fsm.NextState{
					{
						Name: "b",
						Switch: fsm.Switch{
							Trigger: []byte("b"),
						},
						DataHandler: func(usrCtx any, data, trigger []byte) ([]byte, error) {
							return []byte("B"), nil
						},
					},
					{
						Name: "a",
						Switch: fsm.Switch{
							Trigger: []byte("a"),
						},
						DataHandler: func(usrCtx any, data, trigger []byte) ([]byte, error) {
							return []byte("A"), nil
						},
					},
				},
			},
		},
	})

	out, err := io.ReadAll(fsmR)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(out) != "Abc" { // "a" should be found first, then it switches state (where no more triggers)
		t.Errorf("expected 'Abc', got %q", string(out))
	}
}

func TestOverlappingTriggers(t *testing.T) {
	// Should pick the one that starts earlier
	r := strings.NewReader("foobar")
	fsmR := fsm.Init(r, fsm.Description{
		Ctx:       context.Background(),
		InitState: "init",
		States: map[fsm.StateName]fsm.State{
			"init": {
				NextStates: []fsm.NextState{
					{
						Name: "bar",
						Switch: fsm.Switch{
							Trigger: []byte("bar"),
						},
						DataHandler: func(usrCtx any, data, trigger []byte) ([]byte, error) {
							return []byte("BAR"), nil
						},
					},
					{
						Name: "foo",
						Switch: fsm.Switch{
							Trigger: []byte("foo"),
						},
						DataHandler: func(usrCtx any, data, trigger []byte) ([]byte, error) {
							return []byte("FOO"), nil
						},
					},
				},
			},
		},
	})

	out, err := io.ReadAll(fsmR)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(out) != "FOObar" {
		t.Errorf("expected 'FOObar', got %q", string(out))
	}
}

func TestEscapesBoundary(t *testing.T) {
	// Escape symbol at the end of buffer
	trigger := "bar"
	prefix := strings.Repeat("a", 4096-1) // last byte is \
	content := prefix + "\\" + trigger

	r := strings.NewReader(content)
	fsmR := fsm.Init(r, fsm.Description{
		Ctx:       context.Background(),
		InitState: "init",
		States: map[fsm.StateName]fsm.State{
			"init": {
				NextStates: []fsm.NextState{
					{
						Name: "found",
						Switch: fsm.Switch{
							Trigger: []byte(trigger),
							Escape:  true,
						},
						DataHandler: func(usrCtx any, data, trigger []byte) ([]byte, error) {
							return []byte("REPLACED"), nil
						},
					},
				},
			},
		},
	})

	out, err := io.ReadAll(fsmR)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// It should NOT be replaced because it's escaped
	expected := content
	if string(out) != expected {
		t.Errorf("output mismatch: expected it to be escaped and NOT replaced")
	}
}

func BenchmarkFSM(b *testing.B) {
	// Create a larger input for benchmarking
	var sb strings.Builder
	for range 1000 {
		sb.WriteString("COPY public.names (id, name) FROM stdin;\n")
		for range 100 {
			sb.WriteString("12 alice\n")
		}
		sb.WriteString("\\.\n")
	}
	input := sb.String()

	var (
		stateCopySearch  = fsm.StateName("copy search")
		stateCopyTail    = fsm.StateName("copy tail")
		stateTableValues = fsm.StateName("table values")
	)

	dhTableValueColumn1 := func(usrCtx any, data, trigger []byte) ([]byte, error) {
		return append([]byte("000"), trigger...), nil
	}

	dhTableValueColumn2 := func(usrCtx any, data, trigger []byte) ([]byte, error) {
		return append([]byte("abc"), trigger...), nil
	}

	for b.Loop() {
		r := strings.NewReader(input)
		fsmR := fsm.Init(
			r,
			fsm.Description{
				Ctx:       context.TODO(),
				InitState: stateCopySearch,
				States: map[fsm.StateName]fsm.State{
					stateCopySearch: {
						NextStates: []fsm.NextState{
							{
								Name: stateCopyTail,
								Switch: fsm.Switch{
									Trigger: []byte("COPY"),
									Delimiters: fsm.Delimiters{
										L: []byte{'\n'},
										R: []byte{' '},
									},
								},
							},
						},
					},
					stateCopyTail: {
						NextStates: []fsm.NextState{
							{
								Name: stateTableValues,
								Switch: fsm.Switch{
									Trigger: []byte(";\n"),
								},
							},
						},
					},
					stateTableValues: {
						NextStates: []fsm.NextState{
							{
								Name: stateCopySearch,
								Switch: fsm.Switch{
									Trigger: []byte("\\."),
									Delimiters: fsm.Delimiters{
										L: []byte{'\n'},
										R: []byte{'\n'},
									},
								},
							},
							{
								Name: stateTableValues,
								Switch: fsm.Switch{
									Trigger: []byte{' '},
								},
								DataHandler: dhTableValueColumn1,
							},
							{
								Name: stateTableValues,
								Switch: fsm.Switch{
									Trigger: []byte{'\n'},
								},
								DataHandler: dhTableValueColumn2,
							},
						},
					},
				},
			},
		)
		if _, err := io.Copy(io.Discard, fsmR); err != nil {
			b.Fatalf("benchmark copy error: %v", err)
		}
	}
}
