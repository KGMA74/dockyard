package store

import (
	"path/filepath"
	"testing"
	"time"
)

func TestRobotExpiry(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "dockyard.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	soon := time.Now().Add(time.Hour)
	r, err := st.CreateRobot("deploy", "", RoleReader, nil, "hash", "admin", &soon)
	if err != nil {
		t.Fatal(err)
	}
	if r.Expired(time.Now()) {
		t.Fatal("robot expiring in an hour is already expired")
	}
	if !r.Expired(time.Now().Add(2 * time.Hour)) {
		t.Fatal("robot is not expired after its expiry date")
	}

	forever, err := st.CreateRobot("forever", "", RolePusher, nil, "hash2", "admin", nil)
	if err != nil {
		t.Fatal(err)
	}
	if forever.Expired(time.Now().Add(100 * 365 * 24 * time.Hour)) {
		t.Fatal("a robot without expiry expired")
	}

	if _, err := st.CreateRobot("root", "", RoleAdmin, nil, "hash3", "admin", nil); err == nil {
		t.Fatal("a robot was created with the admin role")
	}
}

func TestTouchRobotIsThrottled(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "dockyard.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	r, err := st.CreateRobot("ci", "", RolePusher, nil, "hash", "admin", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.TouchRobot(r.ID); err != nil {
		t.Fatal(err)
	}
	first, _ := st.RobotByID(r.ID)
	if first.LastUsedAt == nil {
		t.Fatal("last_used_at not recorded")
	}
	// Force an old value: the next touch must move it; an immediate one must not.
	if _, err := st.db.Exec(`UPDATE robots SET last_used_at = '2000-01-01T00:00:00Z' WHERE id = ?`, r.ID); err != nil {
		t.Fatal(err)
	}
	_ = st.TouchRobot(r.ID)
	second, _ := st.RobotByID(r.ID)
	if *second.LastUsedAt == "2000-01-01T00:00:00Z" {
		t.Fatal("stale last_used_at was not refreshed")
	}
	_ = st.TouchRobot(r.ID)
	third, _ := st.RobotByID(r.ID)
	if *third.LastUsedAt != *second.LastUsedAt {
		t.Fatal("last_used_at rewritten within the same minute")
	}
}
