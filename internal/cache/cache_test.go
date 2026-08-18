package cache

import (
	"testing"
	"time"
)

func TestGetSetHit(t *testing.T) {
	c := New(time.Minute)
	c.Set("k", 42)
	v, ok := c.Get("k")
	if !ok || v.(int) != 42 {
		t.Fatalf("expected hit with value 42, got ok=%v v=%v", ok, v)
	}
}

func TestGetMiss(t *testing.T) {
	c := New(time.Minute)
	if _, ok := c.Get("missing"); ok {
		t.Fatal("expected miss for unset key")
	}
}

func TestExpiry(t *testing.T) {
	c := New(10 * time.Millisecond)
	c.Set("k", "v")
	time.Sleep(20 * time.Millisecond)
	if _, ok := c.Get("k"); ok {
		t.Fatal("expected expired entry to be a miss")
	}
}
