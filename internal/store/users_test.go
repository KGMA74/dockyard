package store

import (
	"errors"
	"slices"
	"testing"
)

func TestUserCRUD(t *testing.T) {
	s, _ := openTemp(t)

	u, err := s.CreateUser("alice", "hash-a", RoleAdmin, nil)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if u.Role != RoleAdmin || len(u.RepoPatterns) != 0 {
		t.Errorf("created user = %+v, want admin with no patterns", u)
	}
	if _, err := s.CreateUser("bob", "hash-b", RolePusher, []string{"team/*"}); err != nil {
		t.Fatalf("CreateUser bob: %v", err)
	}

	got, err := s.GetUser("bob")
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if got.Role != RolePusher || !slices.Equal(got.RepoPatterns, []string{"team/*"}) {
		t.Errorf("bob = %+v, want pusher with [team/*]", got)
	}

	users, err := s.ListUsers()
	if err != nil || len(users) != 2 {
		t.Fatalf("ListUsers = %d users, %v; want 2", len(users), err)
	}
	if n, _ := s.CountUsers(); n != 2 {
		t.Errorf("CountUsers = %d, want 2", n)
	}

	if err := s.UpdateUserPassword("bob", "new-hash"); err != nil {
		t.Fatalf("UpdateUserPassword: %v", err)
	}
	got, _ = s.GetUser("bob")
	if got.PasswordHash != "new-hash" {
		t.Errorf("password hash not updated")
	}

	if err := s.UpdateUserAccess("bob", RoleReader, []string{"other/*"}); err != nil {
		t.Fatalf("UpdateUserAccess: %v", err)
	}
	got, _ = s.GetUser("bob")
	if got.Role != RoleReader || !slices.Equal(got.RepoPatterns, []string{"other/*"}) {
		t.Errorf("bob after update = %+v", got)
	}

	if err := s.DeleteUser("bob"); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}
	if _, err := s.GetUser("bob"); !errors.Is(err, ErrUserNotFound) {
		t.Errorf("GetUser after delete = %v, want ErrUserNotFound", err)
	}
}

func TestDuplicateUsernameRejected(t *testing.T) {
	s, _ := openTemp(t)
	if _, err := s.CreateUser("alice", "h", RoleAdmin, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateUser("alice", "h2", RoleReader, nil); err == nil {
		t.Fatal("duplicate username accepted")
	}
}

func TestLastAdminProtection(t *testing.T) {
	s, _ := openTemp(t)
	if _, err := s.CreateUser("root", "h", RoleAdmin, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteUser("root"); err == nil {
		t.Error("deleting the last admin succeeded, want refusal")
	}
	if err := s.UpdateUserAccess("root", RoleReader, nil); err == nil {
		t.Error("demoting the last admin succeeded, want refusal")
	}

	// With a second admin, both operations become legal.
	if _, err := s.CreateUser("root2", "h", RoleAdmin, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateUserAccess("root", RoleReader, nil); err != nil {
		t.Errorf("demote with another admin present: %v", err)
	}
	if err := s.DeleteUser("root2"); err == nil {
		t.Error("root2 is now the last admin, delete should fail")
	}
}

func TestUnknownUserOperations(t *testing.T) {
	s, _ := openTemp(t)
	if _, err := s.GetUser("ghost"); !errors.Is(err, ErrUserNotFound) {
		t.Errorf("GetUser(ghost) = %v, want ErrUserNotFound", err)
	}
	if err := s.UpdateUserPassword("ghost", "h"); !errors.Is(err, ErrUserNotFound) {
		t.Errorf("UpdateUserPassword(ghost) = %v, want ErrUserNotFound", err)
	}
	if _, err := s.CreateUser("x", "h", "wizard", nil); err == nil {
		t.Error("invalid role accepted")
	}
}
