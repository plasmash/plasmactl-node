package ovh

import (
	"reflect"
	"testing"
)

func TestSynthesizeDiskPaths_NVMe(t *testing.T) {
	got := SynthesizeDiskPaths("NVMe", 2)
	want := []string{"/dev/nvme0n1", "/dev/nvme1n1"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// Real OVH `/dedicated/server/{name}/specifications/hardware` returns
// diskGroups[].diskType in upper case ("NVME"), not the doc-claimed mixed
// case ("NVMe"). This test pins the case-insensitive behavior so adopted
// NVMe SKUs (e.g. OVH advance-1 BHS) get correct /dev/nvmeXn1 paths
// instead of silently falling through to /dev/sd<a..>.
func TestSynthesizeDiskPaths_NVME_uppercase(t *testing.T) {
	got := SynthesizeDiskPaths("NVME", 2)
	want := []string{"/dev/nvme0n1", "/dev/nvme1n1"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestSynthesizeDiskPaths_SATA(t *testing.T) {
	got := SynthesizeDiskPaths("SATA", 3)
	want := []string{"/dev/sda", "/dev/sdb", "/dev/sdc"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestSynthesizeDiskPaths_SSD(t *testing.T) {
	got := SynthesizeDiskPaths("SSD", 2)
	want := []string{"/dev/sda", "/dev/sdb"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestSynthesizeDiskPaths_HDD(t *testing.T) {
	got := SynthesizeDiskPaths("HDD", 1)
	want := []string{"/dev/sda"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestSynthesizeDiskPaths_SAS(t *testing.T) {
	got := SynthesizeDiskPaths("SAS", 2)
	want := []string{"/dev/sda", "/dev/sdb"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestSynthesizeDiskPaths_ZeroCountReturnsEmpty(t *testing.T) {
	got := SynthesizeDiskPaths("NVMe", 0)
	if len(got) != 0 {
		t.Fatalf("got %v, want empty", got)
	}
}
