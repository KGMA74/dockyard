package store

import "testing"

func TestReserveQuotaUnlimitedWithoutConfiguredQuota(t *testing.T) {
	s, _ := openTemp(t)

	blocked, warnings, err := s.ReserveQuota(1_000_000, QuotaScope{Type: "repo", Value: "team/api"})
	if err != nil {
		t.Fatalf("ReserveQuota: %v", err)
	}
	if blocked != nil {
		t.Fatalf("expected no quota configured to be unlimited, got blocked: %+v", blocked)
	}
	if len(warnings) != 0 {
		t.Fatalf("expected no warnings without a configured quota, got %+v", warnings)
	}

	usage, err := s.ListQuotaUsage()
	if err != nil {
		t.Fatalf("ListQuotaUsage: %v", err)
	}
	if len(usage) != 1 || usage[0].BytesUsed != 1_000_000 {
		t.Fatalf("expected usage tracked even without a quota, got %+v", usage)
	}
}

func TestReserveQuotaBlocksOverLimit(t *testing.T) {
	s, _ := openTemp(t)

	if _, err := s.SetQuota("repo", "team/api", 1000, 90); err != nil {
		t.Fatalf("SetQuota: %v", err)
	}

	blocked, _, err := s.ReserveQuota(2000, QuotaScope{Type: "repo", Value: "team/api"})
	if err != nil {
		t.Fatalf("ReserveQuota: %v", err)
	}
	if blocked == nil {
		t.Fatal("expected push over the 1000-byte limit to be blocked")
	}
	if blocked.ScopeValue != "team/api" || blocked.MaxBytes != 1000 {
		t.Fatalf("unexpected blocked scope: %+v", blocked)
	}

	usage, err := s.ListQuotaUsage()
	if err != nil {
		t.Fatalf("ListQuotaUsage: %v", err)
	}
	if len(usage) != 0 {
		t.Fatalf("a blocked reservation must not record any usage, got %+v", usage)
	}
}

func TestReserveQuotaAllowsWithinLimitAndAccumulates(t *testing.T) {
	s, _ := openTemp(t)

	if _, err := s.SetQuota("repo", "team/api", 1000, 90); err != nil {
		t.Fatalf("SetQuota: %v", err)
	}

	if blocked, _, err := s.ReserveQuota(400, QuotaScope{Type: "repo", Value: "team/api"}); err != nil || blocked != nil {
		t.Fatalf("first 400-byte push should fit: blocked=%+v err=%v", blocked, err)
	}
	if blocked, _, err := s.ReserveQuota(400, QuotaScope{Type: "repo", Value: "team/api"}); err != nil || blocked != nil {
		t.Fatalf("second 400-byte push should fit (800/1000): blocked=%+v err=%v", blocked, err)
	}
	blocked, _, err := s.ReserveQuota(400, QuotaScope{Type: "repo", Value: "team/api"})
	if err != nil {
		t.Fatalf("ReserveQuota: %v", err)
	}
	if blocked == nil {
		t.Fatal("third 400-byte push should overflow the 1000-byte limit (800+400)")
	}

	usage, err := s.ListQuotaUsage()
	if err != nil {
		t.Fatalf("ListQuotaUsage: %v", err)
	}
	if len(usage) != 1 || usage[0].BytesUsed != 800 {
		t.Fatalf("expected 800 bytes recorded (the blocked push must not count), got %+v", usage)
	}
}

func TestReserveQuotaMultipleScopesAllOrNothing(t *testing.T) {
	s, _ := openTemp(t)

	if _, err := s.SetQuota("repo", "team/api", 1_000_000, 90); err != nil {
		t.Fatalf("SetQuota repo: %v", err)
	}
	if _, err := s.SetQuota("user", "alice", 500, 90); err != nil {
		t.Fatalf("SetQuota user: %v", err)
	}

	// alice's user quota (500) is smaller than the repo quota, so the push
	// must be blocked overall, and neither scope's usage should move.
	blocked, _, err := s.ReserveQuota(600,
		QuotaScope{Type: "repo", Value: "team/api"},
		QuotaScope{Type: "user", Value: "alice"},
	)
	if err != nil {
		t.Fatalf("ReserveQuota: %v", err)
	}
	if blocked == nil || blocked.ScopeType != "user" {
		t.Fatalf("expected the user scope to be reported as blocked, got %+v", blocked)
	}

	usage, err := s.ListQuotaUsage()
	if err != nil {
		t.Fatalf("ListQuotaUsage: %v", err)
	}
	if len(usage) != 0 {
		t.Fatalf("neither scope should have recorded usage when one is blocked, got %+v", usage)
	}
}

func TestReserveQuotaWarningFiresOnceWhenCrossingThreshold(t *testing.T) {
	s, _ := openTemp(t)

	if _, err := s.SetQuota("repo", "team/api", 1000, 90); err != nil {
		t.Fatalf("SetQuota: %v", err)
	}

	// 850/1000 = 85%, below the 90% warn threshold.
	if _, warnings, err := s.ReserveQuota(850, QuotaScope{Type: "repo", Value: "team/api"}); err != nil || len(warnings) != 0 {
		t.Fatalf("expected no warning at 85%%: warnings=%+v err=%v", warnings, err)
	}

	// 850 -> 920 crosses the 90% threshold: must warn exactly once.
	_, warnings, err := s.ReserveQuota(70, QuotaScope{Type: "repo", Value: "team/api"})
	if err != nil {
		t.Fatalf("ReserveQuota: %v", err)
	}
	if len(warnings) != 1 || warnings[0].Percent < 90 {
		t.Fatalf("expected exactly one warning at >=90%%, got %+v", warnings)
	}

	// Already past threshold: no repeat warning on the next push.
	_, warnings, err = s.ReserveQuota(10, QuotaScope{Type: "repo", Value: "team/api"})
	if err != nil {
		t.Fatalf("ReserveQuota: %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("expected no repeat warning once already above threshold, got %+v", warnings)
	}
}

func TestSetQuotaUpsertAndDelete(t *testing.T) {
	s, _ := openTemp(t)

	q, err := s.SetQuota("repo", "team/api", 1000, 90)
	if err != nil {
		t.Fatalf("SetQuota: %v", err)
	}
	if q.MaxBytes != 1000 {
		t.Fatalf("expected max_bytes=1000, got %d", q.MaxBytes)
	}

	// Upsert: same scope, new limit, same row.
	q2, err := s.SetQuota("repo", "team/api", 2000, 80)
	if err != nil {
		t.Fatalf("SetQuota upsert: %v", err)
	}
	if q2.ID != q.ID {
		t.Fatalf("expected upsert to reuse the same row id, got %d != %d", q2.ID, q.ID)
	}
	if q2.MaxBytes != 2000 || q2.WarnPercent != 80 {
		t.Fatalf("expected updated values, got %+v", q2)
	}

	quotas, err := s.ListQuotas()
	if err != nil {
		t.Fatalf("ListQuotas: %v", err)
	}
	if len(quotas) != 1 {
		t.Fatalf("expected exactly one quota row after upsert, got %d", len(quotas))
	}

	if err := s.DeleteQuota(q2.ID); err != nil {
		t.Fatalf("DeleteQuota: %v", err)
	}
	if err := s.DeleteQuota(q2.ID); err == nil {
		t.Fatal("expected deleting an already-deleted quota to fail")
	}
}

func TestResetQuotaUsage(t *testing.T) {
	s, _ := openTemp(t)

	if _, _, err := s.ReserveQuota(500, QuotaScope{Type: "repo", Value: "team/api"}); err != nil {
		t.Fatalf("ReserveQuota: %v", err)
	}
	if err := s.ResetQuotaUsage("repo", "team/api"); err != nil {
		t.Fatalf("ResetQuotaUsage: %v", err)
	}
	usage, err := s.ListQuotaUsage()
	if err != nil {
		t.Fatalf("ListQuotaUsage: %v", err)
	}
	if len(usage) != 0 {
		t.Fatalf("expected usage cleared after reset, got %+v", usage)
	}
}
