package utils

import (
	"testing"

	"github.com/vince-0202/acgo/pkg/keys"
)

func TestUUID4(t *testing.T) {
	s, err := UUID4()
	if err != nil {
		t.Fatalf("UUID4: %v", err)
	}
	if len(s) != 36 {
		t.Errorf("UUID4 length = %d, want 36", len(s))
	}
	// format 8-4-4-4-12
	if s[8] != '-' || s[13] != '-' || s[18] != '-' || s[23] != '-' {
		t.Errorf("UUID4 format: %q", s)
	}
	s2, _ := UUID4()
	if s == s2 {
		t.Error("UUID4 should be unique")
	}
}

func TestMustUUID4(t *testing.T) {
	s := MustUUID4()
	if len(s) != 36 {
		t.Errorf("MustUUID4 length = %d", len(s))
	}
}

func TestSnowflakeGenerator(t *testing.T) {
	g := NewSnowflakeGenerator(1)
	id1 := g.Next()
	id2 := g.Next()
	if id1 == id2 {
		t.Error("snowflake IDs should differ")
	}
	if id1 <= 0 || id2 <= 0 {
		t.Errorf("snowflake IDs should be positive: %d, %d", id1, id2)
	}
}

func TestSnowflakeID(t *testing.T) {
	a := SnowflakeID()
	b := SnowflakeID()
	if a == b {
		t.Error("SnowflakeID() should return different values")
	}
}

func TestSnowflakeIDString(t *testing.T) {
	s := SnowflakeIDString()
	if s == "" {
		t.Error("SnowflakeIDString should not be empty")
	}
}

func TestIdGenerator_NextID(t *testing.T) {
	u := GeneratorFor(keys.IdKindUUID)
	id1 := u.NextID()
	id2 := u.NextID()
	if id1 == id2 {
		t.Error("UUID NextID should be unique")
	}
	if len(id1) != 36 {
		t.Errorf("UUID NextID length = %d", len(id1))
	}

	s := GeneratorFor(keys.IdKindSnowflake)
	a := s.NextID()
	b := s.NextID()
	if a == b {
		t.Error("Snowflake NextID should be unique")
	}
}

func TestNextID(t *testing.T) {
	id1 := NextID(keys.IdKindUUID)
	id2 := NextID(keys.IdKindUUID)
	if id1 == id2 {
		t.Error("NextID(uuid) should be unique")
	}
	id3 := NextID(keys.IdKindSnowflake)
	if id3 == "" {
		t.Error("NextID(snowflake) should not be empty")
	}
	id4 := NextID(keys.IdKind("unknown"))
	if len(id4) != 36 {
		t.Errorf("unknown kind should fallback to uuid, got len %d", len(id4))
	}
}
