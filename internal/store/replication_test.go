package store

import (
	"testing"
	"time"
)

func TestReplicationTargetCRUD(t *testing.T) {
	s, _ := openTemp(t)

	target, err := s.CreateReplicationTarget(ReplicationTarget{
		Name: "dr-site", BaseURL: "http://dr.example:8080", Username: "admin", Password: "secret",
		RepoPattern: "team/*", Enabled: true,
	})
	if err != nil {
		t.Fatalf("CreateReplicationTarget: %v", err)
	}
	if target.RepoPattern != "team/*" {
		t.Fatalf("unexpected repo_pattern: %+v", target)
	}

	list, err := s.ListReplicationTargets()
	if err != nil {
		t.Fatalf("ListReplicationTargets: %v", err)
	}
	if len(list) != 1 || list[0].ID != target.ID {
		t.Fatalf("expected exactly the created target, got %+v", list)
	}

	fetched, err := s.ReplicationTargetByID(target.ID)
	if err != nil {
		t.Fatalf("ReplicationTargetByID: %v", err)
	}
	if fetched.BaseURL != target.BaseURL {
		t.Fatalf("mismatched fetched target: %+v", fetched)
	}

	if err := s.DeleteReplicationTarget(target.ID); err != nil {
		t.Fatalf("DeleteReplicationTarget: %v", err)
	}
	if err := s.DeleteReplicationTarget(target.ID); err == nil {
		t.Fatal("expected deleting an already-deleted target to fail")
	}
}

func TestCreateReplicationTargetDefaultsPattern(t *testing.T) {
	s, _ := openTemp(t)
	target, err := s.CreateReplicationTarget(ReplicationTarget{Name: "x", BaseURL: "http://x"})
	if err != nil {
		t.Fatalf("CreateReplicationTarget: %v", err)
	}
	if target.RepoPattern != "*" {
		t.Fatalf("expected default repo_pattern '*', got %q", target.RepoPattern)
	}
}

func TestCreateReplicationTargetRequiresNameAndURL(t *testing.T) {
	s, _ := openTemp(t)
	if _, err := s.CreateReplicationTarget(ReplicationTarget{BaseURL: "http://x"}); err == nil {
		t.Fatal("expected an error without a name")
	}
	if _, err := s.CreateReplicationTarget(ReplicationTarget{Name: "x"}); err == nil {
		t.Fatal("expected an error without a base_url")
	}
}

func TestReplicationDeliveryOutbox(t *testing.T) {
	s, _ := openTemp(t)
	target, err := s.CreateReplicationTarget(ReplicationTarget{Name: "t", BaseURL: "http://x", Enabled: true})
	if err != nil {
		t.Fatalf("CreateReplicationTarget: %v", err)
	}

	if err := s.EnqueueReplication(target.ID, "team/api", "v1"); err != nil {
		t.Fatalf("EnqueueReplication: %v", err)
	}

	due, err := s.DueReplications(8, 10)
	if err != nil {
		t.Fatalf("DueReplications: %v", err)
	}
	if len(due) != 1 || due[0].Repo != "team/api" || due[0].Tag != "v1" {
		t.Fatalf("unexpected due deliveries: %+v", due)
	}

	if err := s.MarkReplicationDelivered(due[0].ID); err != nil {
		t.Fatalf("MarkReplicationDelivered: %v", err)
	}

	due, err = s.DueReplications(8, 10)
	if err != nil {
		t.Fatalf("DueReplications after delivery: %v", err)
	}
	if len(due) != 0 {
		t.Fatalf("expected no due deliveries after marking delivered, got %+v", due)
	}
}

func TestReplicationDeliveryRetryBackoff(t *testing.T) {
	s, _ := openTemp(t)
	target, err := s.CreateReplicationTarget(ReplicationTarget{Name: "t", BaseURL: "http://x", Enabled: true})
	if err != nil {
		t.Fatalf("CreateReplicationTarget: %v", err)
	}
	if err := s.EnqueueReplication(target.ID, "team/api", "v1"); err != nil {
		t.Fatalf("EnqueueReplication: %v", err)
	}
	due, _ := s.DueReplications(8, 10)
	if len(due) != 1 {
		t.Fatalf("expected 1 due delivery, got %d", len(due))
	}

	if err := s.MarkReplicationFailed(due[0].ID, time.Now().Add(time.Hour), "connection refused"); err != nil {
		t.Fatalf("MarkReplicationFailed: %v", err)
	}

	// Retry time is in the future — must not be due yet.
	due, err = s.DueReplications(8, 10)
	if err != nil {
		t.Fatalf("DueReplications: %v", err)
	}
	if len(due) != 0 {
		t.Fatalf("expected no due deliveries before the backoff elapses, got %+v", due)
	}
}
