// Package tracksession keeps the per-file track choices from outgrowing the
// session.
//
// Every audio/subtitle pick is stored as "<resource>_<item>_audio_id" and
// "<resource>_<item>_subtitle_id" in the session, and nothing ever removed
// them: the session lives 30 days and every save renews it. redistore refuses
// a gob over 4096 bytes, so a viewer who had picked tracks on ~17 files got
// "SessionStore: the value to store is too big" on every further pick (312
// failed PUTs on 2026-10-07), and so did any other write to that session.
//
// The bound sits in the serializer because every Save goes through it: the
// guest "resources" list and the CSRF secret share the same 4096 bytes, and a
// cap applied only where tracks are written would not protect those writes.
package tracksession

import (
	"bytes"
	"encoding/gob"
	"fmt"
	"sort"
	"strings"

	"github.com/gin-contrib/sessions/redis"
	"github.com/gorilla/sessions"
	"github.com/snowdreamtech/redistore"
)

// OrderKey lists the files with a stored choice, least recently picked first.
const OrderKey = "track_choice_order_v1"

const (
	// MaxItems is how many files keep their choice.
	MaxItems = 8
	// Budget is where eviction stops: the headroom below HardLimit leaves
	// room for the next resources append without a save failing.
	Budget = 3584
	// HardLimit is redistore's default MaxLength.
	HardLimit = 4096
)

// Install puts the bounded serializer on a redis session store.
func Install(store redis.Store) error {
	err, rs := redis.GetRedisStore(store)
	if err != nil {
		return err
	}
	rs.SetSerializer(Serializer{})
	return nil
}

// Touch moves the file to the end of the order list, the value UpdateSessionData
// stores under OrderKey.
func Touch(old any, current string) []string {
	out := []string{}
	if a, ok := old.([]string); ok {
		for _, p := range a {
			if p != current {
				out = append(out, p)
			}
		}
	}
	out = append(out, current)
	if len(out) > MaxItems {
		out = out[len(out)-MaxItems:]
	}
	return out
}

// Serializer is redistore's gob serializer that evicts the oldest track
// choices until the session fits.
type Serializer struct{ redistore.GobSerializer }

func (Serializer) Serialize(s *sessions.Session) ([]byte, error) {
	// Work on a copy: a refused write must not leave the session half-evicted.
	m := map[any]any{}
	groups := map[string]bool{}
	for k, v := range s.Values {
		m[k] = v
		if p := prefix(k); p != "" {
			groups[p] = true
		}
	}
	indexed := []string{}
	known := map[string]bool{}
	if a, ok := m[OrderKey].([]string); ok {
		for _, p := range a {
			if groups[p] && !known[p] {
				indexed = append(indexed, p)
				known[p] = true
			}
		}
	}
	// Choices saved before OrderKey existed have no age; they go first, in a
	// fixed order so that eviction is deterministic.
	legacy := []string{}
	for p := range groups {
		if !known[p] {
			legacy = append(legacy, p)
		}
	}
	sort.Strings(legacy)
	order := append(legacy, indexed...)
	// The file being picked right now is never evicted.
	pinned := ""
	if len(indexed) > 0 {
		pinned = indexed[len(indexed)-1]
	}
	remove := func() {
		p := order[0]
		order = order[1:]
		delete(m, p+"_audio_id")
		delete(m, p+"_subtitle_id")
	}
	writeOrder := func() {
		a := []string{}
		for _, p := range order {
			if known[p] {
				a = append(a, p)
			}
		}
		if len(a) == 0 {
			delete(m, OrderKey)
		} else {
			m[OrderKey] = a
		}
	}
	for len(order) > MaxItems {
		remove()
	}
	for {
		writeOrder()
		b, err := encode(m)
		if err != nil {
			return nil, err
		}
		if len(b) <= Budget || len(order) == 0 || (len(order) == 1 && order[0] == pinned) {
			if len(b) > HardLimit {
				return nil, fmt.Errorf("bounded session: values that cannot be evicted exceed %d bytes", HardLimit)
			}
			s.Values = m
			return b, nil
		}
		remove()
	}
}

// prefix returns "<resource>_<item>" for a track choice key, "" otherwise.
func prefix(key any) string {
	s, ok := key.(string)
	if !ok {
		return ""
	}
	for _, suffix := range []string{"_audio_id", "_subtitle_id"} {
		if p, found := strings.CutSuffix(s, suffix); found {
			return p
		}
	}
	return ""
}

func encode(values map[any]any) ([]byte, error) {
	var b bytes.Buffer
	err := gob.NewEncoder(&b).Encode(values)
	return b.Bytes(), err
}
