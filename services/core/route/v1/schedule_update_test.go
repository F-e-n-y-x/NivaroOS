package v1

import "testing"

// The Scheduled Tasks editor doesn't know migrated_to; its saves must keep
// a task Backup & Sync took over marked. Backup & Sync's own update names
// the field, including "" to hand the task back.
func TestScheduleUpdateMarkerOnlyWhenNamed(t *testing.T) {
	task, err := decodeScheduleUpdate([]byte(`{"name":"x","cron":"@daily","enabled":true}`))
	if err != nil {
		t.Fatal(err)
	}
	if task.MigratedTo != "" {
		t.Fatalf("marker set from nothing: %q", task.MigratedTo)
	}
	// Indirect check through the service: an unnamed field keeps the
	// stored marker (TestMigratedMarkerSurvivesEditsAndIsReleasedExplicitly);
	// here, that the named cases reach SetMigratedTo.
	for body, want := range map[string]string{
		`{"name":"x","migrated_to":"backup"}`:   "backup",
		`{"name":"x","migrated_to":""}`:         "",
		`{"name":"x","migrated_to":" backup "}`: "backup",
	} {
		task, err := decodeScheduleUpdate([]byte(body))
		if err != nil {
			t.Fatal(err)
		}
		if task.MigratedTo != want {
			t.Errorf("%s: migrated_to %q, want %q", body, task.MigratedTo, want)
		}
	}
	if _, err := decodeScheduleUpdate([]byte(`{"name":`)); err == nil {
		t.Fatal("bad JSON accepted")
	}
}
