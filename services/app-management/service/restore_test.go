package service

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/F-e-n-y-x/NivaroOS/services/app-management/common"
	"github.com/F-e-n-y-x/NivaroOS/services/app-management/pkg/config"
)

func TestRecreateRestoredApps(t *testing.T) {
	dir := t.TempDir()
	oldApps, oldData := config.AppInfo.AppsPath, AppDataRoot
	defer func() { config.AppInfo.AppsPath, AppDataRoot = oldApps, oldData }()
	config.AppInfo.AppsPath, AppDataRoot = filepath.Join(dir, "apps"), filepath.Join(dir, "AppData")

	compose := map[string]string{
		// uses zdb's network: must come after zdb
		"web": "services:\n  web:\n    image: busybox\n    volumes: [\"" + AppDataRoot + "/web/config:/config\"]\n    networks: [shared]\nnetworks:\n  shared:\n    external: true\n    name: zdb_net\n",
		"zdb": "services:\n  zdb:\n    image: busybox\n    privileged: true\n    volumes: [\"" + AppDataRoot + "/zdb:/data\"]\n    networks: [net]\nnetworks:\n  net: {}\n",
		"old": "services:\n  old:\n    image: busybox\n",
		"bad": "services: [nope\n",
		"err": "services:\n  err:\n    image: busybox\n",
	}
	for name, body := range compose {
		p := filepath.Join(config.AppInfo.AppsPath, name, common.ComposeYAMLFileName)
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	_ = os.MkdirAll(filepath.Join(AppDataRoot, "zdb"), 0o755) // zdb's data was restored, web's wasn't

	file := filepath.Join(dir, "restore.json")
	st := RestoreStatus{ID: "x", State: "pending"}
	for _, n := range []string{"web", "zdb", "old", "bad", "err", "../etc", "gone"} {
		st.Apps = append(st.Apps, RestoreApp{Name: n, Status: "waiting"})
	}
	st.Apps[len(st.Apps)-1].Status = "ok" // finished before a restart: left alone
	if err := writeJSONAtomic(file, st); err != nil {
		t.Fatal(err)
	}

	var order []string
	exists := func(_ context.Context, name string) (bool, error) { return name == "old", nil }
	install := func(_ context.Context, app *ComposeApp) error {
		order = append(order, app.Name)
		if app.Name == "err" {
			return errors.New("pull failed")
		}
		return nil
	}
	if err := recreateApps(context.Background(), file, exists, install); err != nil {
		t.Fatal(err)
	}
	if want := []string{"err", "zdb", "web"}; !reflect.DeepEqual(order, want) {
		t.Fatalf("install order %v, want %v", order, want)
	}
	raw, _ := os.ReadFile(file)
	var got RestoreStatus
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	res := map[string]RestoreApp{}
	for _, a := range got.Apps {
		res[a.Name] = a
	}
	check := func(name, status string, dataMissing bool) {
		t.Helper()
		if a := res[name]; a.Status != status || a.DataMissing != dataMissing {
			t.Errorf("%s: %+v, want %s data_missing=%v", name, a, status, dataMissing)
		}
	}
	check("web", "ok", true)
	check("zdb", "ok", false)
	check("old", "skipped", false)
	check("bad", "failed", false)
	check("err", "failed", false)
	check("../etc", "failed", false)
	check("gone", "ok", false)
	if got.State != "done" || got.ID != "x" || res["err"].Reason != "pull failed" ||
		!reflect.DeepEqual(res["zdb"].HostAccess, []string{"zdb: privileged"}) {
		t.Fatalf("status: %s", raw)
	}

	// done: never again
	order = nil
	_ = recreateApps(context.Background(), file, exists, install)
	if order != nil {
		t.Fatal("a done restore ran again")
	}
	if err := recreateApps(context.Background(), filepath.Join(dir, "none.json"), exists, install); err != nil {
		t.Fatal("no restore file must be a no-op:", err)
	}
}
