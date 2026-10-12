// Package kam holds the types and HTTP reader for KAM's local monitor API.
// The contract is docs/kam-api/FeatureMonitorApi.md. KAM code is never imported.
package kam

import "time"

// Status is GET /v1/status (API-ST-1).
type Status struct {
	APIVersion int       `json:"api_version"`
	Epoch      string    `json:"epoch"`
	Now        time.Time `json:"now"`
	StartedAt  time.Time `json:"started_at"`
	NodeName   string    `json:"node_name"`
	Modem      struct {
		Link            string `json:"link"`
		DeviceName      string `json:"device_name"`
		FirmwareVersion int    `json:"firmware_version"`
	} `json:"modem"`
	Radio     Radio `json:"radio"`
	DutyCycle struct {
		ConfiguredPercent float64 `json:"configured_percent"`
		UsedMS            int64   `json:"used_ms"`
		WindowS           int     `json:"window_s"`
	} `json:"duty_cycle"`
	TX struct {
		Queued       int    `json:"queued"`
		Submitted    uint64 `json:"submitted"`
		Sent         uint64 `json:"sent"`
		Completed    uint64 `json:"completed"`
		Rejected     uint64 `json:"rejected"`
		Evicted      uint64 `json:"evicted"`
		Deferred     uint64 `json:"deferred"`
		GateTimeouts uint64 `json:"gate_timeouts"`
	} `json:"tx"`
	Persistence map[string]struct {
		Depth   int    `json:"depth"`
		Dropped uint64 `json:"dropped"`
	} `json:"persistence"`
	Rooms struct {
		Enabled bool `json:"enabled"`
		Count   int  `json:"count"`
	} `json:"rooms"`
	Automation struct {
		Enabled        bool   `json:"enabled"`
		ScriptsEnabled int    `json:"scripts_enabled"`
		QueueDepth     int    `json:"queue_depth"`
		EventsDropped  uint64 `json:"events_dropped"`
	} `json:"automation"`
	EventRing struct {
		Capacity int    `json:"capacity"`
		FirstSeq uint64 `json:"first_seq"`
		LastSeq  uint64 `json:"last_seq"`
	} `json:"event_ring"`
}

// Radio is the radio line in the status. Source is always "modem_echo": a stored copy, not a chip reading.
type Radio struct {
	FrequencyHz     uint32 `json:"frequency_hz"`
	BandwidthHz     uint32 `json:"bandwidth_hz"`
	SpreadingFactor uint8  `json:"spreading_factor"`
	CodingRate      uint8  `json:"coding_rate"`
	TxPowerDBm      *uint8 `json:"tx_power_dbm"`
	Source          string `json:"source"`
}

// Event kinds.
const (
	KindRX     = "rx"
	KindTX     = "tx"
	KindRoom   = "room"
	KindScript = "script"
	KindModem  = "modem"
)

// Event is one record of KAM's event ring. Fields that do not apply to a kind stay zero. It never holds payload text.
type Event struct {
	Seq  uint64    `json:"seq"`
	At   time.Time `json:"at"`
	Kind string    `json:"kind"`

	PayloadType string   `json:"payload_type,omitempty"`
	Route       string   `json:"route,omitempty"`
	Hops        int      `json:"hops,omitempty"`
	Len         int      `json:"len,omitempty"`
	SNRdB       *float64 `json:"snr_db,omitempty"`
	RSSIdBm     *int     `json:"rssi_dbm,omitempty"`
	AirtimeMS   *int     `json:"airtime_ms,omitempty"`
	Duplicate   bool     `json:"duplicate,omitempty"`
	DestHash    string   `json:"dest_hash,omitempty"`
	Room        string   `json:"room,omitempty"`
	Channel     string   `json:"channel,omitempty"`
	Name        string   `json:"name,omitempty"`
	AdvertRole  string   `json:"advert_role,omitempty"`
	OK          *bool    `json:"ok,omitempty"`
	Origin      string   `json:"origin,omitempty"`

	Event  string `json:"event,omitempty"`
	Client string `json:"client,omitempty"`
	Role   string `json:"role,omitempty"`

	Script     string `json:"script,omitempty"`
	DurationMS int    `json:"duration_ms,omitempty"`
	Replies    int    `json:"replies,omitempty"`
	Error      string `json:"error,omitempty"`
}

// EventsResponse is GET /v1/events (API-EV-2).
type EventsResponse struct {
	Epoch    string `json:"epoch"`
	FirstSeq uint64 `json:"first_seq"`
	LastSeq  uint64 `json:"last_seq"`
	// NextAfter is the largest sequence number served in this page: the poller continues with
	// after=next_after (contract D14, recorded KAM response). A caught-up page carries the ring
	// head, so the next poll still starts after it.
	NextAfter uint64  `json:"next_after"`
	Reset     bool    `json:"reset"`
	Events    []Event `json:"events"`
}

// Channel is one entry of GET /v1/channels (API-CH-1). Never carries a key.
type Channel struct {
	Name           string     `json:"name"`
	KeyStrategy    string     `json:"key_strategy"`
	Enabled        bool       `json:"enabled"`
	StoredMessages int        `json:"stored_messages"`
	LastMessageAt  *time.Time `json:"last_message_at"`
}

// Room is one entry of GET /v1/rooms (API-RM-1).
type Room struct {
	ID                string            `json:"id"`
	Name              string            `json:"name"`
	Enabled           bool              `json:"enabled"`
	PublicKey         string            `json:"public_key"`
	MaxClients        int               `json:"max_clients"`
	ClientsRemembered int               `json:"clients_remembered"`
	LastActivityAt    *time.Time        `json:"last_activity_at"`
	LastPostAt        *time.Time        `json:"last_post_at"`
	RetainedPosts     int               `json:"retained_posts"`
	Counters          map[string]uint64 `json:"counters"`
}

// RoomClient is one entry of GET /v1/rooms/{id}/clients (API-RM-2).
type RoomClient struct {
	PublicKey      string     `json:"public_key"`
	Role           string     `json:"role"`
	Pinned         bool       `json:"pinned"`
	LastActivityAt *time.Time `json:"last_activity_at"`
	PushState      string     `json:"push_state"`
	Label          *string    `json:"label"`
}
