package main

import "testing"

func TestHostGuideStatusIsInformationalAndExcludedFromManagedTools(t *testing.T) {
	a := newTestApp(t)
	if err := a.SetProjectDir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	status, err := a.Status()
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"vba", "abap", "verse", "plc"} {
		found := false
		for _, row := range status {
			if row.Tool == id {
				found = true
				if row.Support.Mode != "host" || row.Support.Guide == nil || len(row.Installed) != 0 || row.Current != "" {
					t.Fatalf("host entry offers an independent runtime: %+v", row)
				}
			}
		}
		if !found {
			t.Fatalf("host guide missing from GUI: %s", id)
		}
		for _, tool := range a.Tools() {
			if tool == id {
				t.Fatalf("host guide offered as selectable project tool: %s", id)
			}
		}
		if err := a.SetLocal(id, "1"); err == nil {
			t.Fatalf("host guide selected as local tool: %s", id)
		}
	}
}
