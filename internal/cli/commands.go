package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

// Root builds the dockyard-cli command tree.
func Root(version string) *cobra.Command {
	root := &cobra.Command{
		Use:           "dockyard-cli",
		Short:         "Command-line client for the Dockyard registry admin API",
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(
		loginCmd(), reposCmd(), tagsCmd(), deleteCmd(), gcCmd(),
		exportCmd(), importCmd(), usersCmd(), sessionsCmd(),
	)
	return root
}

func client() (*Client, error) {
	cfg, err := LoadConfig()
	if err != nil {
		return nil, err
	}
	return NewClient(cfg), nil
}

func table(headers ...string) *tabwriter.Writer {
	w := tabwriter.NewWriter(os.Stdout, 2, 4, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, strings.Join(headers, "\t"))
	return w
}

func loginCmd() *cobra.Command {
	var username, password string
	cmd := &cobra.Command{
		Use:   "login <server-url>",
		Short: "Authenticate and store the session in ~/.dockyard/config.json",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			if password == "" {
				password = os.Getenv("DOCKYARD_PASSWORD")
			}
			if username == "" || password == "" {
				return fmt.Errorf("username (-u) and password (-p or DOCKYARD_PASSWORD) are required")
			}
			return Login(args[0], username, password)
		},
	}
	cmd.Flags().StringVarP(&username, "username", "u", "", "username")
	cmd.Flags().StringVarP(&password, "password", "p", "", "password (or env DOCKYARD_PASSWORD)")
	return cmd
}

func reposCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "repos",
		Short: "List repositories",
		Args:  cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			c, err := client()
			if err != nil {
				return err
			}
			var out struct {
				Repositories []struct {
					Name       string   `json:"name"`
					Tags       []string `json:"tags"`
					LastPushed string   `json:"last_pushed"`
				} `json:"repositories"`
			}
			if err := c.GetJSON("/api/admin/repositories", &out); err != nil {
				return err
			}
			w := table("REPOSITORY", "TAGS", "LAST PUSH")
			for _, r := range out.Repositories {
				_, _ = fmt.Fprintf(w, "%s\t%d\t%s\n", r.Name, len(r.Tags), r.LastPushed)
			}
			return w.Flush()
		},
	}
}

func tagsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "tags <repository>",
		Short: "List tags of a repository",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			c, err := client()
			if err != nil {
				return err
			}
			var out struct {
				Tags []struct {
					Tag      string `json:"tag"`
					Digest   string `json:"digest"`
					PushedAt string `json:"pushed_at"`
				} `json:"tags"`
			}
			if err := c.GetJSON("/api/admin/repositories/tags?name="+url.QueryEscape(args[0]), &out); err != nil {
				return err
			}
			w := table("TAG", "DIGEST", "PUSHED")
			for _, t := range out.Tags {
				_, _ = fmt.Fprintf(w, "%s\t%s\t%s\n", t.Tag, t.Digest, t.PushedAt)
			}
			return w.Flush()
		},
	}
}

func deleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete <repository> [tag-or-digest]",
		Short: "Delete a repository, or one manifest by tag/digest (admin)",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(_ *cobra.Command, args []string) error {
			c, err := client()
			if err != nil {
				return err
			}
			repo := url.QueryEscape(args[0])
			if len(args) == 1 {
				if err := c.JSON(http.MethodDelete, "/api/admin/repositories?name="+repo, nil, nil); err != nil {
					return err
				}
				fmt.Printf("Repository %s deleted\n", args[0])
				return nil
			}
			digest := args[1]
			if !strings.HasPrefix(digest, "sha256:") {
				// Resolve the tag to its digest first.
				var out struct {
					Tags []struct{ Tag, Digest string } `json:"tags"`
				}
				if err := c.GetJSON("/api/admin/repositories/tags?name="+repo, &out); err != nil {
					return err
				}
				for _, t := range out.Tags {
					if t.Tag == digest {
						digest = t.Digest
						break
					}
				}
				if !strings.HasPrefix(digest, "sha256:") {
					return fmt.Errorf("tag %q not found in %s", args[1], args[0])
				}
			}
			if err := c.JSON(http.MethodDelete,
				"/api/admin/repositories/manifests?name="+repo+"&digest="+url.QueryEscape(digest), nil, nil); err != nil {
				return err
			}
			fmt.Printf("Manifest %s deleted from %s (run gc to reclaim blobs)\n", digest, args[0])
			return nil
		},
	}
}

func gcCmd() *cobra.Command {
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "gc",
		Short: "Garbage-collect unreferenced blobs (admin)",
		Args:  cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			c, err := client()
			if err != nil {
				return err
			}
			path := "/api/admin/gc"
			if dryRun {
				path += "?dryRun=true"
			}
			var out struct {
				Count      int    `json:"count"`
				FreedHuman string `json:"freed_human"`
				DryRun     bool   `json:"dry_run"`
			}
			if err := c.JSON(http.MethodPost, path, nil, &out); err != nil {
				return err
			}
			verb := "removed"
			if out.DryRun {
				verb = "would remove"
			}
			fmt.Printf("GC %s %d blob(s), freeing %s\n", verb, out.Count, out.FreedHuman)
			return nil
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "preview without deleting")
	return cmd
}

func exportCmd() *cobra.Command {
	var output string
	cmd := &cobra.Command{
		Use:   "export <repository>",
		Short: "Export a repository as an OCI image-layout tarball (admin)",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			c, err := client()
			if err != nil {
				return err
			}
			if output == "" {
				output = strings.ReplaceAll(args[0], "/", "_") + ".oci.tar"
			}
			resp, err := c.Do(http.MethodGet, "/api/admin/repositories/export?name="+url.QueryEscape(args[0]), nil, "")
			if err != nil {
				return err
			}
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != http.StatusOK {
				return apiError(resp)
			}
			f, err := os.Create(output)
			if err != nil {
				return err
			}
			n, err := io.Copy(f, resp.Body)
			if closeErr := f.Close(); err == nil {
				err = closeErr
			}
			if err != nil {
				return err
			}
			fmt.Printf("Exported %s → %s (%d bytes)\n", args[0], output, n)
			return nil
		},
	}
	cmd.Flags().StringVarP(&output, "output", "o", "", "output file (default <repo>.oci.tar)")
	return cmd
}

func importCmd() *cobra.Command {
	var input string
	cmd := &cobra.Command{
		Use:   "import <repository>",
		Short: "Import an OCI image-layout tarball into a repository (admin)",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			c, err := client()
			if err != nil {
				return err
			}
			if input == "" {
				return fmt.Errorf("input file required (-i)")
			}
			f, err := os.Open(input)
			if err != nil {
				return err
			}
			defer func() { _ = f.Close() }()
			resp, err := c.Do(http.MethodPost,
				"/api/admin/repositories/import?name="+url.QueryEscape(args[0]), f, "application/x-tar")
			if err != nil {
				return err
			}
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != http.StatusOK {
				return apiError(resp)
			}
			var out struct {
				Tags int `json:"tags"`
			}
			_ = jsonDecode(resp.Body, &out)
			fmt.Printf("Imported %d tag(s) into %s\n", out.Tags, args[0])
			return nil
		},
	}
	cmd.Flags().StringVarP(&input, "input", "i", "", "OCI image-layout tarball")
	return cmd
}

func usersCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "users", Short: "Manage users (admin)"}

	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List users",
		Args:  cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			c, err := client()
			if err != nil {
				return err
			}
			var out struct {
				Users []struct {
					Username     string   `json:"username"`
					Role         string   `json:"role"`
					RepoPatterns []string `json:"repo_patterns"`
				} `json:"users"`
			}
			if err := c.GetJSON("/api/admin/users", &out); err != nil {
				return err
			}
			w := table("USERNAME", "ROLE", "REPO PATTERNS")
			for _, u := range out.Users {
				patterns := strings.Join(u.RepoPatterns, ",")
				if patterns == "" {
					patterns = "*"
				}
				_, _ = fmt.Fprintf(w, "%s\t%s\t%s\n", u.Username, u.Role, patterns)
			}
			return w.Flush()
		},
	})

	var role, password, patterns string
	create := &cobra.Command{
		Use:   "create <username>",
		Short: "Create a user",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			c, err := client()
			if err != nil {
				return err
			}
			var repoPatterns []string
			for p := range strings.SplitSeq(patterns, ",") {
				if p = strings.TrimSpace(p); p != "" {
					repoPatterns = append(repoPatterns, p)
				}
			}
			payload := map[string]any{
				"username": args[0], "password": password, "role": role, "repo_patterns": repoPatterns,
			}
			if err := c.JSON(http.MethodPost, "/api/admin/users", payload, nil); err != nil {
				return err
			}
			fmt.Printf("User %s created (%s)\n", args[0], role)
			return nil
		},
	}
	create.Flags().StringVar(&role, "role", "reader", "admin | pusher | reader")
	create.Flags().StringVarP(&password, "password", "p", "", "password (min 8 chars)")
	create.Flags().StringVar(&patterns, "repos", "", "comma-separated repo globs (empty = all)")
	cmd.AddCommand(create)

	cmd.AddCommand(&cobra.Command{
		Use:   "delete <username>",
		Short: "Delete a user",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			c, err := client()
			if err != nil {
				return err
			}
			if err := c.JSON(http.MethodDelete, "/api/admin/users/"+url.PathEscape(args[0]), nil, nil); err != nil {
				return err
			}
			fmt.Printf("User %s deleted\n", args[0])
			return nil
		},
	})
	return cmd
}

func sessionsCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "sessions", Short: "Manage sessions (admin)"}

	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List active sessions",
		Args:  cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			c, err := client()
			if err != nil {
				return err
			}
			var out struct {
				Sessions []struct {
					ID         int64  `json:"id"`
					Username   string `json:"username"`
					IP         string `json:"ip"`
					LastSeenAt string `json:"last_seen_at"`
				} `json:"sessions"`
			}
			if err := c.GetJSON("/api/admin/sessions", &out); err != nil {
				return err
			}
			w := table("ID", "USER", "IP", "LAST SEEN")
			for _, s := range out.Sessions {
				_, _ = fmt.Fprintf(w, "%d\t%s\t%s\t%s\n", s.ID, s.Username, s.IP, s.LastSeenAt)
			}
			return w.Flush()
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "revoke <id>",
		Short: "Revoke a session",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			c, err := client()
			if err != nil {
				return err
			}
			if err := c.JSON(http.MethodDelete, "/api/admin/sessions/"+url.PathEscape(args[0]), nil, nil); err != nil {
				return err
			}
			fmt.Printf("Session %s revoked\n", args[0])
			return nil
		},
	})
	return cmd
}

func jsonDecode(r io.Reader, out any) error {
	return json.NewDecoder(r).Decode(out)
}
