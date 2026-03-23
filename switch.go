package fsm

type Switch struct {
	Trigger    []byte
	Delimiters Delimiters
	Escape     bool
}

type Delimiters struct {
	L []byte
	R []byte
}
