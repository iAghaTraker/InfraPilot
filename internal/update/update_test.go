package update

import "testing"

func TestCompareVersions(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		want int
	}{{"1.2.0", "1.3.0", -1}, {"v2.0.0", "1.9.9", 1}, {"1.0.0", "v1.0.0", 0}} {
		if got := Compare(tc.a, tc.b); got != tc.want {
			t.Errorf("Compare(%q,%q)=%d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestVerifySHA256(t *testing.T) {
	if !VerifySHA256([]byte("infrapilot"), "ce49bd61db5da656e9dabf88e72177d65e2722f05574ba943c824276dc08f27a") {
		t.Fatal("valid checksum rejected")
	}
	if VerifySHA256([]byte("modified"), "ce49bd61db5da656e9dabf88e72177d65e2722f05574ba943c824276dc08f27a") {
		t.Fatal("modified content accepted")
	}
}
