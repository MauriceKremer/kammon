// Package store is KAMmon's in-memory history. The poller is the only writer; pages read copies.
// Every buffer has a hard limit (MON-DS-18). Nothing is written to disk.
package store

import "kammon.bytecoder.nl/internal/kam"

// Class groups packets for the airtime chart and the packet mix (docs/requirements/Overview.md §3.1).
type Class int

const (
	ClassGroupText Class = iota
	ClassAdvert
	ClassRoom
	ClassOther
	ClassCount
)

func (c Class) String() string {
	return [...]string{"Group text", "Adverts", "Room traffic", "Other"}[c]
}

// Key is a short lower-case name for CSS classes.
func (c Class) Key() string {
	return [...]string{"group", "advert", "room", "other"}[c]
}

var peerTypes = map[string]bool{
	"REQ": true, "RESPONSE": true, "TXT_MSG": true, "ACK": true,
	"ANON_REQ": true, "PATH": true, "MULTIPART": true,
}

// Classify maps an rx or tx event to a packet class.
func Classify(ev kam.Event) Class {
	if ev.Kind == kam.KindTX && ev.Origin == "room" {
		return ClassRoom
	}
	switch ev.PayloadType {
	case "GRP_TXT", "GRP_DATA":
		return ClassGroupText
	case "ADVERT":
		return ClassAdvert
	}
	if ev.Kind == kam.KindRX && peerTypes[ev.PayloadType] && ev.Room != "" {
		return ClassRoom
	}
	return ClassOther
}
