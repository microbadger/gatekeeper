package shared

type Protocol uint

const (
	HTTPPublic Protocol = iota + 1
	HTTPInternal
)
