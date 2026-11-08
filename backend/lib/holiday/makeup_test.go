package holiday

import "testing"

func TestIsMakeupWorkday(t *testing.T) {
	p, err := NewProvider()
	if err != nil {
		t.Fatal(err)
	}
	c := NewCalendar(p, RegionCN)
	for d, want := range map[string]bool{"2025-09-28": true, "2025-10-11": true, "2025-10-01": false, "2025-10-09": false, "2025-10-12": false} {
		if got := c.IsMakeupWorkday(d); got != want {
			t.Errorf("IsMakeupWorkday(%s) = %v", d, got)
		}
	}
}
