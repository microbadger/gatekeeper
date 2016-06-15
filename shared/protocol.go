package shared

import (
	"fmt"
	"log"
)

type Protocol uint

const (
	HTTPPublic Protocol = iota + 1
	HTTPInternal
)

var formattedProtocols = map[Protocol]string{
	HTTPPublic:   "http-public",
	HTTPInternal: "http-internal",
}

func (p Protocol) String() string {
	str, ok := formattedProtocols[p]
	if !ok {
		log.Fatal("programming error; unformatted protocol")
	}
	return str
}

func NewProtocol(value string) (Protocol, error) {
	for protocol, str := range formattedProtocols {
		if str == value {
			return protocol, nil
		}
	}

	return Protocol(0), fmt.Errorf("unknown protocol")
}
