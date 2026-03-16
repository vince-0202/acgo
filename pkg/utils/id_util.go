package utils

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"acgo/pkg/keys"
)

// IdGenerator generates unique IDs as strings; the format depends on the implementation.
type IdGenerator interface {
	NextID() string
}

// uuidGenerator implements IdGenerator using UUID v4.
type uuidGenerator struct{}

func (uuidGenerator) NextID() string {
	s, err := UUID4()
	if err != nil {
		panic("uuid4: " + err.Error())
	}
	return s
}

// snowflakeIdGenerator implements IdGenerator using snowflake IDs (decimal string).
type snowflakeIdGenerator struct {
	*SnowflakeGenerator
}

func (s snowflakeIdGenerator) NextID() string {
	return s.NextString()
}

var (
	uuidGen      IdGenerator = uuidGenerator{}
	snowflakeGen IdGenerator = snowflakeIdGenerator{defaultSnowflake}
)

// GeneratorFor returns the IdGenerator for the given kind.
func GeneratorFor(kind keys.IdKind) IdGenerator {
	switch kind {
	case keys.IdKindUUID:
		return uuidGen
	case keys.IdKindSnowflake:
		return snowflakeGen
	default:
		return snowflakeGen
	}
}

// NextID generates the next ID for the given kind (uuid or snowflake).
func NextID(kind keys.IdKind) string {
	return GeneratorFor(kind).NextID()
}

// UUID4 returns a new UUID v4 string (e.g. "550e8400-e29b-41d4-a716-446655440000").
func UUID4() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant
	return fmt.Sprintf("%s-%s-%s-%s-%s",
		hex.EncodeToString(b[0:4]),
		hex.EncodeToString(b[4:6]),
		hex.EncodeToString(b[6:8]),
		hex.EncodeToString(b[8:10]),
		hex.EncodeToString(b[10:16])), nil
}

// MustUUID4 is like UUID4 but panics on error (for convenience in tests or init).
func MustUUID4() string {
	s, err := UUID4()
	if err != nil {
		panic(err)
	}
	return s
}

// SnowflakeGenerator generates unique 64-bit IDs (snowflake-like).
// Layout: 1 bit unused (0) + 41 bits ms timestamp + 10 bits machine ID + 12 bits sequence.
// Epoch is 2020-01-01 00:00:00 UTC.
var snowflakeEpoch = time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC).UnixMilli()

// SnowflakeGenerator is safe for concurrent use.
type SnowflakeGenerator struct {
	mu        sync.Mutex
	machineID int64
	sequence  int64
	lastMs    int64
}

// NewSnowflakeGenerator creates a generator. machineID should be 0..1023 (10 bits).
func NewSnowflakeGenerator(machineID int64) *SnowflakeGenerator {
	if machineID < 0 || machineID > 0x3ff {
		machineID = machineID & 0x3ff
	}
	return &SnowflakeGenerator{machineID: machineID}
}

// Next returns the next snowflake ID (int64).
func (s *SnowflakeGenerator) Next() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	ms := time.Now().UTC().UnixMilli()
	if ms == s.lastMs {
		s.sequence++
		if s.sequence > 0xfff {
			for ms <= s.lastMs {
				ms = time.Now().UTC().UnixMilli()
			}
			s.sequence = 0
		}
	} else {
		s.sequence = 0
	}
	s.lastMs = ms
	ts := ms - snowflakeEpoch
	if ts < 0 {
		ts = 0
	}
	return (ts << 22) | (s.machineID << 12) | s.sequence
}

// NextString returns the next snowflake ID as a decimal string.
func (s *SnowflakeGenerator) NextString() string {
	return fmt.Sprintf("%d", s.Next())
}

// default snowflake generator (machine ID 0)
var defaultSnowflake = NewSnowflakeGenerator(0)

// SnowflakeID returns the next ID from the default generator (machine ID 0).
func SnowflakeID() int64 {
	return defaultSnowflake.Next()
}

// SnowflakeIDString returns the next snowflake ID as a string from the default generator.
func SnowflakeIDString() string {
	return defaultSnowflake.NextString()
}
