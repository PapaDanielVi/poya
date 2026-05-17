package poya

import (
	"sync"
	"testing"
)

func TestNewDcValue(t *testing.T) {
	d := NewDcValue("hello")
	if got := d.Get(); got != "hello" {
		t.Errorf("Get() = %v, want %v", got, "hello")
	}
}

func TestDcValueInt(t *testing.T) {
	d := NewDcValue(42)
	if got := d.Get(); got != 42 {
		t.Errorf("Get() = %v, want %v", got, 42)
	}
}

func TestDcValueInternalSet(t *testing.T) {
	d := NewDcValue("initial")
	d.InternalSet("updated")
	if got := d.Get(); got != "updated" {
		t.Errorf("Get() = %v, want %v", got, "updated")
	}
}

func TestDcValueInternalKey(_ *testing.T) {
	d := NewDcValue("val")
	d.InternalKey("mykey")
}

func TestDcValueInternalDefault(t *testing.T) {
	d := NewDcValue("default_val")
	if got := d.InternalDefault(); got != "default_val" {
		t.Errorf("InternalDefault() = %v, want %v", got, "default_val")
	}
}

func TestDcValueConcurrentGetSet(_ *testing.T) {
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
