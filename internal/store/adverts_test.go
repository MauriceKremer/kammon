package store

import (
	"testing"
	"time"

	"kammon.bytecoder.nl/internal/kam"
)

func TestSetAdvertsCapsAndReportsTruncation(t *testing.T) {
	s := New()
	now := time.Now()
	list := make([]kam.Advert, MaxAdvertNodes+2)
	for i := range list {
		list[i] = kam.Advert{PublicKey: "key", NodeHash: "8e"}
	}
	s.SetAdverts(list, true, now)

	got, truncated, dropped := s.Adverts()
	if len(got) != MaxAdvertNodes {
		t.Fatalf("kept %d adverts, want %d", len(got), MaxAdvertNodes)
	}
	if !truncated {
		t.Fatal("KAM's truncated flag was lost")
	}
	if dropped != 2 {
		t.Fatalf("dropped = %d, want 2", dropped)
	}
}

func TestAdvertsAreCopiedAndHaveTheirOwnFreshness(t *testing.T) {
	s := New()
	now := time.Now()
	s.SetAdverts([]kam.Advert{{PublicKey: "one", Nickname: nil}}, false, now)

	got, _, _ := s.Adverts()
	got[0].PublicKey = "changed"
	again, _, _ := s.Adverts()
	if again[0].PublicKey != "one" {
		t.Fatalf("the store handed out its own slice: %q", again[0].PublicKey)
	}

	if freshness, _ := s.SnapshotFreshness("adverts", now.Add(time.Second)); freshness != Fresh {
		t.Fatalf("fresh advert snapshot = %v", freshness)
	}
	if freshness, _ := s.SnapshotFreshness("adverts", now.Add(time.Hour)); freshness != Down {
		t.Fatalf("old advert snapshot = %v", freshness)
	}
	if freshness, _ := s.SnapshotFreshness("channels", now); freshness != Down {
		t.Fatalf("a missing snapshot must stay down, got %v", freshness)
	}
}
