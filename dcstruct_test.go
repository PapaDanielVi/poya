package poya

import (
	"sync"
	"testing"
)

func TestNewDcStruct(t *testing.T) {
	type Config struct {
		Host string `json:"host"`
		Port int    `json:"port"`
	}

	d := NewDcStruct(Config{Host: "localhost", Port: 5432})
	got := d.Get()
	if got.Host != "localhost" || got.Port != 5432 {
		t.Errorf("Get() = %+v, want {localhost 5432}", got)
	}
}

func TestDcStructInternalSetJSON(t *testing.T) {
	type Config struct {
		Host string `json:"host"`
		Port int    `json:"port"`
	}

	d := NewDcStruct(Config{})

	err := d.InternalSetJSON([]byte(`{"host":"remote","port":3306}`))
	if err != nil {
		t.Fatalf("InternalSetJSON error: %v", err)
	}

	got := d.Get()
	if got.Host != "remote" || got.Port != 3306 {
		t.Errorf("Get() = %+v, want {remote 3306}", got)
	}
}

func TestDcStructInternalSetJSONInvalid(t *testing.T) {
	type Config struct {
		Host string `json:"host"`
	}

	d := NewDcStruct(Config{Host: "default"})

	err := d.InternalSetJSON([]byte(`not json`))
	if err == nil {
		t.Error("expected error for invalid JSON")
	}

	// Value should remain unchanged
	if got := d.Get(); got.Host != "default" {
		t.Errorf("Get() = %+v, want {default}", got)
	}
}

func TestDcStructInternalKey(_ *testing.T) {
	type Config struct {
		Val string `json:"val"`
	}
	d := NewDcStruct(Config{})
	d.InternalKey("mykey")
}

func TestDcStructInternalDefault(t *testing.T) {
	type Config struct {
		Val string `json:"val"`
	}
	d := NewDcStruct(Config{Val: "def"})
	if got := d.InternalDefault(); got.Val != "def" {
		t.Errorf("InternalDefault() = %+v, want {def}", got)
	}
}

func TestDcStructConcurrentGetSet(_ *testing.T) {
	type Config struct {
		Counter int `json:"counter"`
	}

	d := NewDcStruct(Config{})
	var wg sync.WaitGroup

	for i := range 100 {
		wg.Add(2)
		go func(v int) {
			defer wg.Done()
			_ = d.InternalSetJSON([]byte(`{"counter":` + string(rune('0'+v%10)) + `}`))
		}(i)
		go func() {
			defer wg.Done()
			_ = d.Get()
		}()
	}

	wg.Wait()
}
