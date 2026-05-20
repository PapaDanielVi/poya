//nolint:testpackage // tests access unexported methods (InternalSet, InternalKey, etc.)
package poya

import (
	"encoding/json"
	"sync"
	"testing"
)

func TestNewDcValue(t *testing.T) {
	t.Parallel()
	d := NewDcValue("hello")
	if got := d.Get(); got != "hello" {
		t.Errorf("Get() = %v, want %v", got, "hello")
	}
}

func TestDcValueInt(t *testing.T) {
	t.Parallel()
	d := NewDcValue(42)
	if got := d.Get(); got != 42 {
		t.Errorf("Get() = %v, want %v", got, 42)
	}
}

func TestDcValueInternalSet(t *testing.T) {
	t.Parallel()
	d := NewDcValue("initial")
	d.InternalSet("updated")
	if got := d.Get(); got != "updated" {
		t.Errorf("Get() = %v, want %v", got, "updated")
	}
}

func TestDcValueInternalKey(t *testing.T) {
	t.Parallel()
	d := NewDcValue("val")
	d.InternalKey("mykey")
}

func TestDcValueInternalDefault(t *testing.T) {
	t.Parallel()
	d := NewDcValue("default_val")
	if got := d.InternalDefault(); got != "default_val" {
		t.Errorf("InternalDefault() = %v, want %v", got, "default_val")
	}
}

func TestDcValueConcurrentGetSet(t *testing.T) {
	t.Parallel()
	d := NewDcValue(0)
	var wg sync.WaitGroup

	for i := range 100 {
		wg.Add(2)
		go func(v int) {
			defer wg.Done()
			d.InternalSet(v)
		}(i)
		go func() {
			defer wg.Done()
			_ = d.Get()
		}()
	}

	wg.Wait()
}

func TestNewDcValueStruct(t *testing.T) {
	t.Parallel()
	type Config struct {
		Host string
		Port int
	}
	d := NewDcValue(Config{Host: "localhost", Port: 5432})
	if d.InternalKind() != entryKindStruct {
		t.Errorf("InternalKind() = %v, want entryKindStruct", d.InternalKind())
	}
	if got := d.Get(); got.Host != "localhost" || got.Port != 5432 {
		t.Errorf("Get() = %+v, want {localhost 5432}", got)
	}
}

func TestNewDcValueScalarKind(t *testing.T) {
	t.Parallel()
	d := NewDcValue(42)
	if d.InternalKind() != entryKindScalar {
		t.Errorf("InternalKind() = %v, want entryKindScalar", d.InternalKind())
	}
}

func TestDcValueInternalSetJSON(t *testing.T) {
	t.Parallel()
	type Config struct {
		Host string `json:"host"`
		Port int    `json:"port"`
	}
	d := NewDcValue(Config{Host: "localhost", Port: 5432})
	err := d.InternalSetJSON([]byte(`{"host":"remote","port":3306}`))
	if err != nil {
		t.Fatalf("InternalSetJSON() error: %v", err)
	}
	got := d.Get()
	if got.Host != "remote" || got.Port != 3306 {
		t.Errorf("Get() = %+v, want {remote 3306}", got)
	}
}

func TestDcValueInternalSetJSONInvalid(t *testing.T) {
	t.Parallel()
	type Config struct {
		Host string `json:"host"`
		Port int    `json:"port"`
	}
	d := NewDcValue(Config{Host: "localhost", Port: 5432})
	err := d.InternalSetJSON([]byte("invalid json"))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
	got := d.Get()
	if got.Host != "localhost" || got.Port != 5432 {
		t.Errorf("Get() should be unchanged, got %+v", got)
	}
}

func TestDcValueStructConcurrentGetSet(t *testing.T) {
	t.Parallel()
	type Config struct {
		Host string `json:"host"`
		Port int    `json:"port"`
	}
	d := NewDcValue(Config{Host: "localhost", Port: 5432})
	var wg sync.WaitGroup

	for i := range 100 {
		wg.Add(2)
		go func(v int) {
			defer wg.Done()
			cfg := Config{Host: "remote", Port: v}
			data, _ := json.Marshal(cfg)
			d.InternalSetJSON(data)
		}(i)
		go func() {
			defer wg.Done()
			_ = d.Get()
		}()
	}

	wg.Wait()
}
