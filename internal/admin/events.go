package admin

import (
	"encoding/json"
	"net/http"
	"time"

	"dockyard/internal/auth"
	"dockyard/internal/events"
	"dockyard/internal/reference"

	"github.com/labstack/echo/v4"
)

// Events streams registry push notifications over SSE. It works the same in
// embedded and proxy mode since both v2 handlers publish to the same hub.
func Events(hub *events.Hub) echo.HandlerFunc {
	return func(c echo.Context) error {
		w := c.Response()
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.WriteHeader(http.StatusOK)
		w.Flush()

		ch := hub.Subscribe()
		defer hub.Unsubscribe(ch)

		// Accounts restricted to some repositories only hear about those.
		principal, _ := auth.CurrentPrincipal(c)
		restricted := len(principal.RepoPatterns) > 0

		// Keeps the connection alive through proxies/load balancers that
		// close idle streams.
		ticker := time.NewTicker(25 * time.Second)
		defer ticker.Stop()

		ctx := c.Request().Context()
		for {
			select {
			case <-ctx.Done():
				return nil

			case <-ticker.C:
				if _, err := w.Write([]byte(": ping\n\n")); err != nil {
					return nil
				}
				w.Flush()

			case ev, ok := <-ch:
				if !ok {
					return nil
				}
				if restricted && !eventVisible(principal, ev) {
					continue
				}
				data, err := json.Marshal(ev)
				if err != nil {
					continue
				}
				if _, err := w.Write([]byte("data: " + string(data) + "\n\n")); err != nil {
					return nil
				}
				w.Flush()
			}
		}
	}
}

// eventVisible reports whether a repository-restricted principal may receive
// ev. Only events that name a repository the account can read get through;
// registry-wide ones (gc, quota warnings) are admin business.
func eventVisible(p auth.Principal, ev events.Event) bool {
	return reference.ValidName(ev.Name) && auth.MatchesRepo(p.RepoPatterns, ev.Name)
}
